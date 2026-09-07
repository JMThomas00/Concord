package server

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// TestHandleUpdateRole_EveryoneRolePermissionsCanBeChanged is a regression
// test for a real bug found in manual testing 2026-09-06: HandleUpdateRole
// blanket-rejected *any* update to the @everyone role ("Cannot edit
// @everyone role"), including pure permission changes -- even though the
// client's own Roles > Permissions Editor explicitly supports opening
// "everyone" for exactly that purpose (e.g. granting Attach Files
// server-wide). The whole request was rejected, not just disallowed fields,
// and the client's save path doesn't wait for/surface that rejection before
// closing the editor, so it looked like the change had silently reverted
// rather than never having been applied at all. Only renaming @everyone
// should be rejected.
func TestHandleUpdateRole_EveryoneRolePermissionsCanBeChanged(t *testing.T) {
	srv, owner, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	client := &Client{UserID: owner.ID, User: owner, send: make(chan *protocol.Message, 10)}

	newPerms := everyone.Permissions | models.PermissionAttachFiles | models.PermissionEmbedLinks
	req := &protocol.UpdateRoleRequest{
		ServerID:      everyone.ServerID,
		ChannelID:     channel.ID,
		RoleID:        everyone.ID,
		Name:          everyone.Name, // unchanged -- this is exactly what the real Permissions Editor sends
		Permissions:   uint64(newPerms),
		Color:         everyone.Color,
		DisplayOrder:  &everyone.DisplayOrder,
		IsHoisted:     everyone.IsHoisted,
		IsMentionable: everyone.IsMentionable,
	}
	msg, err := protocol.NewMessage(protocol.OpUpdateRole, req)
	if err != nil {
		t.Fatalf("failed to build update role message: %v", err)
	}

	srv.handlers.HandleUpdateRole(client, msg)

	select {
	case sent := <-client.send:
		t.Fatalf("expected the permission update to succeed with no error, but the server sent: %+v", sent)
	default:
	}

	reloaded, err := srv.db.GetRoleByID(everyone.ID)
	if err != nil {
		t.Fatalf("failed to reload @everyone role: %v", err)
	}
	if reloaded.Permissions != newPerms {
		t.Errorf("expected @everyone's permissions to actually persist as %v, got %v", newPerms, reloaded.Permissions)
	}
	if !reloaded.HasPermission(models.PermissionAttachFiles) {
		t.Error("expected @everyone to now have PermissionAttachFiles")
	}
}

// TestHandleUpdateRole_EveryoneRoleCannotBeRenamed confirms the fix didn't
// throw out the one restriction that's actually still correct: @everyone's
// name is a protocol-level identity, not just a label, and stays immutable.
func TestHandleUpdateRole_EveryoneRoleCannotBeRenamed(t *testing.T) {
	srv, owner, _, everyone, channel, cleanup := setupOverwriteTestServer(t)
	defer cleanup()

	client := &Client{UserID: owner.ID, User: owner, send: make(chan *protocol.Message, 10)}

	req := &protocol.UpdateRoleRequest{
		ServerID:      everyone.ServerID,
		ChannelID:     channel.ID,
		RoleID:        everyone.ID,
		Name:          "not-everyone",
		Permissions:   uint64(everyone.Permissions),
		Color:         everyone.Color,
		DisplayOrder:  &everyone.DisplayOrder,
		IsHoisted:     everyone.IsHoisted,
		IsMentionable: everyone.IsMentionable,
	}
	msg, err := protocol.NewMessage(protocol.OpUpdateRole, req)
	if err != nil {
		t.Fatalf("failed to build update role message: %v", err)
	}

	srv.handlers.HandleUpdateRole(client, msg)

	select {
	case <-client.send:
		// expected: the server should have rejected the rename
	default:
		t.Fatal("expected renaming @everyone to be rejected, but no error was sent")
	}

	reloaded, err := srv.db.GetRoleByID(everyone.ID)
	if err != nil {
		t.Fatalf("failed to reload @everyone role: %v", err)
	}
	if reloaded.Name != everyone.Name {
		t.Errorf("expected @everyone's name to remain %q, got %q", everyone.Name, reloaded.Name)
	}
}
