package client

import (
	"math"
	"math/rand"
	"testing"
)

// TestAECFilterPassthroughWithoutHistory verifies process() returns the
// near-end frame unchanged when there isn't yet enough far-end history to
// fill the tap window (e.g. right at call start, before any peer audio has
// played) -- it must not filter against a mostly-zero window.
func TestAECFilterPassthroughWithoutHistory(t *testing.T) {
	f := newAECFilter(64)
	near := []int16{100, -200, 300, -400}
	short := make([]int16, 10) // far shorter than len(near)+64-1
	out := f.process(near, short, 0.3)
	for i := range near {
		if out[i] != near[i] {
			t.Fatalf("expected passthrough at index %d: got %d, want %d", i, out[i], near[i])
		}
	}
}

// TestAECFilterConvergesOnLinearEcho models a simple echo path -- the near
// end is a delayed, attenuated copy of the far end, as if this client's own
// playback were leaking into its own mic. It feeds many frames through the
// filter and asserts the residual (post-cancellation) energy drops sharply
// relative to the raw near-end energy once the NLMS taps have had time to
// adapt. This is the only practical way to verify the DSP math in an
// environment with no real audio hardware to listen with.
func TestAECFilterConvergesOnLinearEcho(t *testing.T) {
	const (
		sampleRate    = 8000
		frameMs       = 20                          // matches voiceFrameMs in voice_engine.go, duplicated here so
		periodSamples = sampleRate * frameMs / 1000 // this test compiles under -tags novoice too (no CGO dependency)
		numTaps       = 1600                        // aecTapsForSampleRate(8000)
		delaySamples  = 200                         // within the tap window, plausible device-buffering delay
		echoGain      = 0.6
		numFrames     = 300
		mu            = 0.3
	)

	if got := aecTapsForSampleRate(sampleRate); got != numTaps {
		t.Fatalf("test assumption drifted: aecTapsForSampleRate(%d) = %d, want %d", sampleRate, got, numTaps)
	}

	total := numFrames * periodSamples

	// Broadband pseudo-random far-end signal. NLMS needs a "persistently
	// exciting" reference to uniquely identify a delay+gain -- a pure tone
	// is rank-deficient and can't do this, so a single sine wouldn't
	// actually validate the filter.
	rng := rand.New(rand.NewSource(1))
	farEnd := make([]int16, total)
	for i := range farEnd {
		farEnd[i] = int16(rng.Intn(20001) - 10000) // roughly ±10000, well under clipping
	}

	// Near end = delayed, attenuated copy of far end (pure echo, no
	// independent near-end "voice" component) -- isolates exactly what the
	// filter is meant to remove.
	nearEnd := make([]int16, total)
	for i := delaySamples; i < total; i++ {
		nearEnd[i] = int16(echoGain * float64(farEnd[i-delaySamples]))
	}

	buf := newAECFarEndBuffer(numTaps + periodSamples)
	filt := newAECFilter(numTaps)

	var (
		earlyResidualEnergy, earlyOriginalEnergy float64
		lateResidualEnergy, lateOriginalEnergy   float64
	)
	// Treat the first 40% of frames as adaptation warm-up (not counted) and
	// the last 20% as "converged" (measured) -- middle frames are just
	// filler so the tail has plenty of adaptation time behind it.
	warmupFrames := int(float64(numFrames) * 0.4)
	measureFromFrame := int(float64(numFrames) * 0.8)

	for i := 0; i < numFrames; i++ {
		start := i * periodSamples
		end := start + periodSamples
		farFrame := farEnd[start:end]
		nearFrame := nearEnd[start:end]

		buf.push(farFrame)
		history := buf.snapshot(periodSamples + numTaps - 1)
		out := filt.process(nearFrame, history, mu)

		if i < warmupFrames {
			continue
		}
		origE := energyOf(nearFrame)
		resE := energyOf(out)
		if i < measureFromFrame {
			earlyOriginalEnergy += origE
			earlyResidualEnergy += resE
		} else {
			lateOriginalEnergy += origE
			lateResidualEnergy += resE
		}
	}

	if lateOriginalEnergy == 0 {
		t.Fatal("test bug: no near-end energy recorded in the measured window")
	}
	lateRatio := lateResidualEnergy / lateOriginalEnergy
	t.Logf("residual/original energy ratio: mid-run=%.4f, converged tail=%.4f",
		earlyResidualEnergy/earlyOriginalEnergy, lateRatio)

	// A correctly-adapting NLMS filter on a purely linear synthetic echo
	// path should remove the great majority of the echo's energy by the end
	// of 300 frames (6s of audio). 0.1 (90% energy reduction, ~10dB) is a
	// conservative bar well clear of "barely doing anything" while not
	// demanding the near-perfect cancellation only a noiseless synthetic
	// test could ever achieve.
	const maxResidualRatio = 0.1
	if lateRatio > maxResidualRatio {
		t.Fatalf("echo not adequately cancelled by convergence: residual/original energy = %.4f, want <= %.2f",
			lateRatio, maxResidualRatio)
	}
}

func energyOf(samples []int16) float64 {
	var sum float64
	for _, s := range samples {
		f := float64(s)
		sum += f * f
	}
	return sum
}

// TestAECMuForStrength sanity-checks the strength-to-step-size mapping stays
// within the stable range documented on aecMuForStrength.
func TestAECMuForStrength(t *testing.T) {
	cases := []struct{ strength, wantMin, wantMax float64 }{
		{-1.0, 0.1, 0.1}, // clamped
		{0.0, 0.1, 0.1},
		{0.5, 0.34, 0.36},
		{1.0, 0.6, 0.6},
		{2.0, 0.6, 0.6}, // clamped
	}
	for _, c := range cases {
		got := aecMuForStrength(c.strength)
		if got < c.wantMin-1e-9 || got > c.wantMax+1e-9 {
			t.Errorf("aecMuForStrength(%v) = %v, want in [%v, %v]", c.strength, got, c.wantMin, c.wantMax)
		}
	}
}

// TestAECFarEndBufferTrimsToCapacity verifies the ring buffer keeps only the
// most recent `capacity` samples, oldest-first, matching what aecFilter.process
// assumes about its farHistory argument.
func TestAECFarEndBufferTrimsToCapacity(t *testing.T) {
	buf := newAECFarEndBuffer(10)
	for i := 0; i < 3; i++ {
		buf.push([]int16{int16(i*10 + 1), int16(i*10 + 2), int16(i*10 + 3), int16(i*10 + 4)})
	}
	// Pushed 12 samples total into a capacity-10 buffer -- expect the last 10.
	got := buf.snapshot(20) // request more than available; should clamp
	if len(got) != 10 {
		t.Fatalf("expected snapshot length 10, got %d", len(got))
	}
	want := []int16{3, 4, 11, 12, 13, 14, 21, 22, 23, 24}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("snapshot[%d] = %d, want %d (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestAECFilterOutputNeverClips(t *testing.T) {
	f := newAECFilter(32)
	buf := newAECFarEndBuffer(64)
	rng := rand.New(rand.NewSource(2))
	for frame := 0; frame < 50; frame++ {
		far := make([]int16, 16)
		near := make([]int16, 16)
		for i := range far {
			far[i] = int16(rng.Intn(65535) - 32768)
			near[i] = int16(rng.Intn(65535) - 32768)
		}
		buf.push(far)
		history := buf.snapshot(16 + 32 - 1)
		out := f.process(near, history, 0.8)
		for _, v := range out {
			if math.Abs(float64(v)) > 32768 {
				t.Fatalf("output sample out of int16 range: %d", v)
			}
		}
	}
}
