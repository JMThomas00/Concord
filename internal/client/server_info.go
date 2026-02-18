package client

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ClientServerInfo represents client-side metadata for a server
// This is distinct from models.Server which represents protocol-level server data
type ClientServerInfo struct {
	ID               uuid.UUID          `json:"id"`                // Client-generated tracking ID
	Name             string             `json:"name"`              // Display name for this server
	Address          string             `json:"address"`           // Server address (hostname or IP)
	Port             int                `json:"port"`              // Server port
	UseTLS           bool               `json:"use_tls"`           // Whether to use TLS/WSS
	AddedAt          time.Time          `json:"added_at"`          // When server was added
	LastConnected    *time.Time         `json:"last_connected,omitempty"` // Last successful connection
	SavedCredentials *SavedCredentials  `json:"saved_credentials,omitempty"` // Saved login credentials
	UserID           uuid.UUID          `json:"user_id,omitempty"` // User ID on this server
	IconLetter       string             `json:"icon_letter"`       // Letter shown in server icon (1-2 chars)
	IconColor        string             `json:"icon_color"`        // Hex color for server icon
}

// SavedCredentials stores login credentials for auto-connect
type SavedCredentials struct {
	Email               string `json:"email"`                         // User email
	Token               string `json:"token"`                         // Auth token (plaintext in Phase 1)
	AutoConnect         bool   `json:"auto_connect"`                  // Auto-connect on startup
	RememberCredentials bool   `json:"remember_credentials"`          // Whether to save credentials
}

// NewClientServerInfo creates a new ClientServerInfo with generated ID and defaults
func NewClientServerInfo(name, address string, port int, useTLS bool) *ClientServerInfo {
	// Generate icon letter from name (first 1-2 uppercase letters)
	iconLetter := "S" // Default
	if len(name) > 0 {
		iconLetter = string(name[0])
		if len(name) > 1 {
			iconLetter = name[0:2]
		}
	}

	// Assign a color based on name hash (simple approach)
	iconColor := getColorForName(name)

	return &ClientServerInfo{
		ID:         uuid.New(),
		Name:       name,
		Address:    address,
		Port:       port,
		UseTLS:     useTLS,
		AddedAt:    time.Now(),
		IconLetter: iconLetter,
		IconColor:  iconColor,
	}
}

// GetWebSocketURL returns the full WebSocket URL for this server
func (s *ClientServerInfo) GetWebSocketURL() string {
	protocol := "ws"
	if s.UseTLS {
		protocol = "wss"
	}
	return fmt.Sprintf("%s://%s:%d/ws", protocol, s.Address, s.Port)
}

// GetHTTPURL returns the base HTTP URL for this server
func (s *ClientServerInfo) GetHTTPURL() string {
	protocol := "http"
	if s.UseTLS {
		protocol = "https"
	}
	return fmt.Sprintf("%s://%s:%d", protocol, s.Address, s.Port)
}

// String returns a human-readable string representation
func (s *ClientServerInfo) String() string {
	return fmt.Sprintf("%s (%s:%d)", s.Name, s.Address, s.Port)
}

// getColorForName generates a deterministic color based on name
// Uses a simple palette of Discord-like colors
func getColorForName(name string) string {
	colors := []string{
		"#7289DA", // Blurple
		"#99AAB5", // Gray
		"#43B581", // Green
		"#FAA61A", // Yellow
		"#F26522", // Orange
		"#F04747", // Red
		"#E91E63", // Pink
		"#9B59B6", // Purple
		"#3498DB", // Blue
		"#1ABC9C", // Teal
	}

	// Simple hash based on name length and first character
	hash := len(name)
	if len(name) > 0 {
		hash += int(name[0])
	}

	return colors[hash%len(colors)]
}
