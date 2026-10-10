package client

import (
	"encoding/json"
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// TestRetentionPolicyUpdateClearsChannelOverridesWhenLastOneRemoved is a
// regression test for a real bug found live 2026-09-09: deleting the last
// exempt channel in Server Settings > Messages left the "Channel Overrides"
// list showing the stale, pre-deletion entries until the user left and
// re-entered Server Settings (which does an unguarded fresh fetch).
//
// Root cause: RetentionPolicyUpdatePayload.ChannelOverrides carries
// `json:"channel_overrides,omitempty"`. ListChannelOverrides (sqlite.go)
// returns a nil (not empty-but-non-nil) slice when zero rows match, so once
// the last override is deleted the server's freshly-queried nil slice is
// omitted from the wire payload entirely -- it unmarshals back to nil on
// the client too. The handler's `if payload.ChannelOverrides != nil { ... }`
// guard then skipped applying exactly this "zero overrides remain" update,
// leaving the old list in a.serverManagementState.ChannelOverrides.
//
// This test builds the exact wire payload the server sends in that
// scenario (a JSON object with the "channel_overrides" key entirely absent,
// not present-and-null) and confirms the client's in-memory override list
// is cleared to empty, not left stale.
func TestRetentionPolicyUpdateClearsChannelOverridesWhenLastOneRemoved(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewServerManagement

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	_ = sc

	staleChannelID := uuid.New()
	a.serverManagementState = &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages"},
		SelectedCategory: 3,
		ChannelOverrides: []*models.MessageRetentionPolicy{
			{ID: uuid.New(), ServerID: serverID, ChannelID: &staleChannelID},
		},
		SelectedOverride: 0,
	}

	// Simulate the exact wire shape sent when zero overrides remain: the
	// "channel_overrides" key is entirely absent (omitempty + nil slice),
	// not present as JSON null.
	raw := []byte(`{"policy":null}`)
	var payload protocol.RetentionPolicyUpdatePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("failed to unmarshal test payload: %v", err)
	}
	if payload.ChannelOverrides != nil {
		t.Fatalf("test setup bug: expected ChannelOverrides to unmarshal as nil from an absent key, got %v", payload.ChannelOverrides)
	}

	msg := &protocol.Message{
		Op:   protocol.OpDispatch,
		Type: protocol.EventRetentionPolicyUpdate,
		Data: raw,
	}
	a.handleDispatch(serverID, msg)

	if got := len(a.serverManagementState.ChannelOverrides); got != 0 {
		t.Errorf("expected ChannelOverrides to be cleared to empty after the last override was deleted, still has %d stale entries", got)
	}
}
