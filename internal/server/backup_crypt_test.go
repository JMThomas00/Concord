package server

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/database"
)

// A backup holds the database, the settings, the plugin secrets key and
// the plugins' data, never the backup key; nothing in it is readable
// without the key, and the wrong key opens nothing.
func TestEncryptedBackupContents(t *testing.T) {
	srv := idleServer(t)
	data := filepath.Dir(srv.config.DatabasePath)
	srv.config.PluginsDir = filepath.Join(data, "Plugins")
	board := filepath.Join(data, "PluginData", "Tukan", "boards.json")
	os.MkdirAll(filepath.Dir(board), 0o700)
	os.WriteFile(board, []byte(`{"board":"groceries"}`), 0o600)
	cfg := filepath.Join(data, "concord-server.toml")
	os.WriteFile(cfg, []byte("server_name = \"Vineyard\"\n"), 0o600)
	srv.SetConfigPath(cfg)
	os.WriteFile(filepath.Join(data, "plugin-secrets.key"), []byte("plugin secret"), 0o600)

	if _, err := srv.backupNow(); err != nil {
		t.Fatal(err)
	}
	b := listBackups(srv.backupsDir())
	if len(b) != 1 {
		t.Fatalf("backups %+v", b)
	}
	file := filepath.Join(srv.backupsDir(), b[0].name)
	raw, _ := os.ReadFile(file)
	for _, plain := range []string{"SQLite format", "groceries", "plugin secret", "Vineyard"} {
		if bytes.Contains(raw, []byte(plain)) {
			t.Errorf("%q is readable in the backup file", plain)
		}
	}
	if !bytes.HasPrefix(raw, []byte("age-encryption.org/v1")) {
		t.Errorf("not an age file: %q", raw[:min(30, len(raw))])
	}
	if leftovers, _ := filepath.Glob(filepath.Join(srv.backupsDir(), ".snapshot-*")); len(leftovers) > 0 {
		t.Errorf("the unencrypted snapshot was left behind: %v", leftovers)
	}

	key, err := os.ReadFile(backupKeyPath(srv.config.DatabasePath))
	if err != nil {
		t.Fatal(err)
	}
	if fi, _ := os.Stat(backupKeyPath(srv.config.DatabasePath)); fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v", fi.Mode())
	}
	out := filepath.Join(t.TempDir(), "restore")
	wrote, err := OpenBackup(file, string(key), out)
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(wrote)
	want := []string{"PluginData/Tukan/boards.json", "concord-server.toml", "concord.db", "plugin-secrets.key"}
	if strings.Join(wrote, ",") != strings.Join(want, ",") {
		t.Errorf("backup holds %v, want %v", wrote, want)
	}
	if got, _ := os.ReadFile(filepath.Join(out, "PluginData", "Tukan", "boards.json")); string(got) != `{"board":"groceries"}` {
		t.Errorf("board %q", got)
	}
	db, err := database.New(filepath.Join(out, "concord.db"))
	if err != nil {
		t.Fatalf("the backed-up database doesn't open: %v", err)
	}
	db.Close()

	// The wrong key, and an existing folder, open nothing.
	other, _, _ := loadBackupKey(filepath.Join(t.TempDir(), "other.key"))
	if _, err := OpenBackup(file, other.String(), filepath.Join(t.TempDir(), "x")); err == nil {
		t.Error("another key opened the backup")
	}
	if _, err := OpenBackup(file, string(key), out); err == nil {
		t.Error("unpacked over an existing folder")
	}
	// The key alone (pasted, not the file) works too.
	if _, err := OpenBackup(file, strings.TrimSpace(strings.Split(string(key), "\n")[2]), filepath.Join(t.TempDir(), "y")); err != nil {
		t.Errorf("the bare key: %v", err)
	}
}

// The key is made once and then kept.
func TestBackupKeyIsKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "backup.key")
	a, made, err := loadBackupKey(path)
	if err != nil || !made {
		t.Fatalf("first: %v %v", made, err)
	}
	b, made, err := loadBackupKey(path)
	if err != nil || made || a.String() != b.String() {
		t.Fatalf("second: made %v, same %v, %v", made, a.String() == b.String(), err)
	}
	if _, err := BackupKeyText(filepath.Join(t.TempDir(), "concord.db")); err == nil {
		t.Error("--backup-key made a key")
	}
}

// Scheduled backups: due ten minutes after start with none, then a day
// after the newest; off with every_hours = 0; a failure waits an hour.
func TestScheduledBackupTimes(t *testing.T) {
	srv := idleServer(t)
	srv.config.Backups.EveryHours = 24
	start := srv.admin.startedAt
	if got := srv.nextBackupAt(); !got.Equal(start.Add(firstBackupDelay)) {
		t.Errorf("no backups: next %v, want %v", got, start.Add(firstBackupDelay))
	}
	if _, err := srv.backupNow(); err != nil {
		t.Fatal(err)
	}
	newest := listBackups(srv.backupsDir())[0].at
	if got := srv.nextBackupAt(); !got.Equal(newest.Add(24 * time.Hour)) {
		t.Errorf("after one: next %v, want a day later", got)
	}
	retry := time.Now().Add(48 * time.Hour).Truncate(time.Second)
	srv.admin.backupRetryAt.Store(retry.Unix())
	if got := srv.nextBackupAt(); !got.Equal(retry) {
		t.Errorf("after a failure: next %v, want %v", got, retry)
	}
	srv.config.Backups.EveryHours = 0
	if !srv.nextBackupAt().IsZero() {
		t.Error("scheduled with every_hours = 0")
	}
}

// idleServer is a server that isn't running (no scheduled backups or other
// loops reading its settings), its files in a folder of its own.
func idleServer(t *testing.T) *Server {
	t.Helper()
	srv, cleanup := createTestServer(t)
	t.Cleanup(cleanup)
	srv.config.DatabasePath = filepath.Join(t.TempDir(), "concord.db") // where backups and the key go
	return srv
}
