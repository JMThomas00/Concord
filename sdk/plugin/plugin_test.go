package plugin_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// counter is a tiny pane plugin: each viewer sees their name and how many
// keys they've pressed; "q" hands the keyboard back.
type counter struct {
	mu     sync.Mutex
	counts map[uuid.UUID]int
	names  map[uuid.UUID]string
	sizes  map[uuid.UUID][2]int
}

func newCounter() *counter {
	return &counter{counts: map[uuid.UUID]int{}, names: map[uuid.UUID]string{}, sizes: map[uuid.UUID][2]int{}}
}

func (p *counter) draw(c *plugin.Conn, ch, v uuid.UUID) {
	p.mu.Lock()
	frame := fmt.Sprintf("%s pressed %d keys (%dx%d)", p.names[v], p.counts[v], p.sizes[v][0], p.sizes[v][1])
	p.mu.Unlock()
	_ = c.Frame(ch, v, frame)
}

func (p *counter) handler() plugin.Handler {
	return plugin.Handler{
		OnEnter: func(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
			p.mu.Lock()
			p.names[e.ViewerID], p.sizes[e.ViewerID] = e.ViewerDisplayName, [2]int{e.Width, e.Height}
			p.mu.Unlock()
			p.draw(c, e.ChannelID, e.ViewerID)
		},
		OnInput: func(c *plugin.Conn, e wire.PluginPaneInputPayload) {
			if e.KeyString == "q" {
				_ = c.LeavePane(e.ChannelID, e.ViewerID)
				return
			}
			p.mu.Lock()
			p.counts[e.ViewerID]++
			p.mu.Unlock()
			p.draw(c, e.ChannelID, e.ViewerID)
		},
		OnResize: func(c *plugin.Conn, e wire.PluginPaneResizePayload) {
			p.mu.Lock()
			p.sizes[e.ViewerID] = [2]int{e.Width, e.Height}
			p.mu.Unlock()
			p.draw(c, e.ChannelID, e.ViewerID)
		},
	}
}

func startPlugin(t *testing.T, h plugin.Handler) (*plugintest.Server, context.CancelFunc, chan error) {
	t.Helper()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- plugin.Run(ctx, srv.Config(), h) }()
	t.Cleanup(cancel)
	srv.WaitReady()
	return srv, cancel, done
}

func TestPaneLifecycle(t *testing.T) {
	srv, _, _ := startPlugin(t, newCounter().handler())
	board := uuid.New()

	alice := srv.Enter(board, "alice", 80, 24)
	bob := srv.Enter(board, "bob", 40, 10)
	if got := srv.NextFrame(alice); got != "alice pressed 0 keys (80x24)" {
		t.Fatalf("alice's first frame: %q", got)
	}
	srv.NextFrame(bob)

	srv.Type(alice, "xyz")
	srv.FrameContaining(alice, "alice pressed 3 keys")
	srv.Resize(bob, 60, 12)
	srv.FrameContaining(bob, "bob pressed 0 keys (60x12)")

	srv.Key(alice, "q")
	e := srv.NextEvent()
	if e.Kind != wire.PluginEventLeavePane || e.ViewerID != alice.ID {
		t.Fatalf("q should hand alice's keys back: %+v", e)
	}
}

// After a dropped connection the plugin reconnects on its own, and the
// replayed Enter repaints everyone -- its own state (the count) intact.
func TestReconnectRepaintsViewers(t *testing.T) {
	srv, _, _ := startPlugin(t, newCounter().handler())
	alice := srv.Enter(uuid.New(), "alice", 80, 24)
	srv.NextFrame(alice)
	srv.Type(alice, "ab")
	srv.FrameContaining(alice, "pressed 2 keys")

	srv.DropConnection()
	srv.WaitReady()
	srv.FrameContaining(alice, "alice pressed 2 keys")
}

func TestHelpersSendTheRightEvents(t *testing.T) {
	var conn *plugin.Conn
	ready := make(chan struct{})
	srv, _, _ := startPlugin(t, plugin.Handler{OnReady: func(c *plugin.Conn, _ *wire.User) { conn = c; close(ready) }})
	<-ready
	ch, user := uuid.New(), uuid.New()

	_ = conn.Notify("server started")
	if e := srv.NextEvent(); e.Kind != wire.PluginEventNotify || !strings.Contains(string(e.Payload), "server started") {
		t.Errorf("Notify sent %+v", e)
	}
	_ = conn.NotifyUser(user, ch, "your turn")
	if e := srv.NextEvent(); e.Kind != wire.PluginEventNotifyUser || !strings.Contains(string(e.Payload), user.String()) {
		t.Errorf("NotifyUser sent %+v", e)
	}
	_ = conn.SetTitle(ch, uuid.Nil, "Chess — alice vs bob")
	if e := srv.NextEvent(); e.Kind != wire.PluginEventPaneTitle || e.ViewerID != uuid.Nil {
		t.Errorf("SetTitle sent %+v", e)
	}
	_ = conn.SendMessage(ch, "hello", nil)
	if m := srv.NextChat(); m.ChannelID != ch || m.Content != "hello" {
		t.Errorf("SendMessage sent %+v", m)
	}

	// RequestMembers waits for the matching reply.
	got := make(chan []wire.PluginMember, 1)
	go func() {
		members, err := conn.RequestMembers(context.Background(), ch)
		if err != nil {
			t.Error(err)
		}
		got <- members
	}()
	req := srv.NextEvent()
	if req.Kind != wire.PluginEventMembers {
		t.Fatalf("RequestMembers sent %+v", req)
	}
	srv.AnswerMembers(req, []wire.PluginMember{{UserID: user, Username: "bob", Online: true}})
	select {
	case m := <-got:
		if len(m) != 1 || m[0].Username != "bob" {
			t.Fatalf("members = %+v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RequestMembers never returned")
	}
}

func TestChannelsSettingsAndChatReachHandlers(t *testing.T) {
	got := make(chan string, 10)
	srv, _, _ := startPlugin(t, plugin.Handler{
		OnChannel:       func(_ *plugin.Conn, ch wire.Channel) { got <- "channel " + ch.Name + " " + ch.PluginConfig["seating"] },
		OnChannelDelete: func(_ *plugin.Conn, e wire.ChannelDeletePayload) { got <- "deleted" },
		OnConfig:        func(_ *plugin.Conn, info wire.PluginInfo) { got <- "config " + info.ConfigValues["api_key"] },
		OnMessage:       func(_ *plugin.Conn, m wire.MessageCreatePayload) { got <- "message " + m.Author.Username + ": " + m.Content },
	})
	id := uuid.New()
	srv.Channel(wire.Channel{ID: id, Name: "hall", PluginConfig: map[string]string{"seating": "seats"}})
	srv.Settings(map[string]string{"api_key": "sk-1"})
	srv.ChatMessage(id, "alex", "@bot hi")
	srv.DeleteChannel(id)
	for _, want := range []string{"channel hall seats", "config sk-1", "message alex: @bot hi", "deleted"} {
		select {
		case g := <-got:
			if g != want {
				t.Fatalf("got %q, want %q (events arrive in order)", g, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("never got %q", want)
		}
	}
}

func TestRejectedTokenStopsRun(t *testing.T) {
	srv := plugintest.NewServer(t)
	cfg := srv.Config()
	cfg.Token = "wrong"
	err := plugin.Run(context.Background(), cfg, plugin.Handler{})
	if !errors.Is(err, plugin.ErrRejected) {
		t.Fatalf("Run with a bad token = %v, want ErrRejected", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("CONCORD_WS_URL", "")
	t.Setenv("CONCORD_PLUGIN_TOKEN", "")
	if _, ok := plugin.ConfigFromEnv(); ok {
		t.Fatal("without Concord's variables the program should run standalone")
	}
	t.Setenv("CONCORD_WS_URL", "ws://127.0.0.1:8080/ws")
	t.Setenv("CONCORD_PLUGIN_TOKEN", "tok")
	t.Setenv("CONCORD_PLUGIN_ID", "chess")
	t.Setenv("CONCORD_PLUGIN_DATA_DIR", "/data/chess")
	cfg, ok := plugin.ConfigFromEnv()
	if !ok || cfg.PluginID != "chess" || cfg.DataDir != "/data/chess" {
		t.Fatalf("ConfigFromEnv = %+v, %v", cfg, ok)
	}
}

// A viewer's client code and the plugin talk through client_message
// events: OnClientMessage in, SendToClient out.
func TestClientMessages(t *testing.T) {
	got := make(chan wire.PluginClientMessagePayload, 1)
	h := newCounter().handler()
	h.OnClientMessage = func(c *plugin.Conn, viewer uuid.UUID, m wire.PluginClientMessagePayload) {
		got <- m
		_ = c.SendToClient(m.ChannelID, viewer, map[string]string{"echo": string(m.Data)})
	}
	srv, _, _ := startPlugin(t, h)
	alice := srv.Enter(uuid.New(), "alice", 80, 24)
	srv.NextFrame(alice)

	srv.ClientMessage(alice, map[string]int{"score": 3})
	m := <-got
	if string(m.Data) != `{"score":3}` || m.ViewerName != "alice" {
		t.Fatalf("plugin got %+v", m)
	}
	reply, viewer := srv.NextClientMessage()
	if viewer != alice.ID || reply.ChannelID != alice.ChannelID || string(reply.Data) != `{"echo":"{\"score\":3}"}` {
		t.Fatalf("reply %+v to %v", reply, viewer)
	}

	var c plugin.Conn
	if err := c.SendToClient(alice.ChannelID, alice.ID, strings.Repeat("x", wire.MaxClientMessageBytes)); err == nil {
		t.Fatal("an oversized message was sent")
	}
}
