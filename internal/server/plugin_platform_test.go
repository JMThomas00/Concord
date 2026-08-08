package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// helloPluginManifest mirrors internal/plugins/testdata/HelloPlugin/plugin.toml.
// Duplicated inline (rather than referenced by relative path) so this test
// doesn't depend on another package's testdata layout.
const helloPluginManifest = `
[plugin]
id = "HelloPlugin"
name = "Hello Plugin"
version = "0.1.0"

[process]
working_dir = "."
restart_on_crash = true
max_restarts = 5
restart_backoff_seconds = 1

[process.entrypoint.windows]
bin = "testplugin.exe"
[process.entrypoint.linux]
bin = "testplugin"
[process.entrypoint.darwin]
bin = "testplugin"

[[channel_kind]]
kind = "counter"
display_name = "Hello Counter"
remote_pane = true

[[server_config_field]]
key = "activity_notify_channel"
label = "Activity Notification Channel"
type = "channel_select"
`

// startTestPluginServer builds the testplugin fixture binary, writes its
// manifest, boots a real server against it, and waits for HelloPlugin to
// finish identifying (status "running"). Shared by every plugin platform
// integration test in this file.
func startTestPluginServer(t *testing.T) (srv *Server, defaultServer *models.Server, wsURL string) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping plugin platform integration test in short mode")
	}

	tmpDir := t.TempDir()
	pluginsDir := filepath.Join(tmpDir, "Plugins")
	pluginDir := filepath.Join(pluginsDir, "HelloPlugin")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	binName := "testplugin"
	if runtime.GOOS == "windows" {
		binName = "testplugin.exe"
	}
	binPath := filepath.Join(pluginDir, binName)

	buildCmd := exec.Command("go", "build", "-tags", "novoice", "-o", binPath, "../../cmd/testplugin")
	if out, err := buildCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to build testplugin fixture binary: %v\n%s", err, out)
	}

	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.toml"), []byte(helloPluginManifest), 0644); err != nil {
		t.Fatalf("failed to write plugin.toml fixture: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "concord.db")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	seedDB, err := database.New(dbPath)
	if err != nil {
		t.Fatalf("failed to open seed db: %v", err)
	}
	seededServer, _, err := seedDB.EnsureDefaultServer("Test Server")
	if err != nil {
		t.Fatalf("failed to ensure default server: %v", err)
	}
	notifyChannel := models.NewTextChannel(seededServer.ID, "plugin-activity")
	if err := seedDB.CreateChannel(notifyChannel); err != nil {
		t.Fatalf("failed to create notify channel: %v", err)
	}
	seedDB.Close()

	config := DefaultConfig()
	config.Host = "127.0.0.1"
	config.Port = port
	config.DatabasePath = dbPath
	config.ServerName = "Test Server"
	config.PluginsDir = pluginsDir
	config.MessagePruning.Enabled = false

	srv, err = New(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	// server.New already ran plugins.Manager.LoadAll synchronously, which
	// provisions the installed_plugins row (satisfying the FK below) before
	// spawning HelloPlugin's process in the background.
	if err := srv.db.SetPluginServerConfig("HelloPlugin", "activity_notify_channel", notifyChannel.ID.String()); err != nil {
		t.Fatalf("failed to seed plugin server config: %v", err)
	}

	go func() {
		if err := srv.Run(); err != nil {
			log.Printf("test server exited: %v", err)
		}
	}()
	t.Cleanup(func() {
		if srv.httpServer != nil {
			srv.httpServer.Close()
		}
		srv.plugins.Shutdown()
		srv.db.Close()
	})

	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if installed, err := srv.db.GetInstalledPlugin("HelloPlugin"); err == nil && installed != nil && installed.Status == models.PluginStatusRunning {
			return srv, seededServer, fmt.Sprintf("ws://127.0.0.1:%d/ws", port)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("HelloPlugin never reached status=running")
	return nil, nil, ""
}

// TestPluginPlatformEndToEnd exercises the full plugin platform lifecycle
// against a real server: discover the HelloPlugin fixture, spawn its process,
// have it identify over a real WebSocket connection, and confirm its startup
// "notify" event lands as a real system message in a real channel — proving
// the discover -> spawn -> identify -> relay -> sendSystemMessage chain works
// end-to-end without any Tukan-specific code.
func TestPluginPlatformEndToEnd(t *testing.T) {
	srv, defaultServer, _ := startTestPluginServer(t)

	notifyChannel, err := findChannelByName(srv, defaultServer.ID, "plugin-activity")
	if err != nil {
		t.Fatalf("failed to look up notify channel: %v", err)
	}

	deadline := time.Now().Add(10 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		messages, err := srv.db.GetChannelMessages(notifyChannel.ID, 10, nil, uuid.Nil)
		if err == nil {
			for _, m := range messages {
				if m.Content == "Hello plugin is online." {
					found = true
					break
				}
			}
		}
		if found {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !found {
		t.Fatal("plugin activity notification never arrived")
	}
}

// TestPluginPaneRoundTrip drives the exact wire protocol Concord's real
// client (internal/client/plugin_pane.go) uses for a remote-pane channel: a
// real "human" WebSocket connection identifies, opens a plugin channel
// (OpPluginPaneEnter), receives HelloPlugin's initial rendered frame, sends a
// keypress (OpPluginPaneInput), and confirms the counter frame it gets back
// incremented — proving the full enter -> relay -> plugin -> relay -> frame
// round trip that the TUI client depends on, without requiring an actual
// interactive terminal to drive the bubbletea client itself.
func TestPluginPaneRoundTrip(t *testing.T) {
	srv, defaultServer, wsURL := startTestPluginServer(t)

	// Create a plugin channel of HelloPlugin's "counter" kind directly via
	// the DB (equivalent to what HandleCreateChannel does after validating a
	// ChannelCreateRequest — this test is about the pane relay, not channel
	// creation, which Phase 2's client-side form change already covers).
	channel := models.NewPluginChannel(defaultServer.ID, "hello-counter", "HelloPlugin", "counter")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create plugin channel: %v", err)
	}

	// Provision a real human user + session token, the same way a normal
	// client authenticates.
	user := models.NewUser("tester", "tester@test.local")
	if err := srv.db.CreateUser(user, "unused-hash"); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	token, err := srv.handlers.CreateAuthToken(user.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	defer conn.Close()

	send := func(op protocol.OpCode, payload interface{}) {
		msg, err := protocol.NewMessage(op, payload)
		if err != nil {
			t.Fatalf("failed to build message: %v", err)
		}
		raw, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("failed to marshal message: %v", err)
		}
		if err := conn.WriteMessage(websocket.TextMessage, raw); err != nil {
			t.Fatalf("failed to send message: %v", err)
		}
	}
	readUntil := func(timeout time.Duration, match func(*protocol.Message) bool) *protocol.Message {
		conn.SetReadDeadline(time.Now().Add(timeout))
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("read failed while waiting for message: %v", err)
			}
			var msg protocol.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}
			if match(&msg) {
				return &msg
			}
		}
	}

	send(protocol.OpIdentify, protocol.IdentifyPayload{Token: token})
	readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpReady })

	send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: channel.ID, Width: 80, Height: 24})

	firstFrame := readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginPaneFrame })
	var framePayload protocol.PluginPaneFramePayload
	if err := json.Unmarshal(firstFrame.Data, &framePayload); err != nil {
		t.Fatalf("failed to parse first frame: %v", err)
	}
	if framePayload.Seq != 0 {
		t.Fatalf("expected initial frame Seq=0, got %d", framePayload.Seq)
	}

	send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: channel.ID, KeyString: "x"})

	secondFrame := readUntil(5*time.Second, func(m *protocol.Message) bool {
		if m.Type != protocol.EventPluginPaneFrame {
			return false
		}
		var p protocol.PluginPaneFramePayload
		return json.Unmarshal(m.Data, &p) == nil && p.Seq == 1
	})
	if err := json.Unmarshal(secondFrame.Data, &framePayload); err != nil {
		t.Fatalf("failed to parse second frame: %v", err)
	}
	if framePayload.Seq != 1 {
		t.Fatalf("expected frame after input to have Seq=1 (counter incremented), got %d", framePayload.Seq)
	}

	send(protocol.OpPluginPaneLeave, protocol.PluginPaneLeavePayload{ChannelID: channel.ID})
}

// findChannelByName looks up a channel by name within a server — a small
// test-only helper since these tests don't go through the client's channel cache.
func findChannelByName(srv *Server, serverID uuid.UUID, name string) (*models.Channel, error) {
	channels, err := srv.db.GetServerChannels(serverID)
	if err != nil {
		return nil, err
	}
	for _, ch := range channels {
		if ch.Name == name {
			return ch, nil
		}
	}
	return nil, fmt.Errorf("channel %q not found", name)
}
