package client

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// appOnPane is an App showing a plugin pane with the keyboard in it.
func appOnPane(t *testing.T) *App {
	t.Helper()
	a := settingsTestApp()
	ch := &models.Channel{ID: uuid.New(), Name: "board"}
	a.view, a.currentChannel, a.focus = ViewMain, ch, FocusChat
	a.pluginPane = &PluginPaneState{ChannelID: ch.ID, entered: true}
	return a
}

func press(a *App, k string) {
	var msg tea.KeyMsg
	switch k {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	case "shift+tab":
		msg = tea.KeyMsg{Type: tea.KeyShiftTab}
	case "ctrl+]":
		msg = tea.KeyMsg{Type: tea.KeyCtrlCloseBracket}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	a.handleKeyPress(msg)
}

// A pane is a stop in the Tab ring like any channel: Esc and Shift+Tab go
// back to the channels, Tab on to the members, and back again.
func TestPaneNavigationKeys(t *testing.T) {
	for _, c := range []struct{ key, want string }{
		{"esc", "channels"}, {"shift+tab", "channels"}, {"tab", "members"}, {"ctrl+]", "channels"}, {"x", "pane"},
	} {
		a := appOnPane(t)
		press(a, c.key)
		got := map[FocusArea]string{FocusChannelList: "channels", FocusUserList: "members", FocusChat: "pane"}[a.focus]
		if got != c.want {
			t.Errorf("%s from a pane went to %q, want %q", c.key, got, c.want)
		}
	}

	// From the members, Shift+Tab comes back to the pane (no message box).
	a := appOnPane(t)
	press(a, "tab")
	a.cycleFocusReverse()
	if a.focus != FocusChat {
		t.Fatalf("Shift+Tab from the members went to %v, not the pane", a.focus)
	}
}

// Keys the frame on screen claims go to the plugin; Ctrl+] still leaves.
func TestPaneClaimedKeysStayInThePane(t *testing.T) {
	a := appOnPane(t)
	a.applyPluginPaneFrame(protocol.PluginPaneFramePayload{ChannelID: a.pluginPane.ChannelID, Frame: "piece selected", Seq: 1,
		Keys: []string{protocol.PaneKeyEsc, protocol.PaneKeyTab}})
	press(a, "esc")
	press(a, "tab")
	if a.focus != FocusChat {
		t.Fatalf("claimed keys moved focus to %v", a.focus)
	}
	press(a, "shift+tab") // not claimed
	if a.focus != FocusChannelList {
		t.Fatalf("unclaimed Shift+Tab: focus %v", a.focus)
	}

	a = appOnPane(t)
	a.pluginPane.Keys = protocol.PaneNavigationKeys
	press(a, "ctrl+]")
	if a.focus != FocusChannelList {
		t.Fatal("Ctrl+] must always leave")
	}

	// The client code's own frame claims for itself.
	a = appOnPane(t)
	a.pluginPane.Keys = []string{protocol.PaneKeyEsc}
	text := "local"
	a.pluginPane.local = &codeFrame{text: &text}
	press(a, "esc")
	if a.focus != FocusChannelList {
		t.Fatal("the server's claim applied to the client code's frame")
	}
}
