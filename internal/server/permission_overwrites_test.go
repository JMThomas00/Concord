package server

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// setupOverwriteTestServer creates a server with an owner, an @everyone
// role, one regular member, and one text channel — the minimum fixture
// hasChannelPermission needs (server owner lookup, member lookup, roles,
// the channel's own overwrites).
func setupOverwriteTestServer(t *testing.T) (srv *Server, owner *models.User, member *models.User, everyone *models.Role, channel *models.Channel, cleanup func()) {
	t.Helper()
	srv, cleanup = createTestServer(t)

	owner = models.NewUser("owner", "owner@overwrite-test.com")
	if err := srv.db.CreateUser(owner, "password123"); err != nil {
		t.Fatalf("failed to create owner: %v", err)
	}

	testServer := models.NewServer("Overwrite Test Server", owner.ID)
	if err := srv.db.CreateServer(testServer); err != nil {
		t.Fatalf("failed to create server: %v", err)
	}

	everyone = models.NewEveryoneRole(testServer.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatalf("failed to create @everyone role: %v", err)
	}

	member = models.NewUser("member", "member@overwrite-test.com")
	if err := srv.db.CreateUser(member, "password123"); err != nil {
		t.Fatalf("failed to create member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(member.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add member: %v", err)
	}
	if err := srv.db.AddServerMember(models.NewServerMember(owner.ID, testServer.ID)); err != nil {
		t.Fatalf("failed to add owner as member: %v", err)
	}

	channel = models.NewTextChannel(testServer.ID, "overwrite-test-channel")
	if err := srv.db.CreateChannel(channel); err != nil {
		t.Fatalf("failed to create channel: %v", err)
	}

	return srv, owner, member, everyone, channel, cleanup
}

func TestHasChannelPermission_DefaultEveryoneCanSendMessages(t *testing.T) {
	srv, _, member, _, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	if err := srv.handlers.hasChannelPermission(member.ID, channel, models.PermissionSendMessages); err != nil {
		t.Errorf("expected a plain member to have Send Messages via the default @everyone role, got error: %v", err)
	}
}

func TestHasChannelPermission_EveryoneOverwriteCanDenySendMessages(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 1 {
		t.Fatalf("expected GetChannelByID to populate PermissionOverwrites, got %d entries", len(reloaded.PermissionOverwrites))
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err == nil {
		t.Error("expected Send Messages to be denied once @everyone's channel overwrite denies it")
	}
}

func TestHasChannelPermission_MemberOverwriteAllowsDespiteEveryoneDeny(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set everyone deny overwrite: %v", err)
	}
	allow := models.PermissionOverwrite{ID: member.ID, Type: "member", Allow: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, allow); err != nil {
		t.Fatalf("failed to set member allow overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 2 {
		t.Fatalf("expected 2 overwrites, got %d", len(reloaded.PermissionOverwrites))
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected the member-specific allow overwrite to win over @everyone's deny (member overwrites apply last), got error: %v", err)
	}
}

func TestHasChannelPermission_ServerOwnerBypassesOverwrites(t *testing.T) {
	srv, owner, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}

	if err := srv.handlers.hasChannelPermission(owner.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected the server owner to bypass channel overwrites entirely, got error: %v", err)
	}
}

func TestChannelPermissionOverwriteDelete(t *testing.T) {
	srv, _, member, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	deny := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, deny); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	if err := srv.db.DeleteChannelPermissionOverwrite(channel.ID, everyone.ID); err != nil {
		t.Fatalf("failed to delete overwrite: %v", err)
	}

	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}
	if len(reloaded.PermissionOverwrites) != 0 {
		t.Fatalf("expected overwrite to be gone after delete, got %d entries", len(reloaded.PermissionOverwrites))
	}
	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("expected Send Messages to be allowed again once the denying overwrite was deleted, got error: %v", err)
	}
}

func TestChannelPermissionOverwriteUpsertReplacesPreviousValue(t *testing.T) {
	srv, _, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	first := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Allow: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, first); err != nil {
		t.Fatalf("failed to set first overwrite: %v", err)
	}
	second := models.PermissionOverwrite{ID: everyone.ID, Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, second); err != nil {
		t.Fatalf("failed to set second (replacing) overwrite: %v", err)
	}

	overwrites, err := srv.db.GetChannelPermissionOverwrites(channel.ID)
	if err != nil {
		t.Fatalf("failed to get overwrites: %v", err)
	}
	if len(overwrites) != 1 {
		t.Fatalf("expected upsert to replace, not duplicate — got %d rows", len(overwrites))
	}
	if overwrites[0].Allow != 0 || overwrites[0].Deny != int64(models.PermissionSendMessages) {
		t.Errorf("expected the second Set call's values to win, got Allow=%d Deny=%d", overwrites[0].Allow, overwrites[0].Deny)
	}
}

func TestHasChannelPermission_UnknownTargetIsIgnored(t *testing.T) {
	// A random UUID that doesn't match any real role/member shouldn't panic
	// or otherwise affect the outcome for an unrelated user.
	srv, _, member, _, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	stray := models.PermissionOverwrite{ID: uuid.New(), Type: "role", Deny: int64(models.PermissionSendMessages)}
	if err := srv.db.SetChannelPermissionOverwrite(channel.ID, stray); err != nil {
		t.Fatalf("failed to set overwrite: %v", err)
	}
	reloaded, err := srv.db.GetChannelByID(channel.ID)
	if err != nil {
		t.Fatalf("failed to reload channel: %v", err)
	}

	if err := srv.handlers.hasChannelPermission(member.ID, reloaded, models.PermissionSendMessages); err != nil {
		t.Errorf("an overwrite targeting an unrelated role ID should not affect this member, got error: %v", err)
	}
}
