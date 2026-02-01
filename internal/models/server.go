package models

import (
	"time"

	"github.com/google/uuid"
)

// Server represents a Concord server (similar to Discord's "guild")
type Server struct {
	ID                   uuid.UUID `json:"id"`
	Name                 string    `json:"name"`
	Description          string    `json:"description,omitempty"`
	IconHash             string    `json:"icon_hash,omitempty"`
	OwnerID              uuid.UUID `json:"owner_id"`
	DefaultChannelID     uuid.UUID `json:"default_channel_id,omitempty"`
	SystemChannelID      uuid.UUID `json:"system_channel_id,omitempty"` // For join/leave messages
	RulesChannelID       uuid.UUID `json:"rules_channel_id,omitempty"`
	MaxMembers           int       `json:"max_members"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
	
	// Server settings
	VerificationLevel    int  `json:"verification_level"`
	ExplicitContentFilter int `json:"explicit_content_filter"`
	
	// Invite settings
	InvitesEnabled       bool `json:"invites_enabled"`
	DefaultInviteMaxAge  int  `json:"default_invite_max_age"`  // In seconds, 0 = never expire
	DefaultInviteMaxUses int  `json:"default_invite_max_uses"` // 0 = unlimited
}

// ServerSettings contains configurable server settings
type ServerSettings struct {
	ServerID                uuid.UUID `json:"server_id"`
	WelcomeMessageEnabled   bool      `json:"welcome_message_enabled"`
	WelcomeMessage          string    `json:"welcome_message,omitempty"`
	LeaveMessageEnabled     bool      `json:"leave_message_enabled"`
	LeaveMessage            string    `json:"leave_message,omitempty"`
	DefaultNotifications    string    `json:"default_notifications"` // "all" or "mentions"
}

// Invite represents a server invite link
type Invite struct {
	Code      string    `json:"code"`
	ServerID  uuid.UUID `json:"server_id"`
	ChannelID uuid.UUID `json:"channel_id,omitempty"`
	InviterID uuid.UUID `json:"inviter_id"`
	MaxAge    int       `json:"max_age"`    // Seconds until expiry, 0 = never
	MaxUses   int       `json:"max_uses"`   // Max uses, 0 = unlimited
	Uses      int       `json:"uses"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	IsRevoked bool      `json:"is_revoked"`
}

// NewServer creates a new server with default settings
func NewServer(name string, ownerID uuid.UUID) *Server {
	now := time.Now()
	return &Server{
		ID:                    uuid.New(),
		Name:                  name,
		OwnerID:               ownerID,
		MaxMembers:            1000, // Default max
		CreatedAt:             now,
		UpdatedAt:             now,
		VerificationLevel:     0,
		ExplicitContentFilter: 0,
		InvitesEnabled:        true,
		DefaultInviteMaxAge:   86400, // 24 hours
		DefaultInviteMaxUses:  0,     // Unlimited
	}
}

// NewServerSettings creates default server settings
func NewServerSettings(serverID uuid.UUID) *ServerSettings {
	return &ServerSettings{
		ServerID:              serverID,
		WelcomeMessageEnabled: true,
		WelcomeMessage:        "Welcome to the server, {user}!",
		LeaveMessageEnabled:   false,
		DefaultNotifications:  "all",
	}
}

// GenerateInvite creates a new invite for the server
func (s *Server) GenerateInvite(inviterID uuid.UUID, channelID uuid.UUID) *Invite {
	now := time.Now()
	invite := &Invite{
		Code:      generateInviteCode(),
		ServerID:  s.ID,
		ChannelID: channelID,
		InviterID: inviterID,
		MaxAge:    s.DefaultInviteMaxAge,
		MaxUses:   s.DefaultInviteMaxUses,
		Uses:      0,
		CreatedAt: now,
	}
	
	if invite.MaxAge > 0 {
		expiresAt := now.Add(time.Duration(invite.MaxAge) * time.Second)
		invite.ExpiresAt = &expiresAt
	}
	
	return invite
}

// generateInviteCode creates a random invite code
func generateInviteCode() string {
	// Generate a short, URL-safe code from UUID
	id := uuid.New()
	// Use first 8 characters of UUID without hyphens
	code := id.String()[:8]
	return code
}

// IsExpired checks if the invite has expired
func (i *Invite) IsExpired() bool {
	if i.IsRevoked {
		return true
	}
	if i.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*i.ExpiresAt)
}

// IsUsable checks if the invite can still be used
func (i *Invite) IsUsable() bool {
	if i.IsExpired() {
		return false
	}
	if i.MaxUses > 0 && i.Uses >= i.MaxUses {
		return false
	}
	return true
}

// Use increments the invite usage count
func (i *Invite) Use() {
	i.Uses++
}

// Revoke marks the invite as revoked
func (i *Invite) Revoke() {
	i.IsRevoked = true
}

// SetDefaultChannel sets the default channel for the server
func (s *Server) SetDefaultChannel(channelID uuid.UUID) {
	s.DefaultChannelID = channelID
	s.UpdatedAt = time.Now()
}

// SetSystemChannel sets the system channel for join/leave messages
func (s *Server) SetSystemChannel(channelID uuid.UUID) {
	s.SystemChannelID = channelID
	s.UpdatedAt = time.Now()
}

// TransferOwnership transfers server ownership to another user
func (s *Server) TransferOwnership(newOwnerID uuid.UUID) {
	s.OwnerID = newOwnerID
	s.UpdatedAt = time.Now()
}

// Update updates server properties
func (s *Server) Update(name, description string) {
	if name != "" {
		s.Name = name
	}
	if description != "" {
		s.Description = description
	}
	s.UpdatedAt = time.Now()
}
