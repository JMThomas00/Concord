//go:build !cgo || novoice

package rnnoise

// Builds without C (-tags novoice, CGO_ENABLED=0) have no RNNoise; the voice
// engine isn't in those builds either. Process leaves audio untouched.

// Available reports whether this build has RNNoise.
const Available = false

// FrameSize is the number of samples RNNoise processes at a time.
const FrameSize = 480

// SampleRate is the only rate RNNoise works at.
const SampleRate = 48000

// Denoiser does nothing in this build.
type Denoiser struct{}

// New returns a Denoiser that does nothing.
func New() *Denoiser { return &Denoiser{} }

// Process leaves frame untouched and reports no speech.
func (d *Denoiser) Process(frame []float32) float32 { return 0 }

// Close does nothing.
func (d *Denoiser) Close() {}
