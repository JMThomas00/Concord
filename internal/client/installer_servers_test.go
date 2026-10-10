package client

import (
	"path/filepath"
	"testing"

	"github.com/concord-chat/concord/internal/installer"
)

// The installer adds servers to servers.json itself (the official server,
// a server installed alongside); the client must load them.
func TestClientLoadsServersTheInstallerAdded(t *testing.T) {
	dir := t.TempDir()
	if _, err := installer.AddClientServers(dir, []installer.ClientServer{{Name: "Concord", Address: "concord.example", Port: 443, TLS: true}}); err != nil {
		t.Fatal(err)
	}
	cm := &ConfigManager{serversFilePath: filepath.Join(dir, "servers.json")}
	cfg, err := cm.LoadServers()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Servers) != 1 {
		t.Fatalf("%d servers", len(cfg.Servers))
	}
	s := cfg.Servers[0]
	if s.Name != "Concord" || s.GetWebSocketURL() != "wss://concord.example:443/ws" || s.IconLetter != "Co" || s.IconColor == "" || s.AddedAt.IsZero() {
		t.Fatalf("loaded %+v", s)
	}
}
