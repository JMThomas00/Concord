//go:build novoice

// This file provides no-op stubs for the VoiceEngine when the application is
// built WITHOUT the "voice" build tag (the default).
//
// To enable real audio support, rebuild with CGO and a C compiler:
//
//	go build -tags voice -o build/concord-client.exe ./cmd/client
//
// Requirements for -tags voice:
//   - C compiler (GCC/Clang/MSVC) in PATH
//   - CGO_ENABLED=1 (the default when a C compiler is present)
//   - Windows: WASAPI (built-in since Windows Vista)
//   - Linux: PulseAudio or PipeWire dev headers
//   - macOS: CoreAudio (built-in)

package client

import (
	"errors"

	"github.com/google/uuid"
)

// errVoiceNotAvailable is returned by stub methods when the application was
// compiled without voice support.
var errVoiceNotAvailable = errors.New("voice not available — rebuild with: go build -tags voice ./cmd/client")

// VoiceEngine is a no-op placeholder. The real implementation lives in
// voice_engine.go and requires the "voice" build tag.
type VoiceEngine struct{}

// NewVoiceEngine returns a stub engine that cannot capture or play audio.
func NewVoiceEngine(_ AudioConfig, _ uuid.UUID, _ chan<- VoiceSignalOut, _ chan<- interface{}) *VoiceEngine {
	return &VoiceEngine{}
}

// Start always returns errVoiceNotAvailable in the stub build.
func (e *VoiceEngine) Start(_, _ uuid.UUID, _ []string) error {
	return errVoiceNotAvailable
}

// Stop is a no-op in the stub build.
func (e *VoiceEngine) Stop() {}

// AddPeer is a no-op in the stub build.
func (e *VoiceEngine) AddPeer(_ uuid.UUID) {}

// RemovePeer is a no-op in the stub build.
func (e *VoiceEngine) RemovePeer(_ uuid.UUID) {}

// HandleSignal is a no-op in the stub build.
func (e *VoiceEngine) HandleSignal(_ uuid.UUID, _, _ string, _ []byte) {}

// SetUserVolume is a no-op in the stub build.
func (e *VoiceEngine) SetUserVolume(_ uuid.UUID, _ float64) {}

// UpdateConfig is a no-op in the stub build.
func (e *VoiceEngine) UpdateConfig(_ AudioConfig) {}

// TogglePTT is a no-op in the stub build.
func (e *VoiceEngine) TogglePTT() {}

// ListDevices delegates to GetAudioDevices so the settings picker shows real
// devices even without the voice build tag. GetAudioDevices is defined in
// wasapi_devices_windows.go (Windows) or wasapi_devices_other.go (other OS).
func (e *VoiceEngine) ListDevices() (inputs, outputs []AudioDevice, err error) {
	return GetAudioDevices()
}

// isVoiceSupported returns false in the stub build — no audio engine available.
func isVoiceSupported() bool { return false }
