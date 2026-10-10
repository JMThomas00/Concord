//go:build !novoice

package client

import (
	"math"
	"math/rand"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/google/uuid"
	"github.com/hraban/opus"

	"github.com/concord-chat/concord/internal/rnnoise"
)

// testEngine is a VoiceEngine with the encoder and processing chain set up
// as Start does, but no audio devices or peers.
func testEngine(t *testing.T, cfg AudioConfig) *VoiceEngine {
	t.Helper()
	e := NewVoiceEngine(cfg, uuid.New(), make(chan VoiceSignalOut, 8), make(chan interface{}, 64))
	enc, err := opus.NewEncoder(voiceSampleRate, voiceChannels, opusAppVoIP)
	if err != nil {
		t.Fatal(err)
	}
	_ = enc.SetBitrate(bitrateForPreset(""))
	_ = enc.SetInBandFEC(true)
	_ = enc.SetPacketLossPerc(opusFECLossPercent)
	e.enc = enc
	e.sampleRate.Store(voiceSampleRate)
	e.lowCut = newLowCut(80, voiceSampleRate)
	e.denoiser = rnnoise.New()
	e.leveler = newAutoLevel()
	t.Cleanup(e.denoiser.Close)
	return e
}

const frame20ms = voiceSampleRate * voiceFrameMs / 1000

func noiseFrame(r *rand.Rand, amp float64) []byte {
	s := make([]int16, frame20ms)
	for i := range s {
		s[i] = int16(r.NormFloat64() * amp)
	}
	return int16ToBytes(s)
}

func localLevel(e *VoiceEngine) float32 {
	v := atomic.LoadUint32(&e.localLevel)
	return *(*float32)(unsafe.Pointer(&v))
}

// With noise suppression on, steady background noise leaves the
// microphone chain far quieter than it went in.
func TestNoiseSuppressionInTheChain(t *testing.T) {
	level := func(ns bool) float32 {
		e := testEngine(t, defaultAudioConfig(AudioConfig{NoiseSuppress: ns, NoiseSuppressStrength: 1, AutoLevelOff: true, ProcessingVersion: 1}))
		r := rand.New(rand.NewSource(7))
		var sum float32
		for i := 0; i < 150; i++ {
			e.sendFrame(noiseFrame(r, 1500))
			if i >= 100 {
				sum += localLevel(e)
			}
		}
		return sum / 50
	}
	off, on := level(false), level(true)
	if on > off/4 {
		t.Fatalf("noise level %.4f with suppression vs %.4f without", on, off)
	}
	t.Logf("noise level: %.4f without suppression, %.4f with", off, on)
}

// Lost packets are filled in (FEC for the last one, concealment before it),
// so the receiver's audio keeps its length; duplicates are dropped; packets
// from clients without sequence numbers still play.
func TestPacketLossIsConcealed(t *testing.T) {
	sender := testEngine(t, defaultAudioConfig(AudioConfig{}))
	recv := testEngine(t, defaultAudioConfig(AudioConfig{}))
	peer := uuid.New()
	recv.touchIncoming(peer)

	var packets [][]byte
	for i := 0; i < 12; i++ {
		pcm := make([]int16, frame20ms)
		for j := range pcm {
			pcm[j] = int16(8000 * math.Sin(2*math.Pi*220*float64(i*frame20ms+j)/voiceSampleRate))
		}
		sender.txSeq++
		buf := make([]byte, maxOpusPacketBytes+3)
		h := voicePacketHeader(buf, sender.txSeq)
		n, err := sender.enc.Encode(pcm, buf[h:])
		if err != nil {
			t.Fatal(err)
		}
		packets = append(packets, buf[:h+n])
	}
	buf := recv.incoming[peer]
	got := 0
	play := func() { got += len(buf.data); buf.pop(len(buf.data)) } // as the speakers would
	for i, p := range packets {
		if i == 4 || i == 7 || i == 8 { // lost in transit
			continue
		}
		recv.onAudioData(peer, p)
		if i == 5 {
			recv.onAudioData(peer, p) // a duplicate
		}
		play()
	}
	if got != 12*frame20ms {
		t.Fatalf("%d samples received, want %d (12 frames, losses concealed, duplicate dropped)", got, 12*frame20ms)
	}

	// The older packet format: tag, then Opus, no sequence number.
	old := append([]byte{opusFrameTag}, packets[0][3:]...)
	before := len(buf.data)
	recv.onAudioData(peer, old)
	if len(buf.data) != before+frame20ms {
		t.Fatal("a packet without a sequence number didn't play")
	}
}
