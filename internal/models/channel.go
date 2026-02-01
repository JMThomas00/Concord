package models

import (
	"time"

	"github.com/google/uuid"
)

// ChannelType represents the type of channel
type ChannelType int

const (
	ChannelTypeText     ChannelType = iota // Text chat channel
	ChannelTypeVoice                       // Voice channel (v2)
	ChannelTypeCategory                    // Channel category/folder
	ChannelTypeDM                          // Direct message
	ChannelTypeGroupDM                     // Group direct message
)

// Channel represents a communication channel within a server
type Channel struct {
	ID          uuid.UUID   `json:"id"`
	ServerID    uuid.UUID   `json:"server_id,omitempty"` // Empty for DMs
	Name        string      `json:"name"`
	Topic       string      `json:"topic,omitempty"`
	Type        ChannelType `json:"type"`
	Position    int         `json:"position"`
	CategoryID  uuid.UUID   `json:"category_id,omitempty"` // Parent category
	IsNSFW      bool        `json:"is_nsfw"`
	RateLimitPerUser int    `json:"rate_limit_per_user,omitempty"` // Slowmode in seconds
	CreatedAt   time.Time   `json:"created_at"`
	UpdatedAt   time.Time   `json:"updated_at"`
	
	// Permission overwrites for roles/users
	PermissionOverwrites []PermissionOverwrite `json:"permission_overwrites,omitempty"`
	
	// For DM channels
	RecipientIDs []uuid.UUID `json:"recipient_ids,omitempty"`
}

// PermissionOverwrite allows/denies specific permissions for a role or user
type PermissionOverwrite struct {
	ID    uuid.UUID `json:"id"`              // Role or User ID
	Type  string    `json:"type"`            // "role" or "member"
	Allow int64     `json:"allow"`           // Allowed permission bits
	Deny  int64     `json:"deny"`            // Denied permission bits
}

// NewTextChannel creates a new text channel
func NewTextChannel(serverID uuid.UUID, name string) *Channel {
	now := time.Now()
	return &Channel{
		ID:        uuid.New(),
		ServerID:  serverID,
		Name:      name,
		Type:      ChannelTypeText,
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewVoiceChannel creates a new voice channel
func NewVoiceChannel(serverID uuid.UUID, name string) *Channel {
	now := time.Now()
	return &Channel{
		ID:        uuid.New(),
		ServerID:  serverID,
		Name:      name,
		Type:      ChannelTypeVoice,
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewCategory creates a new channel category
func NewCategory(serverID uuid.UUID, name string) *Channel {
	now := time.Now()
	return &Channel{
		ID:        uuid.New(),
		ServerID:  serverID,
		Name:      name,
		Type:      ChannelTypeCategory,
		Position:  0,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// NewDMChannel creates a new direct message channel between users
func NewDMChannel(userIDs ...uuid.UUID) *Channel {
	now := time.Now()
	return &Channel{
		ID:           uuid.New(),
		Name:         "", // DM channels don't have names
		Type:         ChannelTypeDM,
		CreatedAt:    now,
		UpdatedAt:    now,
		RecipientIDs: userIDs,
	}
}

// IsTextBased returns true if the channel supports text messages
func (c *Channel) IsTextBased() bool {
	return c.Type == ChannelTypeText || c.Type == ChannelTypeDM || c.Type == ChannelTypeGroupDM
}

// IsVoiceBased returns true if the channel supports voice
func (c *Channel) IsVoiceBased() bool {
	return c.Type == ChannelTypeVoice
}

// IsDM returns true if this is a direct message channel
func (c *Channel) IsDM() bool {
	return c.Type == ChannelTypeDM || c.Type == ChannelTypeGroupDM
}

// SetCategory sets the parent category for this channel
func (c *Channel) SetCategory(categoryID uuid.UUID) {
	c.CategoryID = categoryID
	c.UpdatedAt = time.Now()
}

// AddPermissionOverwrite adds or updates a permission overwrite
func (c *Channel) AddPermissionOverwrite(overwrite PermissionOverwrite) {
	for i, ow := range c.PermissionOverwrites {
		if ow.ID == overwrite.ID {
			c.PermissionOverwrites[i] = overwrite
			c.UpdatedAt = time.Now()
			return
		}
	}
	c.PermissionOverwrites = append(c.PermissionOverwrites, overwrite)
	c.UpdatedAt = time.Now()
}

// RemovePermissionOverwrite removes a permission overwrite
func (c *Channel) RemovePermissionOverwrite(id uuid.UUID) {
	for i, ow := range c.PermissionOverwrites {
		if ow.ID == id {
			c.PermissionOverwrites = append(c.PermissionOverwrites[:i], c.PermissionOverwrites[i+1:]...)
			c.UpdatedAt = time.Now()
			return
		}
	}
}
