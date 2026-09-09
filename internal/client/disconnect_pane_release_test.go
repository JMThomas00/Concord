package client

import (
	"testing"

	"github.com/google/uuid"
)

// TestDisconnectReleasesStuckPluginPane is a regression test for a real bug
// found while removing dead code from Update(): the logic that releases an
// active plugin pane on disconnect (so a viewer isn't stuck with every
// keystroke captured by the pane guards once a plugin can no longer relay
// leave_pane over a dead connection) used to live in an unreachable
// top-level `case DisconnectedMsg` in Update() -- ConnectedMsg/
// DisconnectedMsg are always pre-wrapped in ServerScopedMsg before
// dispatch, so that case could never actually fire. This proves the logic,
// now moved into handleServerScopedMessage's real DisconnectedMsg case,
// actually runs.
func TestDisconnectReleasesStuckPluginPane(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	if _, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	channelID := uuid.New()
	a.pluginPane = &PluginPaneState{ChannelID: channelID}
	a.focus = FocusChat

	a.handleServerScopedMessage(ServerScopedMsg{ServerID: serverID, Msg: DisconnectedMsg{}})

	if a.pluginPane != nil {
		t.Error("expected the plugin pane to be released on disconnect")
	}
	if a.focus != FocusChannelList {
		t.Errorf("expected focus to move to FocusChannelList after pane release, got %v", a.focus)
	}
}

// TestDisconnectWithNoPluginPaneDoesNothing confirms the release logic is a
// no-op (no panic, focus untouched) when there's no active pane -- the
// common case, so this must not have any observable side effect.
func TestDisconnectWithNoPluginPaneDoesNothing(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	if _, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID}); err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	a.focus = FocusChat

	a.handleServerScopedMessage(ServerScopedMsg{ServerID: serverID, Msg: DisconnectedMsg{}})

	if a.pluginPane != nil {
		t.Error("expected pluginPane to remain nil")
	}
	if a.focus != FocusChat {
		t.Errorf("expected focus to remain unchanged with no active pane, got %v", a.focus)
	}
}
