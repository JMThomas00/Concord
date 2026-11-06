//go:build !windows

package client

import (
	"os"
	"path/filepath"
	"testing"
)

// Finding 9 (security audit 2026-11-05): ~/.concord was 0755 and
// servers.json (sign-in tokens) 0644, readable by every account on the
// computer.
func TestTheConcordFolderIsPrivate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".concord")
	// An older install: open folder, open files.
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.Chmod(dir, 0o755)
	old := filepath.Join(dir, "servers.json")
	os.WriteFile(old, []byte(`{"servers":[]}`), 0o644)
	os.Chmod(old, 0o644)

	cm, err := NewConfigManager()
	if err != nil {
		t.Fatal(err)
	}
	if err := cm.SaveAppConfig(&AppConfig{}); err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("~/.concord is %v", fi.Mode().Perm())
	}
	for _, name := range []string{"servers.json", "config.json"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm()&0o077 != 0 {
			t.Errorf("%s is %v", name, fi.Mode().Perm())
		}
	}
}
