package models

import (
	"time"

	"github.com/google/uuid"
)

// MessageRetentionPolicy defines how long messages are kept in a channel
type MessageRetentionPolicy struct {
	ID                      uuid.UUID
	ServerID                uuid.UUID
	ChannelID               *uuid.UUID // NULL = server default
	TimeRetentionDays       *int       // NULL = disabled
	SystemTimeRetentionDays *int       // NULL = disabled (separate for system messages)
	MaxMessageCount         *int       // NULL = disabled
	PreservePinned          bool       // Always true - pinned messages never auto-deleted
	CreatedAt               time.Time
	UpdatedAt               time.Time
	CreatedBy               uuid.UUID
}

// NewRetentionPolicy creates a new retention policy with defaults
func NewRetentionPolicy(serverID, createdBy uuid.UUID) *MessageRetentionPolicy {
	now := time.Now()
	return &MessageRetentionPolicy{
		ID:             uuid.New(),
		ServerID:       serverID,
		ChannelID:      nil, // Server default
		PreservePinned: true,
		CreatedAt:      now,
		UpdatedAt:      now,
		CreatedBy:      createdBy,
	}
}

// MessagePruneHistory records a pruning operation for audit/transparency
type MessagePruneHistory struct {
	ID              uuid.UUID
	ServerID        uuid.UUID
	ChannelID       *uuid.UUID // NULL = server-wide prune
	MessagesDeleted int
	TimeBasedCount  int
	CountBasedCount int
	TriggerType     string // "manual" or "automatic"
	TriggeredBy     *uuid.UUID
	ExecutedAt      time.Time
	DurationMs      int64
}

// PruneStats contains statistics from a pruning operation
type PruneStats struct {
	ChannelID         uuid.UUID
	TimeBasedDeleted  int
	CountBasedDeleted int
	TotalDeleted      int
	DurationMs        int64
}
