package models

import (
	"time"

	"github.com/google/uuid"
)

// VoiceState tracks a user's current voice channel membership and state.
// IsSpeaking is ephemeral (not persisted); all other fields are stored in voice_states.
type VoiceState struct {
	UserID           uuid.UUID `json:"user_id"`
	ServerID         uuid.UUID `json:"server_id"`
	ChannelID        uuid.UUID `json:"channel_id"`
	IsSelfMuted      bool      `json:"is_self_muted"`
	IsSelfDeafened   bool      `json:"is_self_deafened"`
	IsServerMuted    bool      `json:"is_server_muted"`
	IsServerDeafened bool      `json:"is_server_deafened"`
	IsSpeaking       bool      `json:"is_speaking"` // ephemeral, not persisted
	JoinedAt         time.Time `json:"joined_at"`
}
