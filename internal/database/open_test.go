package database

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite" // the old driver, to write a database the way servers before v0.2.0 did

	"github.com/concord-chat/concord/internal/models"
)

// A database written by the old driver opens, and its times (Go's debug
// form in the server's zone, monotonic reading and all) come back right,
// stored as UTC.
func TestOldTimesAreConverted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	u := models.NewUser("old", "old@example.com")
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	db.Close()

	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	cst := time.FixedZone("CST", -6*3600)
	want := time.Date(2026, 11, 4, 16, 54, 48, 830000000, cst)
	if _, err := old.Exec(`UPDATE users SET created_at = ? WHERE id = ?`, want.String()+" m=+2.031", u.ID.String()); err != nil {
		t.Fatal(err)
	}
	old.Close()

	db, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var stored string
	db.QueryRow(`SELECT CAST(created_at AS TEXT) FROM users WHERE id = ?`, u.ID.String()).Scan(&stored)
	if stored != "2026-11-04 22:54:48" {
		t.Errorf("stored as %q, want UTC to the second", stored)
	}
	got, err := db.GetUserByID(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.CreatedAt.Equal(want.Truncate(time.Second)) {
		t.Errorf("created %v, want %v", got.CreatedAt, want)
	}
}

func TestParseStoredTime(t *testing.T) {
	want := time.Date(2026, 11, 5, 2, 41, 15, 0, time.UTC)
	for _, s := range []string{
		"2026-11-05 02:41:15 +0000 UTC m=+8688.856348389",
		"2026-11-04 20:41:15 -0600 CST",
		"2026-11-05T02:41:15Z",
		"2026-11-04T20:41:15-06:00",
		"2026-11-05 02:41:15",
	} {
		got, ok := parseStoredTime(s)
		if !ok || !got.Equal(want) {
			t.Errorf("%q: %v %v", s, got, ok)
		}
	}
	if _, ok := parseStoredTime("not a time"); ok {
		t.Error("parsed nonsense")
	}
}

// An encrypted database: nothing readable on disk, the wrong key or none
// opens nothing, the right one opens it, and a backup copy comes out
// unencrypted (the backup encrypts it with its own key).
func TestEncryptedDatabase(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concord.db")
	key := Key{Raw: bytes.Repeat([]byte{7}, KeySize)}
	db, err := Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	u := models.NewUser("secretname", "secret@example.com")
	if err := db.CreateUser(u, "x"); err != nil {
		t.Fatal(err)
	}
	if err := db.BackupTo(filepath.Join(dir, "copy.db")); err != nil {
		t.Fatal(err)
	}
	db.Close()

	for _, f := range []string{path, path + "-wal"} {
		raw, _ := os.ReadFile(f)
		if bytes.Contains(raw, []byte("secretname")) || bytes.Contains(raw, []byte("SQLite format")) {
			t.Errorf("%s is readable", filepath.Base(f))
		}
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, "copy.db")); !bytes.Contains(raw, []byte("SQLite format")) {
		t.Error("the backup copy isn't a plain database")
	}
	for _, k := range []Key{{}, {Raw: bytes.Repeat([]byte{8}, KeySize)}, {Passphrase: "guess"}} {
		if d, err := Open(path, k); !errors.Is(err, ErrWrongKey) {
			if d != nil {
				d.Close()
			}
			t.Errorf("key %+v: %v, want ErrWrongKey", k.Encrypted(), err)
		}
	}
	db, err = Open(path, key)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if got, err := db.GetUserByID(u.ID); err != nil || got.Username != "secretname" {
		t.Fatalf("read back: %v %v", got, err)
	}

	// A passphrase works the same way.
	pp := filepath.Join(dir, "pass.db")
	d2, err := Open(pp, Key{Passphrase: "correct horse battery staple"})
	if err != nil {
		t.Fatal(err)
	}
	d2.Close()
	if _, err := Open(pp, Key{Passphrase: "wrong"}); !errors.Is(err, ErrWrongKey) {
		t.Errorf("wrong passphrase: %v", err)
	}
	_ = uuid.Nil
}

func TestQuoteIdent(t *testing.T) {
	for in, want := range map[string]string{`messages`: `"messages"`, `we"ird`: `"we""ird"`, `a\b`: `"a\b"`} {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
}
