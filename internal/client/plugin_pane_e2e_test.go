package client

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/server"
)

// TestPluginPaneEndToEnd runs a real server with the real testplugin and
// drives the real client connection, dispatch path and Update loop: select
// a plugin channel, and the plugin's frame must end up on screen.
func TestPluginPaneEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("end-to-end")
	}
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "Plugins", "HelloPlugin")
	if err := os.MkdirAll(pluginDir, 0o755); err != nil {
		t.Fatal(err)
	}
	bin := "testplugin"
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	if out, err := exec.Command("go", "build", "-tags", "novoice", "-o", filepath.Join(pluginDir, bin), "../../cmd/testplugin").CombinedOutput(); err != nil {
		t.Fatalf("build testplugin: %v\n%s", err, out)
	}
	manifest, err := os.ReadFile("../plugins/testdata/HelloPlugin/plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), manifest, 0o644); err != nil {
		t.Fatal(err)
	}

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := server.DefaultConfig()
	cfg.Host, cfg.Port = "127.0.0.1", port
	cfg.DatabasePath = filepath.Join(dir, "concord.db")
	cfg.PluginsDir = filepath.Join(dir, "Plugins")
	cfg.MessagePruning.Enabled = false
	srv, err := server.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	go srv.Run()
	t.Cleanup(srv.Shutdown)
	time.Sleep(2 * time.Second) // server listening + plugin identified

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	conn := NewConnection(addr)
	_, token, err := conn.Register("owner", "owner@test.local", "correct-horse-battery")
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	incoming := make(chan *protocol.Message, 256)
	conn.SetHandlers(func(m *protocol.Message) { incoming <- m }, nil, nil, nil)
	if err := conn.Connect(); err != nil {
		t.Fatal(err)
	}
	defer conn.Disconnect()
	if err := conn.Identify(token); err != nil {
		t.Fatal(err)
	}

	next := func(what string, match func(*protocol.Message) bool) *protocol.Message {
		t.Helper()
		deadline := time.After(5 * time.Second)
		for {
			select {
			case m := <-incoming:
				if match(m) {
					return m
				}
			case <-deadline:
				t.Fatalf("timed out waiting for %s", what)
			}
		}
	}
	created := next("SERVER_CREATE", func(m *protocol.Message) bool { return m.Type == protocol.EventServerCreate })
	var sc struct {
		Server *models.Server `json:"server"`
	}
	_ = json.Unmarshal(created.Data, &sc)
	if sc.Server == nil {
		var s models.Server
		_ = json.Unmarshal(created.Data, &s)
		sc.Server = &s
	}

	req, _ := protocol.NewMessage(protocol.OpChannelCreate, protocol.ChannelCreateRequest{
		ServerID: sc.Server.ID, Name: "hello", Type: models.ChannelTypePlugin, PluginID: "HelloPlugin", PluginChannelKind: "counter",
	})
	_ = conn.Send(req)
	chMsg := next("CHANNEL_CREATE", func(m *protocol.Message) bool { return m.Type == protocol.EventChannelCreate })
	var chPayload protocol.ChannelCreatePayload
	_ = json.Unmarshal(chMsg.Data, &chPayload)
	ch := chPayload.Channel
	if ch == nil || ch.Type != models.ChannelTypePlugin {
		t.Fatalf("CHANNEL_CREATE = %s", chMsg.Data)
	}

	a := newLayoutTestApp(t, 160, 45)
	a.view = ViewMain
	a.focus = FocusChannelList
	scConn, err := a.connMgr.AddServer(&ClientServerInfo{ID: sc.Server.ID})
	if err != nil {
		t.Fatal(err)
	}
	scConn.Connection = conn
	a.activeConn = scConn
	a.currentChannel = ch
	a.enterPluginPane(ch)

	// Drive the app: timers via pump, server events via the real dispatch.
	deadline := time.Now().Add(8 * time.Second)
	pump(t, a, tea.WindowSizeMsg{Width: 160, Height: 45}, time.Second)
	for time.Now().Before(deadline) {
		select {
		case m := <-incoming:
			if m.Op == protocol.OpDispatch {
				a.handleDispatch(scConn.ServerID, m)
			}
		case <-time.After(50 * time.Millisecond):
		}
		if a.pluginPane != nil && a.pluginPane.Frame != "" {
			t.Logf("frame on screen: %q", a.pluginPane.Frame)
			return
		}
	}
	t.Fatalf("no frame arrived; pane state %+v", *a.pluginPane)
}
