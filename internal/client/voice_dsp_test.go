package client

import (
	"math"
	"testing"
)

func sine(freq float64, n int, amp float64) []float32 {
	out := make([]float32, n)
	for i := range out {
		out[i] = float32(amp * math.Sin(2*math.Pi*freq*float64(i)/voiceSampleRate))
	}
	return out
}

func rms32(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}

// The low-cut filter removes hum and rumble and leaves speech alone.
func TestHighPass(t *testing.T) {
	for _, c := range []struct {
		freq     float64
		min, max float64 // kept fraction of the level
	}{{30, 0, 0.05}, {50, 0, 0.15}, {60, 0, 0.25}, {300, 0.95, 1.02}, {1000, 0.99, 1.01}} {
		f := newLowCut(80, voiceSampleRate)
		x := sine(c.freq, voiceSampleRate, 10000)
		f.process(x)
		kept := rms32(x[voiceSampleRate/2:]) / (10000 / math.Sqrt2)
		if kept < c.min || kept > c.max {
			t.Errorf("%v Hz: kept %.2f of the level, want %.2f–%.2f", c.freq, kept, c.min, c.max)
		}
	}
}

// Quiet speech is brought up, loud speech down, within ±12 dB; silence
// between words doesn't change the gain; nothing clips.
func TestAutoLevel(t *testing.T) {
	quiet, loud := newAutoLevel(), newAutoLevel()
	for i := 0; i < 500; i++ {
		quiet.process(sine(200, 960, 300), true)
		loud.process(sine(200, 960, 30000), true)
	}
	if quiet.gain < 3.9 || loud.gain > 0.26 {
		t.Fatalf("gains: quiet %.2f, loud %.2f", quiet.gain, loud.gain)
	}
	before := quiet.gain
	quiet.process(sine(200, 960, 50), false) // a pause
	if quiet.gain != before {
		t.Fatal("the gain moved on a frame that isn't speech")
	}
	hot := newAutoLevel()
	hot.gain = 4
	x := sine(200, 960, 20000)
	hot.process(x, false)
	for _, s := range x {
		if s > 32767 || s < -32768 {
			t.Fatalf("clipped: %v", s)
		}
	}
}

func TestMixDenoised(t *testing.T) {
	den, orig := []float32{0, 0}, []float32{100, -100}
	mixDenoised(den, orig, 0.5)
	if den[0] != 25 || den[1] != -25 {
		t.Fatalf("50%%: %v", den)
	}
	den = []float32{0}
	mixDenoised(den, []float32{100}, 1)
	if den[0] != 0 {
		t.Fatalf("100%%: %v", den)
	}
	if speechThreshold(0.78) > 0.35 || speechThreshold(0) != 0.95 || speechThreshold(1) != 0.1 {
		t.Fatalf("thresholds %v %v %v", speechThreshold(0.78), speechThreshold(0), speechThreshold(1))
	}
}

func TestVoicePackets(t *testing.T) {
	buf := make([]byte, 8)
	n := voicePacketHeader(buf, 0xBEEF)
	copy(buf[n:], "opus")
	payload, seq, hasSeq, ok := parseVoicePacket(buf[:n+4])
	if !ok || !hasSeq || seq != 0xBEEF || string(payload) != "opus" {
		t.Fatalf("new format: %q %x %v %v", payload, seq, hasSeq, ok)
	}
	payload, _, hasSeq, ok = parseVoicePacket([]byte{opusFrameTag, 1, 2})
	if !ok || hasSeq || len(payload) != 2 {
		t.Fatal("the older format isn't accepted")
	}
	if _, _, _, ok := parseVoicePacket([]byte{0x00, 1, 2, 3}); ok {
		t.Fatal("an unknown tag was accepted")
	}
}

func TestRxSequence(t *testing.T) {
	var r rxSequence
	steps := []struct {
		seq  uint16
		lost int
		late bool
	}{
		{65533, 0, false},
		{65534, 0, false},
		{1, 2, false}, // wrapped, 65535 and 0 lost
		{1, 0, true},  // duplicate
		{0, 0, true},  // arrived too late
		{2, 0, false},
	}
	for i, s := range steps {
		lost, late := r.next(s.seq)
		if lost != s.lost || late != s.late {
			t.Fatalf("step %d (seq %d): lost %d late %v, want %d %v", i, s.seq, lost, late, s.lost, s.late)
		}
	}
}

// Settings from before RNNoise switch noise suppression on, once, at a
// strength that removes most noise; later changes stick.
func TestAudioProcessingMigration(t *testing.T) {
	c := defaultAudioConfig(AudioConfig{NoiseSuppress: false, NoiseSuppressStrength: 0.3})
	if !c.NoiseSuppress || c.NoiseSuppressStrength != 0.8 || c.ProcessingVersion != 1 {
		t.Fatalf("migrated: %+v", c)
	}
	c.NoiseSuppress, c.NoiseSuppressStrength = false, 0.4
	c = defaultAudioConfig(c)
	if c.NoiseSuppress || c.NoiseSuppressStrength != 0.4 {
		t.Fatal("a choice made after the migration was overridden")
	}
	if bitrateForPreset("") != 48000 {
		t.Fatal("the default bitrate isn't 48 kbps")
	}
}
