package server

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func init() {
	// Initialize loggers for tests (silent mode)
	InitLogger(os.Stderr, log.FatalLevel)
}

func TestNewHandlers(t *testing.T) {
	hub := NewHub()
	stats := NewStatsTracker()

	handlers := NewHandlers(nil, hub, stats, nil)

	if handlers == nil {
		t.Fatal("NewHandlers returned nil")
	}

	if handlers.hub != hub {
		t.Error("hub not set correctly")
	}

	if handlers.stats != stats {
		t.Error("stats not set correctly")
	}

	if handlers.typingManager == nil {
		t.Error("typingManager not initialized")
	}
}

func TestHashToken(t *testing.T) {
	tests := []struct {
		name     string
		token1   string
		token2   string
		samHash  bool
	}{
		{
			name:    "identical tokens produce same hash",
			token1:  "test_token_123",
			token2:  "test_token_123",
			samHash: true,
		},
		{
			name:    "different tokens produce different hashes",
			token1:  "test_token_123",
			token2:  "test_token_456",
			samHash: false,
		},
		{
			name:    "empty tokens",
			token1:  "",
			token2:  "",
			samHash: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash1 := hashToken(tt.token1)
			hash2 := hashToken(tt.token2)

			if tt.samHash && hash1 != hash2 {
				t.Errorf("expected same hash for identical tokens")
			}

			if !tt.samHash && hash1 == hash2 {
				t.Errorf("expected different hashes for different tokens")
			}

			// Hash should be hex string of correct length (SHA256 = 64 hex chars)
			if len(hash1) != 64 {
				t.Errorf("expected hash length 64, got %d", len(hash1))
			}
		})
	}
}

func TestMinFunction(t *testing.T) {
	tests := []struct {
		name     string
		a        int
		b        int
		expected int
	}{
		{"a smaller", 5, 10, 5},
		{"b smaller", 10, 5, 5},
		{"equal values", 7, 7, 7},
		{"negative values", -5, 3, -5},
		{"zero and positive", 0, 100, 0},
		{"both negative", -10, -5, -10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := min(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("min(%d, %d) = %d, expected %d", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

func TestCheckPermissionLogic(t *testing.T) {
	// Test permission bitfield logic
	tests := []struct {
		name       string
		rolePerm   uint64
		checkPerm  models.Permission
		shouldPass bool
	}{
		{
			name:       "has exact permission",
			rolePerm:   uint64(models.PermissionManageChannels),
			checkPerm:  models.PermissionManageChannels,
			shouldPass: true,
		},
		{
			name:       "has multiple permissions including target",
			rolePerm:   uint64(models.PermissionManageChannels | models.PermissionKickMembers),
			checkPerm:  models.PermissionManageChannels,
			shouldPass: true,
		},
		{
			name:       "missing required permission",
			rolePerm:   uint64(models.PermissionManageChannels),
			checkPerm:  models.PermissionKickMembers,
			shouldPass: false,
		},
		{
			name:       "no permissions",
			rolePerm:   0,
			checkPerm:  models.PermissionManageChannels,
			shouldPass: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test bitwise permission checking logic
			hasPermission := (tt.rolePerm & uint64(tt.checkPerm)) != 0

			if hasPermission != tt.shouldPass {
				t.Errorf("permission check failed: expected %v, got %v", tt.shouldPass, hasPermission)
			}
		})
	}
}

func TestTypingManager(t *testing.T) {
	hub := NewHub()
	stats := NewStatsTracker()
	handlers := NewHandlers(nil, hub, stats, nil)

	if handlers.typingManager == nil {
		t.Fatal("typingManager not initialized")
	}

	// Test typing manager can be created
	tm := NewTypingManager(hub)
	if tm == nil {
		t.Error("NewTypingManager returned nil")
	}
}

// TestStartTypingSetsIsBot confirms a plugin's typing indicator is flagged
// IsBot in the broadcast payload (so the client renders "is thinking"
// instead of "is typing"), and that a real human's is not.
func TestStartTypingSetsIsBot(t *testing.T) {
	hub := NewHub()
	tm := NewTypingManager(hub)
	channelID := uuid.New()

	tm.StartTyping(uuid.New(), channelID, uuid.Nil, "Alice", true)
	tm.StartTyping(uuid.New(), channelID, uuid.Nil, "gh0st", false)

	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case bm := <-hub.broadcast:
			var payload protocol.TypingStartEventPayload
			if err := json.Unmarshal(bm.Message.Data, &payload); err != nil {
				t.Fatalf("unmarshal broadcast payload: %v", err)
			}
			got[payload.Username] = payload.IsBot
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for broadcast message")
		}
	}

	if !got["Alice"] {
		t.Error("Alice's typing event should have IsBot = true")
	}
	if got["gh0st"] {
		t.Error("gh0st's typing event should have IsBot = false")
	}
}
