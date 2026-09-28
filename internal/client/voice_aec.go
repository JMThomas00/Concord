package client

import "sync"

// Echo cancellation: this client's own speaker output (the "far-end" signal,
// i.e. decoded audio from remote peers) can acoustically or electrically leak
// back into this client's own microphone capture (the "near-end" signal),
// which peers then hear as their own voice echoed back a few hundred
// milliseconds later. aecFilter removes that predictable component before a
// frame is encoded and sent.
//
// It's a Normalized-LMS (NLMS) adaptive FIR filter: rather than being told
// the acoustic delay and impulse response of the echo path (speaker → room/
// headset → mic), it continuously adapts its own tap weights to predict the
// near-end sample from recent far-end history, then subtracts that
// prediction. Because its taps span a window long enough to cover any
// plausible delay, NLMS naturally concentrates weight at whichever lag(s)
// actually correlate -- it discovers the delay adaptively instead of needing
// it measured or configured.

const (
	// aecTailMs is the span of far-end history the filter can model. It needs
	// to cover the full round-trip: capture + playback device buffering
	// (roughly 3 periods each, per the malgo period-size fix in Start()) plus
	// any acoustic delay. 200ms is generous headroom for both.
	aecTailMs = 200
	// aecMaxTaps bounds the filter length (and so its O(taps) per-sample
	// cost) regardless of sample rate -- at the "ultra" 48kHz preset,
	// aecTailMs alone would want 9600 taps; capping it trades a shorter
	// effective tail (~125ms at 48kHz) for a bounded, real-time-safe amount
	// of per-frame computation.
	aecMaxTaps = 6000

	// aecMinEnergy gates NLMS adaptation on the far-end window actually
	// containing signal. Skipping updates during far-end silence (rather
	// than normalizing by a near-zero energy with a small epsilon) avoids
	// the classic NLMS instability of dividing by ~0 and sending the taps to
	// garbage when nothing was played. A single full-scale int16 sample
	// contributes ~1.07e9 to the energy sum; true silence (quantization
	// noise only) sits many orders of magnitude below this threshold.
	aecMinEnergy = 1e6
)

// aecFarEndBuffer is a small history of recently-played PCM samples, used as
// the far-end reference for echo cancellation. mixPCM pushes into it on every
// playback callback; sendFrame (capture side) reads a snapshot to use as the
// filter's delay line. The two run on different malgo callback goroutines, so
// access is mutex-guarded.
type aecFarEndBuffer struct {
	mu       sync.Mutex
	data     []int16
	capacity int
}

func newAECFarEndBuffer(capacity int) *aecFarEndBuffer {
	return &aecFarEndBuffer{capacity: capacity}
}

// push appends newly-played samples, trimming from the front to stay within capacity.
func (b *aecFarEndBuffer) push(samples []int16) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, samples...)
	if len(b.data) > b.capacity {
		b.data = b.data[len(b.data)-b.capacity:]
	}
}

// snapshot returns a copy of the most recent min(n, available) samples, oldest first.
func (b *aecFarEndBuffer) snapshot(n int) []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if n > len(b.data) {
		n = len(b.data)
	}
	if n <= 0 {
		return nil
	}
	out := make([]int16, n)
	copy(out, b.data[len(b.data)-n:])
	return out
}

// aecFilter is the adaptive NLMS canceller itself. Not safe for concurrent
// use -- owned exclusively by the processCapture goroutine, same as
// VoiceEngine.enc.
type aecFilter struct {
	taps []float64
}

func newAECFilter(numTaps int) *aecFilter {
	return &aecFilter{taps: make([]float64, numTaps)}
}

// process runs the filter over one near-end frame and returns the
// echo-cancelled result (a new slice; near is not modified). farHistory must
// be oldest-first PCM ending at approximately the same moment as near's last
// sample (i.e. "just played" aligned with "just captured"); the two malgo
// devices share the same period size so this holds to within one callback's
// slop, which the filter's tail comfortably absorbs. If farHistory is shorter
// than one full tap-window plus the frame (e.g. right at call start, before
// any peer audio has played), near is returned unmodified -- there's nothing
// yet to have echoed, and filtering against a mostly-zero window would only
// waste adaptation cycles.
//
// mu is the NLMS step size (0 < mu < ~1 for stability; higher adapts faster
// but tolerates less noise before mis-adapting) -- passed in per call rather
// than stored, so a live strength-slider change takes effect on the very next
// frame without needing to synchronize a field across goroutines.
func (f *aecFilter) process(near []int16, farHistory []int16, mu float64) []int16 {
	L := len(f.taps)
	need := len(near) + L - 1
	if len(farHistory) < need {
		return near
	}
	base := len(farHistory) - len(near)
	out := make([]int16, len(near))

	for j, nSample := range near {
		// The L most recent far-end samples as of this near-end sample,
		// oldest first; window[L-1] is lag 0 (same moment as nSample).
		end := base + j + 1
		window := farHistory[end-L : end]

		var estimate, energy float64
		for k := 0; k < L; k++ {
			fk := float64(window[L-1-k])
			estimate += f.taps[k] * fk
			energy += fk * fk
		}

		x := float64(nSample)
		e := x - estimate // residual after removing the predicted echo

		if energy > aecMinEnergy {
			norm := mu * e / energy
			for k := 0; k < L; k++ {
				f.taps[k] += norm * float64(window[L-1-k])
			}
		}

		if e > 32767 {
			e = 32767
		} else if e < -32768 {
			e = -32768
		}
		out[j] = int16(e)
	}
	return out
}

// aecTapsForSampleRate picks the filter length for a given sample rate,
// bounded by aecMaxTaps.
func aecTapsForSampleRate(sampleRate uint32) int {
	taps := int(sampleRate) * aecTailMs / 1000
	if taps > aecMaxTaps {
		taps = aecMaxTaps
	}
	return taps
}

// aecMuForStrength maps the 0.0-1.0 "Echo Cancellation Strength" UI control
// to an NLMS step size. The range (0.1-0.6) stays well inside the stable
// 0 < mu < ~1 band for this normalization while still giving a meaningful
// difference in adaptation speed across the slider.
func aecMuForStrength(strength float64) float64 {
	return 0.1 + clampF(strength, 0.0, 1.0)*0.5
}
