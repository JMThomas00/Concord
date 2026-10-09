package database

import (
	"path/filepath"
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// An @everyone from before voice permissions were enforced gets Connect,
// Speak and Use Voice Activity once, so nobody loses voice; an admin who
// later takes them away keeps it that way across restarts.
func TestEveryoneGetsVoiceOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := models.NewUser("vm-owner", "vm-owner@test.com")
	if err := db.CreateUser(owner, "password123"); err != nil {
		t.Fatal(err)
	}
	s := models.NewServer("Old Server", owner.ID)
	if err := db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	everyone := models.NewEveryoneRole(s.ID)
	everyone.Permissions &^= models.PermissionsVoice // as roles were before
	if err := db.CreateRole(everyone); err != nil {
		t.Fatal(err)
	}
	// Pretend this database predates the migration, then restart.
	if _, err := db.Exec(`DELETE FROM migrations_done WHERE name = '2026-10-09-everyone-voice'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	perms := func(db *DB) models.Permission {
		var p int64
		if err := db.QueryRow(`SELECT permissions FROM roles WHERE id = ?`, everyone.ID.String()).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return models.Permission(p)
	}
	db, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	if perms(db)&models.PermissionsVoice != models.PermissionsVoice {
		t.Fatal("the migration didn't give @everyone voice")
	}
	// An admin takes Speak away; a restart mustn't give it back.
	if _, err := db.Exec(`UPDATE roles SET permissions = ? WHERE id = ?`, int64(perms(db)&^models.PermissionSpeak), everyone.ID.String()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if perms(db)&models.PermissionSpeak != 0 {
		t.Fatal("a restart gave Speak back")
	}
	_ = uuid.Nil
}
