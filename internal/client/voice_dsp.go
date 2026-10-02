package client

import (
	"encoding/binary"
	"math"
)

// Voice processing that needs no C, so it builds and is tested everywhere
// (the engine that uses it, voice_engine.go, is CGO-only). The microphone
// chain in sendFrame is:
//
//	echo cancellation → input gain → low-cut filter → RNNoise noise
//	suppression (internal/rnnoise) → automatic levelling → Opus
//
// and each packet carries a sequence number, so receivers can conceal or
// recover lost ones (rxSequence, Opus FEC and PLC).

// voiceSampleRate is the rate voice is captured, encoded and played at.
// RNNoise only works at 48 kHz, and Opus is at its best there; the codec
// presets only change the bitrate (bitrateForPreset).
const voiceSampleRate = 48000

// bitrateForPreset maps a codec preset name to an Opus target bitrate in bps.
// Everything runs at 48 kHz; Opus picks the bandwidth the bitrate allows,
// full band from about 32 kbps.
//
//	low    →  24 000 bps  (for poor connections)
//	medium →  48 000 bps  (the default: full-band voice)
//	high   →  64 000 bps
//	ultra  →  96 000 bps  (music-grade)
func bitrateForPreset(preset string) int {
	switch preset {
	case "low":
		return 24000
	case "high":
		return 64000
	case "ultra":
		return 96000
	default: // "medium" or unset
		return 48000
	}
}

// ── Low-cut filter ───────────────────────────────────────────────────────────

// highPass is a second-order (12 dB/octave) Butterworth high-pass filter.
// lowCut runs two in a row.
type highPass struct {
	b0, b1, b2, a1, a2 float64
	x1, x2, y1, y2     float64
}

// newHighPass makes a filter cutting below cutoff Hz at sampleRate.
func newHighPass(cutoff, sampleRate float64) *highPass {
	// RBJ audio EQ cookbook, Q = 1/√2.
	w := 2 * math.Pi * cutoff / sampleRate
	alpha := math.Sin(w) / (2 * math.Sqrt2 / 2)
	cos := math.Cos(w)
	a0 := 1 + alpha
	return &highPass{
		b0: (1 + cos) / 2 / a0, b1: -(1 + cos) / a0, b2: (1 + cos) / 2 / a0,
		a1: -2 * cos / a0, a2: (1 - alpha) / a0,
	}
}

// lowCut removes rumble, desk bumps and mains hum below speech: two
// high-pass stages, 24 dB/octave below the cutoff.
type lowCut struct{ a, b *highPass }

func newLowCut(cutoff, sampleRate float64) *lowCut {
	return &lowCut{newHighPass(cutoff, sampleRate), newHighPass(cutoff, sampleRate)}
}

func (l *lowCut) process(samples []float32) {
	l.a.process(samples)
	l.b.process(samples)
}

// process filters samples in place.
func (f *highPass) process(samples []float32) {
	for i, s := range samples {
		x := float64(s)
		y := f.b0*x + f.b1*f.x1 + f.b2*f.x2 - f.a1*f.y1 - f.a2*f.y2
		f.x2, f.x1, f.y2, f.y1 = f.x1, x, f.y1, y
		samples[i] = float32(y)
	}
}

// ── Automatic levelling ──────────────────────────────────────────────────────

// autoLevel brings speech towards a common loudness, so quiet and loud
// talkers sound about the same to everyone. It only measures frames that
// are speech, and moves slowly, so it doesn't pump up the room between
// words.
type autoLevel struct {
	gain float64
}

const (
	autoLevelTarget  = 3000.0 // speech RMS to aim for, on the int16 scale (about -21 dBFS)
	autoLevelMaxGain = 4.0    // +12 dB at most
	autoLevelMinGain = 0.25   // -12 dB at most
	autoLevelUp      = 0.02   // per 20 ms frame: slow to raise...
	autoLevelDown    = 0.15   // ...quicker to lower, so a shout isn't left loud
)

func newAutoLevel() *autoLevel { return &autoLevel{gain: 1} }

// process adjusts the gain from this frame (if it's speech) and applies it,
// with a soft limit so nothing clips.
func (l *autoLevel) process(samples []float32, speech bool) {
	if speech {
		var sum float64
		for _, s := range samples {
			sum += float64(s) * float64(s)
		}
		if rms := math.Sqrt(sum / float64(len(samples))); rms > 1 {
			want := math.Min(math.Max(autoLevelTarget/rms, autoLevelMinGain), autoLevelMaxGain)
			rate := autoLevelUp
			if want < l.gain {
				rate = autoLevelDown
			}
			l.gain += (want - l.gain) * rate
		}
	}
	for i, s := range samples {
		samples[i] = softLimit(float64(s) * l.gain)
	}
}

// softLimit keeps a sample within int16 range, rounding off peaks above
// about -3 dBFS instead of clipping them.
func softLimit(x float64) float32 {
	const knee = 23000.0
	switch {
	case x > knee:
		x = knee + (32767-knee)*math.Tanh((x-knee)/(32767-knee))
	case x < -knee:
		x = -knee - (32767-knee)*math.Tanh((-x-knee)/(32767-knee))
	}
	return float32(x)
}

// ── Noise suppression strength ───────────────────────────────────────────────

// mixDenoised blends RNNoise's output with what went in, by strength (0–1):
// the noise left is (1-strength)², so 50% already removes three quarters of
// it (-12 dB) and 100% all RNNoise can.
func mixDenoised(denoised, original []float32, strength float64) {
	r := float32((1 - strength) * (1 - strength))
	if r <= 0 {
		return
	}
	for i := range denoised {
		denoised[i] += r * (original[i] - denoised[i])
	}
}

// speechThreshold is the RNNoise speech probability that opens the
// microphone, from the VAD Sensitivity setting (0–1): more sensitive opens
// on less certain speech.
func speechThreshold(sensitivity float64) float32 {
	return float32(math.Min(math.Max(0.95-0.85*sensitivity, 0.1), 0.95))
}

// ── Packets ──────────────────────────────────────────────────────────────────

// Voice packets: a tag byte, then (from opusFrameTagSeq on) a big-endian
// sequence number, then the Opus frame. Clients from before sequence
// numbers send opusFrameTag packets with none; those are still played.
const (
	opusFrameTag    byte = 0xC0 // tag, Opus
	opusFrameTagSeq byte = 0xC1 // tag, uint16 sequence, Opus
)

// voicePacket writes a sequenced packet's header into buf (which must have
// 3 bytes of room) and returns the header length.
func voicePacketHeader(buf []byte, seq uint16) int {
	buf[0] = opusFrameTagSeq
	binary.BigEndian.PutUint16(buf[1:3], seq)
	return 3
}

// parseVoicePacket splits a packet into its Opus frame and sequence number
// (hasSeq false for the older format).
func parseVoicePacket(data []byte) (payload []byte, seq uint16, hasSeq, ok bool) {
	switch {
	case len(data) >= 2 && data[0] == opusFrameTag:
		return data[1:], 0, false, true
	case len(data) >= 4 && data[0] == opusFrameTagSeq:
		return data[3:], binary.BigEndian.Uint16(data[1:3]), true, true
	}
	return nil, 0, false, false
}

// rxSequence tracks one sender's sequence numbers, to find lost packets.
type rxSequence struct {
	last    uint16
	started bool
}

// maxConcealedFrames is the longest gap (in 20 ms frames) filled in with
// concealment; after a longer one the stream just carries on.
const maxConcealedFrames = 5

// next reports how many packets were lost before seq, and whether seq is
// late (already passed: a duplicate or reordered packet, to drop).
func (r *rxSequence) next(seq uint16) (lost int, late bool) {
	if !r.started {
		r.started, r.last = true, seq
		return 0, false
	}
	diff := int16(seq - r.last) // wraps around correctly
	if diff <= 0 {
		return 0, true
	}
	r.last = seq
	return int(diff) - 1, false
}
