package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// UserStatus represents the online status of a user
type UserStatus string

const (
	StatusOnline  UserStatus = "online"
	StatusIdle    UserStatus = "idle"
	StatusDND     UserStatus = "dnd"     // Do Not Disturb
	StatusOffline UserStatus = "offline"
)

// User represents a Concord user
type User struct {
	ID           uuid.UUID  `json:"id"`
	Username     string     `json:"username"`
	Discriminator string    `json:"discriminator"` // 4-digit number like Discord's old system
	DisplayName  string     `json:"display_name,omitempty"`
	Email        string     `json:"email,omitempty"`
	PasswordHash string     `json:"-"` // Never serialize
	AvatarHash   string     `json:"avatar_hash,omitempty"`
	Status       UserStatus `json:"status"`
	StatusText   string     `json:"status_text,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	LastSeenAt   time.Time  `json:"last_seen_at,omitempty"`
	IsBot        bool       `json:"is_bot"`
}

// NewUser creates a new user with a generated UUID
func NewUser(username, email string) *User {
	now := time.Now()
	return &User{
		ID:            uuid.New(),
		Username:      username,
		Discriminator: generateDiscriminator(),
		Email:         email,
		Status:        StatusOffline,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
}

// FullUsername returns the username with discriminator (e.g., "user#1234")
func (u *User) FullUsername() string {
	return u.Username + "#" + u.Discriminator
}

// GetDisplayName returns the display name if set, otherwise the username
func (u *User) GetDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

// SetOnline updates the user's status to online
func (u *User) SetOnline() {
	u.Status = StatusOnline
	u.LastSeenAt = time.Now()
	u.UpdatedAt = time.Now()
}

// SetOffline updates the user's status to offline
func (u *User) SetOffline() {
	u.Status = StatusOffline
	u.LastSeenAt = time.Now()
	u.UpdatedAt = time.Now()
}

// generateDiscriminator creates a random 4-digit discriminator
func generateDiscriminator() string {
	// Use UUID to generate random bytes and convert to 4-digit number
	id := uuid.New()
	num := int(id[0])<<8 | int(id[1])
	num = num % 10000
	return fmt.Sprintf("%04d", num)
}

// ServerMember represents a user's membership in a server
type ServerMember struct {
	UserID    uuid.UUID   `json:"user_id"`
	ServerID  uuid.UUID   `json:"server_id"`
	Nickname  string      `json:"nickname,omitempty"`
	RoleIDs   []uuid.UUID `json:"role_ids"`
	JoinedAt  time.Time   `json:"joined_at"`
	IsMuted   bool        `json:"is_muted"`
	IsDeafened bool       `json:"is_deafened"`
}

// NewServerMember creates a new server membership
func NewServerMember(userID, serverID uuid.UUID) *ServerMember {
	return &ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{},
		JoinedAt: time.Now(),
	}
}

// HasRole checks if the member has a specific role
func (m *ServerMember) HasRole(roleID uuid.UUID) bool {
	for _, id := range m.RoleIDs {
		if id == roleID {
			return true
		}
	}
	return false
}

// AddRole adds a role to the member
func (m *ServerMember) AddRole(roleID uuid.UUID) {
	if !m.HasRole(roleID) {
		m.RoleIDs = append(m.RoleIDs, roleID)
	}
}

// RemoveRole removes a role from the member
func (m *ServerMember) RemoveRole(roleID uuid.UUID) {
	for i, id := range m.RoleIDs {
		if id == roleID {
			m.RoleIDs = append(m.RoleIDs[:i], m.RoleIDs[i+1:]...)
			return
		}
	}
}
