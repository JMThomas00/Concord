//go:build cgo && !novoice

// Package rnnoise wraps Xiph's RNNoise (https://github.com/xiph/rnnoise,
// v0.1.1, BSD-3-Clause, see COPYING): a small recurrent neural network
// that removes background noise from speech (fans, hum, keyboards, the
// room) while someone talks, and estimates how likely each frame is to be
// speech. Concord's voice engine runs it on the microphone before encoding.
//
// The C files here are RNNoise's own, unchanged apart from a build
// constraint line at the top of each, so builds without C (-tags novoice,
// or CGO_ENABLED=0) skip them and get rnnoise_stub.go instead.
package rnnoise

/*
#cgo CFLAGS: -O2 -DRNNOISE_EXPORT= -w
#cgo !windows LDFLAGS: -lm
#include <stdlib.h>
#include "rnnoise.h"
*/
import "C"

import "unsafe"

// Available reports whether this build has RNNoise.
const Available = true

// FrameSize is the number of samples RNNoise processes at a time: 10 ms of
// mono audio at 48 kHz.
const FrameSize = 480

// SampleRate is the only rate RNNoise works at.
const SampleRate = 48000

// Denoiser is one stream's RNNoise state. Not safe for concurrent use.
type Denoiser struct {
	st      *C.DenoiseState
	in, out *C.float
}

// New makes a Denoiser with RNNoise's built-in model.
func New() *Denoiser {
	size := C.size_t(FrameSize) * C.size_t(unsafe.Sizeof(C.float(0)))
	return &Denoiser{
		st:  C.rnnoise_create(nil),
		in:  (*C.float)(C.malloc(size)),
		out: (*C.float)(C.malloc(size)),
	}
}

// Process denoises one FrameSize frame in place and returns the
// probability (0–1) that it's speech. Samples are on the int16 scale
// (-32768 to 32767), as RNNoise expects, not -1 to 1.
func (d *Denoiser) Process(frame []float32) float32 {
	if d == nil || d.st == nil || len(frame) < FrameSize {
		return 0
	}
	in := unsafe.Slice(d.in, FrameSize)
	out := unsafe.Slice(d.out, FrameSize)
	for i := range in {
		in[i] = C.float(frame[i])
	}
	vad := C.rnnoise_process_frame(d.st, d.out, d.in)
	for i := range out {
		frame[i] = float32(out[i])
	}
	return float32(vad)
}

// Close frees the Denoiser.
func (d *Denoiser) Close() {
	if d == nil || d.st == nil {
		return
	}
	C.rnnoise_destroy(d.st)
	C.free(unsafe.Pointer(d.in))
	C.free(unsafe.Pointer(d.out))
	d.st = nil
}
