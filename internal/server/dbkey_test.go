package server

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
)

func dbKeyConfig(t *testing.T) (*Config, string) {
	t.Helper()
	dir := t.TempDir()
	c := DefaultConfig()
	c.DatabasePath = filepath.Join(dir, "data", "concord.db")
	os.MkdirAll(filepath.Dir(c.DatabasePath), 0o700)
	return c, filepath.Join(dir, "concord-server.toml")
}

// A new database gets a key beside the settings file (not in the data
// folder); the same key opens it next time; an existing unencrypted
// database stays as it is; a missing key is an error, never a new key.
func TestResolveDatabaseKey(t *testing.T) {
	c, cfg := dbKeyConfig(t)
	key, info, err := ResolveDatabaseKey(c, cfg, nil)
	if err != nil || len(key.Raw) != database.KeySize || !info.Encrypted {
		t.Fatalf("new: %v %+v %v", key.Encrypted(), info, err)
	}
	if info.KeyFile != filepath.Join(filepath.Dir(cfg), DefaultDatabaseKeyName) {
		t.Errorf("key file at %s, want beside the settings file", info.KeyFile)
	}
	if fi, _ := os.Stat(info.KeyFile); fi.Mode().Perm() != 0o600 {
		t.Errorf("key file mode %v", fi.Mode())
	}
	db, err := database.Open(c.DatabasePath, key)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	again, _, err := ResolveDatabaseKey(c, cfg, nil)
	if err != nil || !bytes.Equal(again.Raw, key.Raw) {
		t.Fatalf("second start: %v", err)
	}
	os.Rename(info.KeyFile, info.KeyFile+".moved")
	if _, _, err := ResolveDatabaseKey(c, cfg, nil); err == nil || !strings.Contains(err.Error(), "isn't at") {
		t.Errorf("missing key: %v", err)
	}
	if _, err := os.Stat(info.KeyFile); err == nil {
		t.Error("a new key was made for an existing encrypted database")
	}
	c.Database.KeyFile = info.KeyFile + ".moved" // key_file says where it went
	if k, _, err := ResolveDatabaseKey(c, cfg, nil); err != nil || !bytes.Equal(k.Raw, key.Raw) {
		t.Errorf("key_file: %v", err)
	}

	// An existing unencrypted database: opened as it is.
	p, pcfg := dbKeyConfig(t)
	plain, _ := database.New(p.DatabasePath)
	plain.Close()
	if k, info, err := ResolveDatabaseKey(p, pcfg, nil); err != nil || k.Encrypted() || info.Encrypted {
		t.Errorf("plain: %v %+v %v", k.Encrypted(), info, err)
	}
	// encrypt = false: new databases aren't.
	n, ncfg := dbKeyConfig(t)
	off := false
	n.Database.Encrypt = &off
	if k, _, err := ResolveDatabaseKey(n, ncfg, nil); err != nil || k.Encrypted() {
		t.Errorf("encrypt = false: %v %v", k.Encrypted(), err)
	}
	// A passphrase: asked (twice for a new database), never without a terminal.
	a, acfg := dbKeyConfig(t)
	a.Database.AskPassphrase = true
	if _, _, err := ResolveDatabaseKey(a, acfg, nil); err == nil {
		t.Error("a passphrase without a terminal")
	}
	confirmed := false
	k, info, err := ResolveDatabaseKey(a, acfg, func(_ string, confirm bool) (string, error) { confirmed = confirm; return "a long passphrase", nil })
	if err != nil || k.Passphrase != "a long passphrase" || !info.Passphrase || !confirmed {
		t.Errorf("passphrase: %+v %+v %v (confirm %v)", k, info, err, confirmed)
	}
	if _, _, err := ResolveDatabaseKey(a, acfg, func(string, bool) (string, error) { return "short", nil }); err == nil {
		t.Error("a short passphrase was taken")
	}
}

// --encrypt-database and back: the messages survive, the original is kept
// aside, and the server opens the result.
func TestConvertDatabase(t *testing.T) {
	c, cfg := dbKeyConfig(t)
	db, _ := database.New(c.DatabasePath)
	u := models.NewUser("conv", "conv@example.com")
	db.CreateUser(u, "x")
	srv := models.NewServer("S", u.ID)
	db.CreateServer(srv)
	ch := models.NewTextChannel(srv.ID, "general")
	db.CreateChannel(ch)
	for i := 0; i < 5; i++ {
		db.CreateMessage(models.NewMessage(ch.ID, u.ID, "plain words"))
	}
	db.Close()

	out, err := ConvertDatabase(c, cfg, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "5 messages") {
		t.Errorf("said %q", out)
	}
	raw, _ := os.ReadFile(c.DatabasePath)
	if bytes.Contains(raw, []byte("plain words")) || bytes.HasPrefix(raw, sqliteHeader) {
		t.Error("still readable after encrypting")
	}
	olds, _ := filepath.Glob(c.DatabasePath + ".unencrypted-*")
	if len(olds) == 0 {
		t.Error("the original wasn't kept")
	} else if fi, _ := os.Stat(olds[0]); fi.Mode().Perm() != 0o600 {
		t.Errorf("the unencrypted original is mode %v, want 0600", fi.Mode())
	}
	if _, err := ConvertDatabase(c, cfg, true, nil); err == nil {
		t.Error("encrypted twice")
	}
	key, info, err := ResolveDatabaseKey(c, cfg, nil)
	if err != nil || !info.Encrypted {
		t.Fatalf("after encrypting: %v", err)
	}
	c.DatabaseKey, c.DatabaseKeyInfo = key, info
	s, err := New(c)
	if err != nil {
		t.Fatalf("the server doesn't open it: %v", err)
	}
	if _, msgs, _ := s.db.ContentCounts(); msgs != 5 {
		t.Errorf("%d messages", msgs)
	}
	if st := s.adminStatus(); !st.DatabaseEncrypted || st.DatabaseKeyFile == "" {
		t.Errorf("status %+v", st)
	}
	s.db.Close()

	if _, err := ConvertDatabase(c, cfg, false, nil); err != nil {
		t.Fatal(err)
	}
	if raw, _ := os.ReadFile(c.DatabasePath); !bytes.HasPrefix(raw, sqliteHeader) {
		t.Error("not unencrypted after decrypting")
	}

	// A key file already where a new one would go: refused, not mixed up.
	e, ecfg := dbKeyConfig(t)
	pdb, _ := database.New(e.DatabasePath)
	pdb.Close()
	os.WriteFile(filepath.Join(filepath.Dir(ecfg), DefaultDatabaseKeyName), []byte("old"), 0o600)
	if _, err := ConvertDatabase(e, ecfg, true, nil); err == nil || !strings.Contains(err.Error(), "already") {
		t.Errorf("over an old key: %v", err)
	}
}
