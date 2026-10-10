package client

import (
	"encoding/json"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// paneTestApp returns an app viewing a plugin channel whose pane sends go
// into the returned queue instead of a socket.
func paneTestApp(t *testing.T) (*App, *Connection, *models.Channel) {
	t.Helper()
	a := newLayoutTestApp(t, 160, 45)
	a.view = ViewMain
	conn := &Connection{connected: true, send: make(chan *protocol.Message, 64)}
	sc := &ServerConnection{ServerID: uuid.New(), Connection: conn}
	a.activeConn = sc
	ch := &models.Channel{ID: uuid.New(), Name: "chess", Type: models.ChannelTypePlugin}
	a.currentChannel = ch
	a.enterPluginPane(ch)
	return a, conn, ch
}

// sent drains the connection's queue, returning the opcodes sent in order.
func sent(conn *Connection) []*protocol.Message {
	var out []*protocol.Message
	for {
		select {
		case m := <-conn.send:
			out = append(out, m)
		default:
			return out
		}
	}
}

func TestPaneEnterWaitsForSelectionToSettle(t *testing.T) {
	a, conn, ch := paneTestApp(t)
	a.focus = FocusChannelList

	// Just selected while browsing: nothing yet, a re-check is scheduled.
	if cmd := a.syncPluginPane(); cmd == nil {
		t.Fatal("expected a deferred re-check while the selection settles")
	}
	if msgs := sent(conn); len(msgs) != 0 {
		t.Fatalf("Enter sent immediately while arrowing through channels: %v", msgs)
	}

	// Moving on before it settles never bothers the plugin at all.
	a.leavePluginPane()
	if msgs := sent(conn); len(msgs) != 0 {
		t.Fatalf("a never-entered pane sent %d messages on leave", len(msgs))
	}

	// Staying put: Enter goes out with the drawn size and the theme.
	a.enterPluginPane(ch)
	a.pluginPane.selectedAt = time.Now().Add(-time.Second)
	a.renderPluginPaneFrame(77, 33)
	a.syncPluginPane()
	msgs := sent(conn)
	if len(msgs) != 1 || msgs[0].Op != protocol.OpPluginPaneEnter {
		t.Fatalf("want one Enter, got %v", msgs)
	}
	var enter protocol.PluginPaneEnterPayload
	_ = json.Unmarshal(msgs[0].Data, &enter)
	if enter.Width != 77 || enter.Height != 33 || enter.Theme == nil || enter.Theme.Palette["foreground"] == "" {
		t.Fatalf("Enter = %+v (theme %+v)", enter, enter.Theme)
	}
}

func TestPaneEntersImmediatelyWhenFocusedAndResizesOnLayoutChange(t *testing.T) {
	a, conn, _ := paneTestApp(t)
	a.focus = FocusChat
	a.renderPluginPaneFrame(80, 30)
	a.syncPluginPane()
	if msgs := sent(conn); len(msgs) != 1 || msgs[0].Op != protocol.OpPluginPaneEnter {
		t.Fatalf("focused pane should Enter at once, got %v", msgs)
	}

	// Same size: nothing to say.
	a.syncPluginPane()
	if msgs := sent(conn); len(msgs) != 0 {
		t.Fatalf("unchanged size re-sent: %v", msgs)
	}

	// The member list collapses and the pane is drawn wider.
	a.renderPluginPaneFrame(100, 30)
	a.syncPluginPane()
	msgs := sent(conn)
	if len(msgs) != 1 || msgs[0].Op != protocol.OpPluginPaneResize {
		t.Fatalf("want one Resize, got %v", msgs)
	}
	var resize protocol.PluginPaneResizePayload
	_ = json.Unmarshal(msgs[0].Data, &resize)
	if resize.Width != 100 || resize.Height != 30 {
		t.Fatalf("Resize = %+v", resize)
	}
}

func TestPaneReentersAfterReconnect(t *testing.T) {
	a, conn, _ := paneTestApp(t)
	a.focus = FocusChat
	a.syncPluginPane()
	sent(conn)

	// Connection drops: sends fail, the pane waits and retries.
	conn.connected = false
	a.paneDisconnected()
	a.syncPluginPane()
	if a.pluginPane.entered {
		t.Fatal("pane claims to be entered while disconnected")
	}

	// Back up: the retry re-enters without the user doing anything.
	conn.connected = true
	a.pluginPane.retryAt = time.Time{}
	a.syncPluginPane()
	if msgs := sent(conn); len(msgs) != 1 || msgs[0].Op != protocol.OpPluginPaneEnter {
		t.Fatalf("want a re-Enter after reconnect, got %v", msgs)
	}
}

func TestCtrlBracketAlwaysLeavesPaneAndIsNeverForwarded(t *testing.T) {
	a, conn, _ := paneTestApp(t)
	a.focus = FocusChat
	a.syncPluginPane()
	sent(conn)

	a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if msgs := sent(conn); len(msgs) != 1 || msgs[0].Op != protocol.OpPluginPaneInput {
		t.Fatalf("an ordinary key should be forwarded, got %v", msgs)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	for _, m := range sent(conn) {
		if m.Op == protocol.OpPluginPaneInput {
			t.Fatal("Ctrl+] was forwarded to the plugin")
		}
	}
	if a.focus != FocusChannelList {
		t.Fatalf("Ctrl+] should hand focus back, focus is %v", a.focus)
	}
	if a.pluginPane == nil {
		t.Fatal("Ctrl+] should keep the pane open")
	}
}

func TestPluginEventsLeavePaneTitleAndNotifyUser(t *testing.T) {
	a, _, ch := paneTestApp(t)
	a.focus = FocusChat
	raw := func(v interface{}) json.RawMessage { b, _ := json.Marshal(v); return b }

	a.handlePluginEvent(a.activeConn.ServerID, a.activeConn, protocol.PluginEventPayload{
		Kind: protocol.PluginEventPaneTitle, Payload: raw(protocol.PluginPaneTitlePayload{ChannelID: ch.ID, Title: "Chess —\x1b]52;c;aGk=\x07 alice\nvs bob"}),
	})
	if a.pluginPane.Title != "Chess — alice vs bob" {
		t.Errorf("title = %q", a.pluginPane.Title)
	}

	a.handlePluginEvent(a.activeConn.ServerID, a.activeConn, protocol.PluginEventPayload{
		Kind: protocol.PluginEventLeavePane, Payload: raw(protocol.PluginPaneClosePayload{ChannelID: ch.ID}),
	})
	if a.focus != FocusChannelList || a.pluginPane == nil {
		t.Errorf("leave_pane: focus %v, pane %v", a.focus, a.pluginPane)
	}

	other := uuid.New()
	a.notifConfig.DesktopNotifyMode = "off"
	a.notifConfig.SoundsMuted = true
	a.handlePluginEvent(a.activeConn.ServerID, a.activeConn, protocol.PluginEventPayload{
		Kind: protocol.PluginEventNotifyUser, Payload: raw(protocol.PluginNotifyUserPayload{ChannelID: other, Content: "bob challenged you"}),
	})
	if a.mentionCounts[a.activeConn.ServerID][other] != 1 {
		t.Error("notify_user didn't badge the plugin's channel")
	}
}
