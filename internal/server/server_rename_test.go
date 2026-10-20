package server

import (
	"path/filepath"
	"testing"
)

// Changing server_name renames the server on the next start. It used to be
// read only when the database was created, so the server kept its first name
// while the Grapevine listing and emails used the new one.
func TestServerNameRenamesOnStart(t *testing.T) {
	dir := t.TempDir()
	start := func(name string) string {
		t.Helper()
		config := DefaultConfig()
		config.DatabasePath = filepath.Join(dir, "concord.db")
		config.PluginsDir = filepath.Join(dir, "Plugins")
		config.MessagePruning.Enabled = false
		config.ServerName = name
		srv, err := New(config)
		if err != nil {
			t.Fatal(err)
		}
		defer srv.db.Close()
		servers, err := srv.db.GetAllServers()
		if err != nil || len(servers) != 1 {
			t.Fatalf("servers: %v, %v", servers, err)
		}
		return servers[0].Name
	}
	if got := start("First Name"); got != "First Name" {
		t.Fatalf("created as %q", got)
	}
	if got := start("Second Name"); got != "Second Name" {
		t.Fatalf("after changing server_name: %q", got)
	}
}
