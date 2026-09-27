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
	"strings"
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

// testWSClient wraps a raw WebSocket connection with the send/receive
// helpers every plugin-platform integration test in this file needs —
// factored out so each test isn't reimplementing its own protocol plumbing.
type testWSClient struct {
	t    *testing.T
	conn *websocket.Conn
}

func newTestWSClient(t *testing.T, wsURL string) *testWSClient {
	t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testWSClient{t: t, conn: conn}
}

func (c *testWSClient) send(op protocol.OpCode, payload interface{}) {
	c.t.Helper()
	msg, err := protocol.NewMessage(op, payload)
	if err != nil {
		c.t.Fatalf("failed to build message: %v", err)
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		c.t.Fatalf("failed to marshal message: %v", err)
	}
	if err := c.conn.WriteMessage(websocket.TextMessage, raw); err != nil {
		c.t.Fatalf("failed to send message: %v", err)
	}
}

func (c *testWSClient) readUntil(timeout time.Duration, match func(*protocol.Message) bool) *protocol.Message {
	c.t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			c.t.Fatalf("read failed while waiting for message: %v", err)
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

// identify sends OpIdentify with token and waits for OpReady.
func (c *testWSClient) identify(token string) {
	c.send(protocol.OpIdentify, protocol.IdentifyPayload{Token: token})
	c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Op == protocol.OpReady })
}

// createTestUserAndToken provisions a real human user + session token, the
// same way a normal client authenticates.
func createTestUserAndToken(t *testing.T, srv *Server, username string) (*models.User, string) {
	t.Helper()
	user := models.NewUser(username, username+"@test.local")
	if err := srv.db.CreateUser(user, "unused-hash"); err != nil {
		t.Fatalf("failed to create test user: %v", err)
	}
	token, err := srv.handlers.CreateAuthToken(user.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatalf("failed to create auth token: %v", err)
	}
	return user, token
}

// joinWithEveryoneRole makes userID a member of serverID holding that
// server's @everyone role -- what joining a server does for a real user.
func joinWithEveryoneRole(t *testing.T, srv *Server, serverID, userID uuid.UUID) {
	t.Helper()
	if err := srv.db.AddServerMember(models.NewServerMember(userID, serverID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	roles, err := srv.db.GetServerRoles(serverID)
	if err != nil {
		t.Fatalf("failed to load roles: %v", err)
	}
	for _, r := range roles {
		if r.IsDefault {
			if err := srv.db.AddMemberRole(userID, serverID, r.ID); err != nil {
				t.Fatalf("failed to assign @everyone: %v", err)
			}
			return
		}
	}
	t.Fatalf("server %s has no @everyone role", serverID)
}

// A user who can't see a plugin channel (here: not even a member of its
// server) must not be able to open, drive or watch its pane just by
// knowing the channel ID.
func TestPluginPaneRejectsViewerWithoutAccess(t *testing.T) {
	srv, defaultServer, wsURL := startTestPluginServer(t)
	channel := models.NewPluginChannel(defaultServer.ID, "hello-counter", "HelloPlugin", "counter")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create plugin channel: %v", err)
	}

	_, token := createTestUserAndToken(t, srv, "outsider")
	client := newTestWSClient(t, wsURL)
	client.identify(token)

	client.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: channel.ID, Width: 80, Height: 24})
	got := client.readUntil(5*time.Second, func(m *protocol.Message) bool {
		// Errors arrive as a dispatch with no event type.
		return (m.Op == protocol.OpDispatch && m.Type == "") || m.Type == protocol.EventPluginPaneFrame
	})
	if got.Type == protocol.EventPluginPaneFrame {
		t.Fatal("a non-member received the plugin's frame")
	}
	var perr protocol.ErrorPayload
	if err := json.Unmarshal(got.Data, &perr); err != nil || perr.Code != protocol.ErrorCodeForbidden {
		t.Fatalf("want a Forbidden error, got %s", got.Data)
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

	viewer, token := createTestUserAndToken(t, srv, "tester")
	joinWithEveryoneRole(t, srv, defaultServer.ID, viewer.ID)
	client := newTestWSClient(t, wsURL)
	client.identify(token)

	client.send(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{ChannelID: channel.ID, Width: 80, Height: 24})

	firstFrame := client.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginPaneFrame })
	var framePayload protocol.PluginPaneFramePayload
	if err := json.Unmarshal(firstFrame.Data, &framePayload); err != nil {
		t.Fatalf("failed to parse first frame: %v", err)
	}
	// The plugin knows who's looking (server-stamped identity on Enter).
	if !strings.Contains(framePayload.Frame, "Hello, tester!") || !strings.Contains(framePayload.Frame, "seen: 0") {
		t.Fatalf("unexpected first frame %q", framePayload.Frame)
	}
	firstSeq := framePayload.Seq

	client.send(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{ChannelID: channel.ID, KeyString: "x"})

	secondFrame := client.readUntil(5*time.Second, func(m *protocol.Message) bool {
		if m.Type != protocol.EventPluginPaneFrame {
			return false
		}
		var p protocol.PluginPaneFramePayload
		return json.Unmarshal(m.Data, &p) == nil && p.Seq > firstSeq
	})
	if err := json.Unmarshal(secondFrame.Data, &framePayload); err != nil {
		t.Fatalf("failed to parse second frame: %v", err)
	}
	if !strings.Contains(framePayload.Frame, "seen: 1") {
		t.Fatalf("expected the frame after input to show the counter incremented, got %q", framePayload.Frame)
	}

	client.send(protocol.OpPluginPaneLeave, protocol.PluginPaneLeavePayload{ChannelID: channel.ID})
}

// TestPluginEnableDisableToggle verifies Settings > Plugins > toggle
// enabled/disabled actually starts/stops the plugin's Supervisor live — no
// Concord restart required — and that toggling back on works, mirroring
// what a server admin driving OpPluginConfigSet from the real client does.
func TestPluginEnableDisableToggle(t *testing.T) {
	srv, defaultServer, wsURL := startTestPluginServer(t)

	// Make the test user the server owner so checkPermission's
	// PermissionManagePlugins gate (required for OpPluginConfigSet) passes.
	admin, token := createTestUserAndToken(t, srv, "admin")
	if err := srv.db.UpdateServerOwner(defaultServer.ID, admin.ID); err != nil {
		t.Fatalf("failed to make test user server owner: %v", err)
	}

	client := newTestWSClient(t, wsURL)
	client.identify(token)

	// Disable HelloPlugin.
	disabled := false
	client.send(protocol.OpPluginConfigSet, protocol.PluginConfigSetRequest{
		ServerID: defaultServer.ID,
		PluginID: "HelloPlugin",
		Enabled:  &disabled,
	})
	resp := client.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginConfigUpdate })
	var list protocol.PluginConfigListPayload
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		t.Fatalf("failed to parse plugin config list: %v", err)
	}
	if len(list.Plugins) != 1 || list.Plugins[0].Enabled {
		t.Fatalf("expected HelloPlugin to be disabled in the response, got %+v", list.Plugins)
	}

	// Confirm the process actually stopped (Supervisor.Stop completed before
	// Manager.SetEnabled returned, which is before the response was sent).
	deadline := time.Now().Add(10 * time.Second)
	for {
		installed, err := srv.db.GetInstalledPlugin("HelloPlugin")
		if err != nil {
			t.Fatalf("failed to load installed plugin: %v", err)
		}
		if installed.Status == models.PluginStatusStopped {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin never reached status=stopped after disable, last status=%q", installed.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Re-enable it and confirm it actually restarts and re-identifies.
	enabled := true
	client.send(protocol.OpPluginConfigSet, protocol.PluginConfigSetRequest{
		ServerID: defaultServer.ID,
		PluginID: "HelloPlugin",
		Enabled:  &enabled,
	})
	client.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventPluginConfigUpdate })

	deadline = time.Now().Add(10 * time.Second)
	for {
		installed, err := srv.db.GetInstalledPlugin("HelloPlugin")
		if err != nil {
			t.Fatalf("failed to load installed plugin: %v", err)
		}
		if installed.Status == models.PluginStatusRunning {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin never returned to status=running after re-enable, last status=%q", installed.Status)
		}
		time.Sleep(100 * time.Millisecond)
	}
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

// Deleting a plugin channel (directly, or via its parent category) must
// reach the plugin that owns it. Plugin connections aren't in the
// server-wide broadcast lists, so before this was fixed a plugin kept state
// for deleted channels forever and never learned they were gone.
func TestChannelDeleteReachesOwningPlugin(t *testing.T) {
	srv, defaultServer, wsURL := startTestPluginServer(t)

	category := models.NewCategory(defaultServer.ID, "games")
	if err := srv.db.CreateChannel(category); err != nil {
		t.Fatalf("failed to create category: %v", err)
	}
	direct := models.NewPluginChannel(defaultServer.ID, "counter-a", "HelloPlugin", "counter")
	nested := models.NewPluginChannel(defaultServer.ID, "counter-b", "HelloPlugin", "counter")
	nested.CategoryID = category.ID
	for _, ch := range []*models.Channel{direct, nested} {
		if err := srv.db.CreateChannel(ch); err != nil {
			t.Fatalf("failed to create plugin channel: %v", err)
		}
	}

	// Stand in for the plugin's connection so the test can see exactly what
	// the plugin would receive.
	serviceUserID, err := srv.plugins.ServiceUserIDFor("HelloPlugin")
	if err != nil {
		t.Fatalf("no service account for HelloPlugin: %v", err)
	}
	fakePlugin := &Client{UserID: serviceUserID, IsPlugin: true, PluginID: "HelloPlugin", send: make(chan *protocol.Message, 64)}
	srv.hub.register <- fakePlugin

	admin, token := createTestUserAndToken(t, srv, "admin")
	if err := srv.db.UpdateServerOwner(defaultServer.ID, admin.ID); err != nil {
		t.Fatalf("failed to make test user server owner: %v", err)
	}
	client := newTestWSClient(t, wsURL)
	client.identify(token)

	client.send(protocol.OpChannelDelete, protocol.ChannelDeleteRequest{ServerID: defaultServer.ID, ChannelID: direct.ID})
	client.send(protocol.OpChannelDelete, protocol.ChannelDeleteRequest{ServerID: defaultServer.ID, ChannelID: category.ID})

	want := map[uuid.UUID]bool{direct.ID: true, nested.ID: true}
	deadline := time.After(5 * time.Second)
	for len(want) > 0 {
		select {
		case m := <-fakePlugin.send:
			if m.Type != protocol.EventChannelDelete {
				continue
			}
			var p protocol.ChannelDeletePayload
			if err := json.Unmarshal(m.Data, &p); err != nil {
				t.Fatalf("bad delete payload: %v", err)
			}
			if p.Type != models.ChannelTypePlugin {
				t.Errorf("delete for %s reported type %q, want plugin", p.ChannelID, p.Type)
			}
			delete(want, p.ChannelID)
		case <-deadline:
			t.Fatalf("plugin never heard about deleting %d of its channels", len(want))
		}
	}
}
