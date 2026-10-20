package database

import (
	"path/filepath"
	"testing"

	"github.com/concord-chat/concord/internal/models"
)

// An existing server's @everyone loses Manage Channels once, a role made
// from the old Moderator preset gains it, other roles are untouched, and an
// admin who gives @everyone Manage Channels back keeps it across restarts.
func TestEveryoneLosesManageChannelsOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := models.NewUser("mc-owner", "mc-owner@test.com")
	if err := db.CreateUser(owner, "password123"); err != nil {
		t.Fatal(err)
	}
	s := models.NewServer("Old Server", owner.ID)
	if err := db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	role := func(name string, def bool, p models.Permission) *models.Role {
		r := models.NewRole(s.ID, name)
		r.IsDefault, r.Permissions = def, p
		if err := db.CreateRole(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	oldModerator := models.PermissionsModerator &^ models.PermissionManageChannels
	everyone := role("everyone", true, models.NewEveryoneRole(s.ID).Permissions|models.PermissionManageChannels) // as before
	mod := role("Moderator", false, oldModerator)
	helper := role("Helper", false, models.PermissionsText)
	if _, err := db.Exec(`DELETE FROM migrations_done WHERE name = '2026-10-20-everyone-manage-channels'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	perms := func(db *DB, r *models.Role) models.Permission {
		var p int64
		if err := db.QueryRow(`SELECT permissions FROM roles WHERE id = ?`, r.ID.String()).Scan(&p); err != nil {
			t.Fatal(err)
		}
		return models.Permission(p)
	}
	if db, err = New(path); err != nil {
		t.Fatal(err)
	}
	if p := perms(db, everyone); p&models.PermissionManageChannels != 0 || p&models.PermissionSendMessages == 0 {
		t.Fatalf("@everyone after the migration: %b", p)
	}
	if p := perms(db, mod); p != models.PermissionsModerator {
		t.Fatalf("Moderator after the migration: %b, want the new preset", p)
	}
	if p := perms(db, helper); p != models.PermissionsText {
		t.Fatalf("an unrelated role changed: %b", p)
	}
	// An admin deliberately gives it back; a restart mustn't take it again.
	if _, err := db.Exec(`UPDATE roles SET permissions = permissions | ? WHERE id = ?`, int64(models.PermissionManageChannels), everyone.ID.String()); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if db, err = New(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if perms(db, everyone)&models.PermissionManageChannels == 0 {
		t.Fatal("a restart took Manage Channels away again")
	}
}

// Plugins' service accounts aren't people: one created before the first
// person registers mustn't stop that person becoming the owner.
func TestCountRealUsersSkipsServiceAccounts(t *testing.T) {
	db, err := New(filepath.Join(t.TempDir(), "concord.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	bot := models.NewUser("a-plugin", "a-plugin@plugins.local")
	bot.IsServiceAccount = true
	if err := db.CreateUser(bot, ""); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CountRealUsers(); err != nil || n != 0 {
		t.Fatalf("with only a plugin: %d, %v", n, err)
	}
	person := models.NewUser("first", "first@test.com")
	if err := db.CreateUser(person, "password123"); err != nil {
		t.Fatal(err)
	}
	if n, err := db.CountRealUsers(); err != nil || n != 1 {
		t.Fatalf("with a plugin and one person: %d, %v", n, err)
	}
}
