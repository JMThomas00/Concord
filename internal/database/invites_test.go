package database

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
)

func TestInvites(t *testing.T) {
	f := newReadsFixture(t)
	now := time.Now()
	if m, _ := f.db.JoinMode(f.srv.ID); m != JoinOpen {
		t.Fatalf("a server starts %q", m)
	}
	if err := f.db.SetJoinMode(f.srv.ID, "closed"); err == nil {
		t.Error("a made-up join mode was saved")
	}

	two := &Invite{Code: "TWOUSES2", ServerID: f.srv.ID, InviterID: f.alice.ID, MaxUses: 2, ExpiresAt: now.Add(time.Hour)}
	if err := f.db.CreateInvite(two); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := f.db.UseInvite("TWOUSES2", now); err != nil {
			t.Fatalf("use %d: %v", i+1, err)
		}
	}
	if err := f.db.UseInvite("TWOUSES2", now); !errors.Is(err, ErrInviteUsedUp) {
		t.Errorf("a third use: %v", err)
	}
	if err := f.db.UseInvite("TWOUSES2", now.Add(2*time.Hour)); !errors.Is(err, ErrInviteExpired) {
		t.Errorf("after expiring: %v", err)
	}
	if _, err := f.db.GetInvite("NOSUCHXX"); !errors.Is(err, ErrInviteNotFound) {
		t.Errorf("unknown code: %v", err)
	}

	mailed := &Invite{Code: "MAILED22", ServerID: f.srv.ID, InviterID: f.bob.ID, Email: "Pal@Example.com", MaxUses: 1}
	f.db.CreateInvite(mailed)
	if p, _ := f.db.PendingEmailInvite(f.srv.ID, "pal@example.com", now); p == nil || p.Code != "MAILED22" || !p.ExpiresAt.IsZero() {
		t.Fatalf("pending %+v", p)
	}
	if n, _ := f.db.EmailInvitesSince(f.bob.ID, now.Add(-time.Hour)); n != 1 {
		t.Errorf("bob emailed %d", n)
	}
	if all, _ := f.db.ListInvites(f.srv.ID, [16]byte{}); len(all) != 2 {
		t.Errorf("%d invites", len(all))
	}
	if mine, _ := f.db.ListInvites(f.srv.ID, f.bob.ID); len(mine) != 1 {
		t.Errorf("bob's: %d", len(mine))
	}
	f.db.RevokeInvite("MAILED22")
	if p, _ := f.db.PendingEmailInvite(f.srv.ID, "pal@example.com", now); p != nil {
		t.Error("a cancelled invite is still waiting")
	}
	if err := f.db.UseInvite("MAILED22", now); !errors.Is(err, ErrInviteRevoked) {
		t.Errorf("a cancelled invite: %v", err)
	}

	// Requests: a note kept, a no remembered.
	f.db.AddJoinRequest(JoinRequest{UserID: f.bob.ID, ServerID: f.srv.ID, Note: "hi"})
	f.db.AddJoinRequest(JoinRequest{UserID: f.bob.ID, ServerID: f.srv.ID})
	if r, _ := f.db.GetJoinRequest(f.srv.ID, f.bob.ID); r == nil || r.Note != "hi" {
		t.Fatalf("request %+v", r)
	}
	f.db.DenyJoinRequest(f.srv.ID, f.bob.ID)
	if r, _ := f.db.GetJoinRequest(f.srv.ID, f.bob.ID); r == nil || !r.Denied {
		t.Errorf("denied %+v", r)
	}
	if l, _ := f.db.ListJoinRequests(f.srv.ID); len(l) != 0 {
		t.Errorf("listed %+v", l)
	}
}

// Roles made from the Moderator preset gain Create Invite once.
func TestModeratorsGainCreateInvite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	owner := models.NewUser("inv-owner", "inv-owner@test.com")
	db.CreateUser(owner, "x")
	s := models.NewServer("S", owner.ID)
	db.CreateServer(s)
	mod := models.NewRole(s.ID, "Moderator")
	mod.Permissions = models.PermissionsModerator &^ models.PermissionCreateInvite
	db.CreateRole(mod)
	helper := models.NewRole(s.ID, "Helper")
	db.CreateRole(helper)
	db.Exec(`DELETE FROM migrations_done WHERE name = '2026-11-05-moderator-invites'`)
	db.Close()
	if db, err = New(path); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, _ := db.GetRoleByID(mod.ID)
	if got.Permissions != models.PermissionsModerator {
		t.Errorf("Moderator: %b", got.Permissions)
	}
	if h, _ := db.GetRoleByID(helper.ID); h.Permissions&models.PermissionCreateInvite != 0 {
		t.Error("an unrelated role gained Create Invite")
	}
}
