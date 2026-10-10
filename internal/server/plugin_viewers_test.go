package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// paneTestRig is a running server with a HelloPlugin channel, where the
// plugin's connection is replaced by a fake whose outbound queue the test
// reads directly -- so assertions see exactly what the plugin would.
type paneTestRig struct {
	srv     *Server
	server  *models.Server
	wsURL   string
	channel *models.Channel
	plugin  *Client
}

func newPaneTestRig(t *testing.T) *paneTestRig {
	t.Helper()
	srv, defaultServer, wsURL := startTestPluginServer(t)
	channel := models.NewPluginChannel(defaultServer.ID, "hello-counter", "HelloPlugin", "counter")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create plugin channel: %v", err)
	}
	serviceUserID, err := srv.plugins.ServiceUserIDFor("HelloPlugin")
	if err != nil {
		t.Fatalf("no service account for HelloPlugin: %v", err)
	}
	fake := &Client{UserID: serviceUserID, IsPlugin: true, PluginID: "HelloPlugin", send: make(chan *protocol.Message, 256), connEpoch: 7}
	srv.hub.register <- fake
	return &paneTestRig{srv: srv, server: defaultServer, wsURL: wsURL, channel: channel, plugin: fake}
}

// member connects a new server member and returns their WS client.
func (r *paneTestRig) member(t *testing.T, username string) (*models.User, *testWSClient) {
	t.Helper()
	user, token := createTestUserAndToken(t, r.srv, username)
	joinWithEveryoneRole(t, r.srv, r.server.ID, user.ID)
	c := newTestWSClient(t, r.wsURL)
	c.identify(token)
	return user, c
}

// pluginReceives waits for the plugin to be sent event, decoding it into out.
func (r *paneTestRig) pluginReceives(t *testing.T, event protocol.EventType, out interface{}) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-r.plugin.send:
			if m.Type != event {
				continue
			}
			if err := json.Unmarshal(m.Data, out); err != nil {
				t.Fatalf("bad %s payload: %v", event, err)
			}
			return
		case <-deadline:
			t.Fatalf("plugin never received %s", event)
		}
	}
}

// pushFrame sends a frame as the plugin.
func (r *paneTestRig) pushFrame(t *testing.T, frame protocol.PluginPaneFramePayload) {
	t.Helper()
	msg, err := protocol.NewMessage(protocol.OpPluginPaneFrame, frame)
	if err != nil {
		t.Fatal(err)
	}
	r.srv.handlers.HandlePluginPaneFrame(r.plugin, msg)
}

// pluginEvent sends an OpPluginEvent as the plugin.
func (r *paneTestRig) pluginEvent(t *testing.T, kind string, viewer uuid.UUID, payload interface{}) {
	t.Helper()
	raw, _ := json.Marshal(payload)
	msg, err := protocol.NewMessage(protocol.OpPluginEvent, protocol.PluginEventPayload{Kind: kind, ViewerID: viewer, Payload: raw})
	if err != nil {
		t.Fatal(err)
	}
	r.srv.handlers.HandlePluginEvent(r.plugin, msg)
}

func isFrame(m *protocol.Message) bool { return m.Type == protocol.EventPluginPaneFrame }

// expectNoFrameBefore sends a marker through the same hub queue frames use,
// then reads c until the marker: a frame arriving first means one was
// delivered that shouldn't have been.
func expectNoFrameBefore(t *testing.T, r *paneTestRig, userID uuid.UUID, c *testWSClient) {
	t.Helper()
	expectNothingBefore(t, r, userID, c, isFrame)
}

// expectNothingBefore is expectNoFrameBefore for any unwanted message.
func expectNothingBefore(t *testing.T, r *paneTestRig, userID uuid.UUID, c *testWSClient, unwanted func(*protocol.Message) bool) {
	t.Helper()
	if err := r.srv.hub.SendToUser(userID, protocol.EventTypingStop, protocol.TypingStartEventPayload{}); err != nil {
		t.Fatal(err)
	}
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return unwanted(m) || m.Type == protocol.EventTypingStop
	})
	if got.Type != protocol.EventTypingStop {
		t.Fatalf("user received a message they should not have: %s %s", got.Type, got.Data)
	}
}

func TestPaneEnterCarriesViewerIdentityAndDisconnectSendsLeave(t *testing.T) {
	r := newPaneTestRig(t)
	alice, c := r.member(t, "alice")

	c.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{
		ChannelID: r.channel.ID, Width: 80, Height: 24,
		Theme: &protocol.PaneTheme{Name: "dracula", ColorProfile: "truecolor", Palette: map[string]string{"red": "#ff5555"}},
	})
	var enter protocol.PluginPaneEnterPayload
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)
	if enter.ViewerID != alice.ID || enter.ViewerName != "alice" || enter.ViewerDisplayName != "alice" {
		t.Fatalf("Enter identity = %+v", enter)
	}
	if enter.Theme == nil || enter.Theme.Palette["red"] != "#ff5555" {
		t.Fatalf("Enter theme not relayed: %+v", enter.Theme)
	}

	c.send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: r.channel.ID, KeyString: "x"})
	var input protocol.PluginPaneInputPayload
	r.pluginReceives(t, protocol.EventPluginPaneInput, &input)
	if input.ViewerID != alice.ID || input.ViewerName != "alice" {
		t.Fatalf("Input identity = %+v", input)
	}

	// Closing the connection can't send a Leave; the server must.
	c.conn.Close()
	var leave protocol.PluginPaneLeavePayload
	r.pluginReceives(t, protocol.EventPluginPaneLeave, &leave)
	if leave.ViewerID != alice.ID || leave.ChannelID != r.channel.ID {
		t.Fatalf("Leave = %+v", leave)
	}
}

func TestPaneInputIgnoredWithoutEnter(t *testing.T) {
	r := newPaneTestRig(t)
	_, c := r.member(t, "bob")
	c.send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: r.channel.ID, KeyString: "x"})
	c.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 10, Height: 5})
	// The first thing the plugin hears must be the Enter, not the stray key.
	deadline := time.After(5 * time.Second)
	for {
		select {
		case m := <-r.plugin.send:
			switch m.Type {
			case protocol.EventPluginPaneInput:
				t.Fatal("input from a user who hadn't entered the pane was relayed")
			case protocol.EventPluginPaneEnter:
				return
			}
		case <-deadline:
			t.Fatal("plugin never received the Enter")
		}
	}
}

func TestPaneFramesBroadcastAndSpoofRejected(t *testing.T) {
	r := newPaneTestRig(t)
	alice, a := r.member(t, "alice")
	bob, b := r.member(t, "bob")
	carol, c := r.member(t, "carol") // a member, but not viewing

	for _, cl := range []*testWSClient{a, b} {
		cl.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 40, Height: 10})
		var enter protocol.PluginPaneEnterPayload
		r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)
	}

	// Empty ViewerID: every viewer gets it, stamped with their own ID and
	// the plugin connection's epoch.
	r.pushFrame(t, protocol.PluginPaneFramePayload{ChannelID: r.channel.ID, Frame: "shared", Seq: 1})
	for who, cl := range map[uuid.UUID]*testWSClient{alice.ID: a, bob.ID: b} {
		got := cl.readUntil(5*time.Second, isFrame)
		var f protocol.PluginPaneFramePayload
		_ = json.Unmarshal(got.Data, &f)
		if f.Frame != "shared" || f.ViewerID != who || f.Epoch != 7 {
			t.Fatalf("broadcast frame = %+v, want shared for %s at epoch 7", f, who)
		}
	}

	// A frame aimed at someone who isn't viewing is dropped.
	r.pushFrame(t, protocol.PluginPaneFramePayload{ChannelID: r.channel.ID, ViewerID: carol.ID, Frame: "spoof", Seq: 2})
	expectNoFrameBefore(t, r, carol.ID, c)

	// And so are viewer-directed events to a non-viewer.
	r.pluginEvent(t, "leave_pane", carol.ID, protocol.PluginPaneClosePayload{ChannelID: r.channel.ID})
	expectNothingBefore(t, r, carol.ID, c, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginEvent })
}

func TestPaneEntersReplayedWhenPluginReconnects(t *testing.T) {
	r := newPaneTestRig(t)
	alice, a := r.member(t, "alice")
	a.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 70, Height: 20})
	var enter protocol.PluginPaneEnterPayload
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)
	a.send(protocol.OpPluginPaneResize, protocol.PluginPaneResizePayload{ChannelID: r.channel.ID, Width: 90, Height: 30})
	var resize protocol.PluginPaneResizePayload
	r.pluginReceives(t, protocol.EventPluginPaneResize, &resize)

	// The plugin restarts: a fresh connection identifies and must be told
	// about alice at her latest size without her doing anything.
	restarted := &Client{UserID: r.plugin.UserID, IsPlugin: true, PluginID: "HelloPlugin", send: make(chan *protocol.Message, 16)}
	r.srv.handlers.replayPaneEnters(restarted, "HelloPlugin")
	select {
	case m := <-restarted.send:
		var got protocol.PluginPaneEnterPayload
		_ = json.Unmarshal(m.Data, &got)
		if m.Type != protocol.EventPluginPaneEnter || got.ViewerID != alice.ID || got.Width != 90 || got.Height != 30 || got.ViewerName != "alice" {
			t.Fatalf("replayed %s %+v", m.Type, got)
		}
	default:
		t.Fatal("no Enter replayed to the reconnected plugin")
	}
}

func TestPluginMembersNotifyUserAndPaneTitle(t *testing.T) {
	r := newPaneTestRig(t)
	alice, a := r.member(t, "alice")
	bob, b := r.member(t, "bob")
	a.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: r.channel.ID, Width: 40, Height: 10})
	var enter protocol.PluginPaneEnterPayload
	r.pluginReceives(t, protocol.EventPluginPaneEnter, &enter)

	r.pluginEvent(t, protocol.PluginEventMembers, uuid.Nil, protocol.PluginMembersRequest{ChannelID: r.channel.ID, RequestID: "q1"})
	var ev protocol.PluginEventPayload
	r.pluginReceives(t, protocol.EventPluginEvent, &ev)
	var members protocol.PluginMembersResponse
	if err := json.Unmarshal(ev.Payload, &members); err != nil || ev.Kind != protocol.PluginEventMembers || members.RequestID != "q1" {
		t.Fatalf("members reply = %s %s", ev.Kind, ev.Payload)
	}
	found := map[uuid.UUID]protocol.PluginMember{}
	for _, m := range members.Members {
		found[m.UserID] = m
	}
	if m := found[alice.ID]; !m.Online || !m.Viewing {
		t.Errorf("alice = %+v, want online and viewing", m)
	}
	if m := found[bob.ID]; !m.Online || m.Viewing {
		t.Errorf("bob = %+v, want online, not viewing", m)
	}
	if _, ok := found[r.plugin.UserID]; ok {
		t.Error("the plugin's own service account was listed as a member")
	}

	// notify_user reaches bob even though he isn't viewing the pane.
	r.pluginEvent(t, protocol.PluginEventNotifyUser, uuid.Nil, protocol.PluginNotifyUserPayload{UserID: bob.ID, ChannelID: r.channel.ID, Content: "alice challenged you"})
	got := b.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginEvent })
	_ = json.Unmarshal(got.Data, &ev)
	var note protocol.PluginNotifyUserPayload
	_ = json.Unmarshal(ev.Payload, &note)
	if ev.Kind != protocol.PluginEventNotifyUser || note.Content != "alice challenged you" || note.ChannelID != r.channel.ID {
		t.Fatalf("notify_user = %s %s", ev.Kind, ev.Payload)
	}

	// pane_title with no ViewerID reaches every viewer (alice).
	r.pluginEvent(t, protocol.PluginEventPaneTitle, uuid.Nil, protocol.PluginPaneTitlePayload{ChannelID: r.channel.ID, Title: "Chess — alice vs bob"})
	got = a.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginEvent })
	_ = json.Unmarshal(got.Data, &ev)
	var title protocol.PluginPaneTitlePayload
	_ = json.Unmarshal(ev.Payload, &title)
	if ev.Kind != protocol.PluginEventPaneTitle || title.Title != "Chess — alice vs bob" {
		t.Fatalf("pane_title = %s %s", ev.Kind, ev.Payload)
	}
}
