package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// setupNicknameTestServer creates a server with an owner, an @everyone
// role, and one regular member with only @everyone assigned — enough to
// exercise HandleSetNickname's self vs. other branching.
func setupNicknameTestServer(t *testing.T) (srv *Server, testServer *models.Server, member *models.User, cleanup func()) {
	t.Helper()
	srv, cleanup = createTestServer(t)

	owner := models.NewUser("nickname-owner", "nickname-owner@test.com")
	if err := srv.db.CreateUser(owner, "password123"); err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}
	testServer = models.NewServer("Nickname Test Server", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone := models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}

	member = models.NewUser("nickname-member", "nickname-member@test.com")
	if err := srv.db.CreateUser(member, "password123"); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(member.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	if err := srv.db.AddMemberRole(member.ID, testServer.ID, everyone.ID); err != nil {
		t.Fatalf("failed to assign @everyone to member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(owner.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add owner as member: %v", err)
	}
	if err := srv.db.AddMemberRole(owner.ID, testServer.ID, everyone.ID); err != nil {
		t.Fatalf("failed to assign @everyone to owner: %v", err)
	}

	return srv, testServer, member, cleanup
}

func TestUpdateServerMemberNicknameRoundTrip(t *testing.T) {
	srv, testServer, member, cleanup := setupNicknameTestServer(t)
	defer cleanup()

	if err := srv.db.UpdateServerMemberNickname(testServer.ID, member.ID, "Nicky"); err != nil {
		t.Fatalf("failed to set nickname: %v", err)
	}
	got, err := srv.db.GetServerMember(testServer.ID, member.ID)
	if err != nil {
		t.Fatalf("failed to reload member: %v", err)
	}
	if got.Nickname != "Nicky" {
		t.Errorf("expected nickname %q, got %q", "Nicky", got.Nickname)
	}

	// Clearing (empty string) works the same way /nick clear does.
	if err := srv.db.UpdateServerMemberNickname(testServer.ID, member.ID, ""); err != nil {
		t.Fatalf("failed to clear nickname: %v", err)
	}
	got, err = srv.db.GetServerMember(testServer.ID, member.ID)
	if err != nil {
		t.Fatalf("failed to reload member: %v", err)
	}
	if got.Nickname != "" {
		t.Errorf("expected nickname cleared, got %q", got.Nickname)
	}
}

// TestNicknameEndToEndOverWebSocket drives the real wire path: a member
// renames themselves (self-service, gated only on PermissionChangeNickname
// which @everyone grants by default), then tries to rename someone else and
// is correctly rejected without PermissionManageNicknames.
func TestNicknameEndToEndOverWebSocket(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level integration test in short mode")
	}

	srv, wsURL := startPlainTestServer(t)

	owner, ownerToken := createTestUserAndToken(t, srv, "nick-owner")
	member, memberToken := createTestUserAndToken(t, srv, "nick-member")

	testServer := models.NewServer("Nickname Wire Test", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	everyone := models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}
	for _, u := range []*models.User{owner, member} {
		if err := srv.db.AddServerMember(models.NewServerMember(u.ID, testServer.ID)); err != nil {
			t.Fatalf("failed to add member %s: %v", u.Username, err)
		}
		if err := srv.db.AddMemberRole(u.ID, testServer.ID, everyone.ID); err != nil {
			t.Fatalf("failed to assign @everyone to %s: %v", u.Username, err)
		}
	}

	memberClient := newTestWSClient(t, wsURL)
	memberClient.identify(memberToken)

	// Self-rename: allowed by @everyone's default PermissionChangeNickname,
	// no ManageNicknames needed.
	memberClient.send(protocol.OpSetNickname, protocol.SetNicknameRequest{
		ServerID: testServer.ID,
		Nickname: "Wire Nick",
	})
	updated := memberClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventNicknameUpdate
	})
	var nickPayload protocol.NicknamePayload
	if err := json.Unmarshal(updated.Data, &nickPayload); err != nil {
		t.Fatalf("failed to decode nickname payload: %v", err)
	}
	if nickPayload.UserID != member.ID || nickPayload.Nickname != "Wire Nick" {
		t.Errorf("expected self-rename to %s, got %+v", member.ID, nickPayload)
	}
	if got, err := srv.db.GetServerMember(testServer.ID, member.ID); err != nil || got.Nickname != "Wire Nick" {
		t.Errorf("expected DB nickname 'Wire Nick', got %+v (err=%v)", got, err)
	}

	// Renaming someone else must be rejected without PermissionManageNicknames.
	memberClient.send(protocol.OpSetNickname, protocol.SetNicknameRequest{
		ServerID: testServer.ID,
		UserID:   owner.ID,
		Nickname: "Not Allowed",
	})
	rejection := memberClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == ""
	})
	var errPayload protocol.ErrorPayload
	if err := json.Unmarshal(rejection.Data, &errPayload); err != nil {
		t.Fatalf("failed to decode error payload: %v", err)
	}
	if errPayload.Code != protocol.ErrorCodeForbidden {
		t.Errorf("expected ErrorCodeForbidden renaming another member without ManageNicknames, got %d: %s", errPayload.Code, errPayload.Message)
	}
	if got, err := srv.db.GetServerMember(testServer.ID, owner.ID); err != nil || got.Nickname == "Not Allowed" {
		t.Errorf("owner's nickname should not have changed, got %+v (err=%v)", got, err)
	}

	// Positive case: an admin WITH PermissionManageNicknames can rename
	// someone else.
	adminRole := models.NewRole(testServer.ID, "Nick Admin")
	adminRole.Permissions = models.PermissionManageNicknames
	if err := srv.db.CreateRole(adminRole); err != nil {
		t.Fatalf("failed to create admin role: %v", err)
	}
	if err := srv.db.AddMemberRole(owner.ID, testServer.ID, adminRole.ID); err != nil {
		t.Fatalf("failed to assign admin role: %v", err)
	}

	ownerClient := newTestWSClient(t, wsURL)
	ownerClient.identify(ownerToken)
	ownerClient.send(protocol.OpSetNickname, protocol.SetNicknameRequest{
		ServerID: testServer.ID,
		UserID:   member.ID,
		Nickname: "Renamed By Admin",
	})
	adminUpdate := ownerClient.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && m.Type == protocol.EventNicknameUpdate
	})
	var adminPayload protocol.NicknamePayload
	if err := json.Unmarshal(adminUpdate.Data, &adminPayload); err != nil {
		t.Fatalf("failed to decode admin nickname payload: %v", err)
	}
	if adminPayload.UserID != member.ID || adminPayload.Nickname != "Renamed By Admin" {
		t.Errorf("expected admin rename of %s, got %+v", member.ID, adminPayload)
	}
}
