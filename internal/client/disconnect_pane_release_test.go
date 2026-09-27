package client

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// TestDisconnectReleasesStuckPluginPane is a regression test for a real bug
// found while removing dead code from Update(): the logic that releases an
// active plugin pane's key capture on disconnect used to live in an
// unreachable top-level `case DisconnectedMsg` in Update() -- ConnectedMsg/
// DisconnectedMsg are always pre-wrapped in ServerScopedMsg before
// dispatch, so that case could never actually fire. This proves the logic,
// in handleServerScopedMessage's real DisconnectedMsg case, actually runs.
//
// The pane itself survives the disconnect (it re-enters once the connection
// is back -- see syncPluginPane); only key capture is released, and the
// pane is marked as no longer registered with the server.
func TestDisconnectReleasesStuckPluginPane(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}

	channel := &models.Channel{ID: uuid.New(), Type: models.ChannelTypePlugin}
	a.currentChannel = channel
	a.pluginPane = &PluginPaneState{ChannelID: channel.ID, conn: sc, entered: true}
	a.focus = FocusChat

	a.handleServerScopedMessage(ServerScopedMsg{ServerID: serverID, Msg: DisconnectedMsg{}})

	if a.pluginPane == nil {
		t.Fatal("the pane should stay open across a disconnect")
	}
	if a.pluginPane.entered {
		t.Error("the pane should be marked for re-entry once the connection is back")
	}
	if a.focus != FocusChannelList {
		t.Errorf("expected focus to move to FocusChannelList, got %v", a.focus)
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
