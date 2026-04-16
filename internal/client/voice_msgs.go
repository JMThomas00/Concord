package client

import (
	"encoding/json"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// VoiceStateUpdateMsg is dispatched when a user joins/leaves/updates voice state in a channel.
type VoiceStateUpdateMsg struct {
	ServerID uuid.UUID
	State    *models.VoiceState // nil ChannelID field = user left voice
}

// VoiceServerUpdateMsg carries the WebRTC connection info the server sends after joining voice.
// This is the trigger to start the local VoiceEngine.
type VoiceServerUpdateMsg struct {
	Payload *protocol.VoiceServerUpdatePayload
}

// VoiceSpeakingMsg is dispatched when a remote user starts or stops speaking.
type VoiceSpeakingMsg struct {
	ServerID   uuid.UUID
	UserID     uuid.UUID
	ChannelID  uuid.UUID
	IsSpeaking bool
}

// VoiceSignalInMsg is dispatched when the server relays a WebRTC SDP/ICE signal
// from another peer. The VoiceEngine consumes this to complete negotiation.
type VoiceSignalInMsg struct {
	FromUserID uuid.UUID
	ChannelID  uuid.UUID
	Type       string          // "offer", "answer", "candidate"
	SDP        string
	Candidate  json.RawMessage
}

// VoiceEngineReadyMsg signals that the local audio engine has initialised successfully.
type VoiceEngineReadyMsg struct{}

// VoiceEngineErrorMsg signals that the local audio engine encountered a fatal error.
type VoiceEngineErrorMsg struct{ Err error }

// VoiceConnectedMsg signals that the local user has successfully joined a voice channel.
type VoiceConnectedMsg struct{ ChannelID uuid.UUID }

// VoiceDisconnectedMsg signals that the local user has left voice (cleanly or due to error).
type VoiceDisconnectedMsg struct{}

// VoicePeerConnectedMsg fires when a WebRTC data channel to a peer opens (audio live).
type VoicePeerConnectedMsg struct{ UserID uuid.UUID }

// VoicePeerDisconnectedMsg fires when a peer connection closes.
type VoicePeerDisconnectedMsg struct{ UserID uuid.UUID }

// VoiceSignalOut is queued by the VoiceEngine when it needs to send a signaling
// message to a remote peer (offer, answer, or ICE candidate).
type VoiceSignalOut struct {
	TargetUserID uuid.UUID
	ChannelID    uuid.UUID
	Type         string          // "offer", "answer", "candidate"
	SDP          string
	Candidate    json.RawMessage
}

// AudioDevice describes a physical audio input or output device.
type AudioDevice struct {
	ID   string
	Name string
}

// voiceLocalSpeakingMsg is sent through voiceEventOut when the local user's
// VAD/PTT speaking state changes. The Update loop converts it to OpVoiceSpeaking.
type voiceLocalSpeakingMsg struct{ speaking bool }

// VoiceQualityMsg is emitted every ~5 s per connected peer with the latest
// ICE round-trip latency. LatencyMs == -1 means no data yet.
type VoiceQualityMsg struct {
	UserID    uuid.UUID
	LatencyMs int
}

// VoiceLevelMsg is emitted ~5× per second per active peer with the current
// RMS output level (0.0 = silence, 1.0 = full scale). Used to drive the
// per-user VU meter in the Members panel.
type VoiceLevelMsg struct {
	UserID uuid.UUID
	Level  float32
}
