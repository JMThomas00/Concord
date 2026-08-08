package server

import (
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
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

// TestPluginPlatformEndToEnd exercises the full plugin platform lifecycle
// against a real server: discover the HelloPlugin fixture, spawn its process,
// have it identify over a real WebSocket connection, and confirm its startup
// "notify" event lands as a real system message in a real channel — proving
// the discover -> spawn -> identify -> relay -> sendSystemMessage chain works
// end-to-end without any Tukan-specific code.
func TestPluginPlatformEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping plugin platform end-to-end test in short mode")
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
	defaultServer, _, err := seedDB.EnsureDefaultServer("Test Server")
	if err != nil {
		t.Fatalf("failed to ensure default server: %v", err)
	}
	notifyChannel := models.NewTextChannel(defaultServer.ID, "plugin-activity")
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

	srv, err := New(config)
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
	var found bool
	var lastStatus, lastError string
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
		if installed, err := srv.db.GetInstalledPlugin("HelloPlugin"); err == nil && installed != nil {
			lastStatus, lastError = installed.Status, installed.LastError
		}
		time.Sleep(250 * time.Millisecond)
	}

	if !found {
		t.Fatalf("plugin activity notification never arrived (last plugin status=%q lastError=%q)", lastStatus, lastError)
	}
}
