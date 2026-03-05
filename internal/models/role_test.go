package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewRole(t *testing.T) {
	serverID := uuid.New()
	roleName := "Moderator"

	role := NewRole(serverID, roleName)

	if role == nil {
		t.Fatal("expected non-nil role")
	}
	if role.ServerID != serverID {
		t.Errorf("expected server_id %v, got %v", serverID, role.ServerID)
	}
	if role.Name != roleName {
		t.Errorf("expected name %q, got %q", roleName, role.Name)
	}
	if role.ID == uuid.Nil {
		t.Error("role ID should not be nil UUID")
	}
	if role.Permissions != PermissionsText {
		t.Errorf("expected default permissions %v, got %v", PermissionsText, role.Permissions)
	}
	if role.Color != 0 {
		t.Errorf("expected default color 0, got %d", role.Color)
	}
	if role.Position != 0 {
		t.Errorf("expected default position 0, got %d", role.Position)
	}
	if role.IsHoisted {
		t.Error("expected IsHoisted to be false")
	}
	if !role.IsMentionable {
		t.Error("expected IsMentionable to be true")
	}
	if role.IsDefault {
		t.Error("expected IsDefault to be false")
	}
	if role.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
	if role.UpdatedAt.IsZero() {
		t.Error("updated_at should be set")
	}
}

func TestNewEveryoneRole(t *testing.T) {
	serverID := uuid.New()

	role := NewEveryoneRole(serverID)

	if role == nil {
		t.Fatal("expected non-nil role")
	}
	if role.Name != "everyone" {
		t.Errorf("expected name %q, got %q", "everyone", role.Name)
	}
	if !role.IsDefault {
		t.Error("expected IsDefault to be true")
	}
	if role.IsMentionable {
		t.Error("expected IsMentionable to be false for @everyone")
	}
	expectedPerms := PermissionViewChannels | PermissionSendMessages | PermissionReadMessageHistory | PermissionAddReactions | PermissionManageChannels
	if role.Permissions != expectedPerms {
		t.Errorf("expected everyone permissions %v, got %v", expectedPerms, role.Permissions)
	}
}

func TestRoleHasPermission(t *testing.T) {
	tests := []struct {
		name       string
		rolePerms  Permission
		checkPerm  Permission
		shouldHave bool
	}{
		{"has single permission", PermissionSendMessages, PermissionSendMessages, true},
		{"has multiple permissions", PermissionsText, PermissionSendMessages, true},
		{"does not have permission", PermissionViewChannels, PermissionSendMessages, false},
		{"admin has all permissions", PermissionAdministrator, PermissionBanMembers, true},
		{"admin has view channels", PermissionAdministrator, PermissionViewChannels, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role := &Role{
				ID:          uuid.New(),
				ServerID:    uuid.New(),
				Name:        "TestRole",
				Permissions: tt.rolePerms,
			}

			result := role.HasPermission(tt.checkPerm)
			if result != tt.shouldHave {
				t.Errorf("expected HasPermission to be %v, got %v", tt.shouldHave, result)
			}
		})
	}
}

func TestRoleAddPermission(t *testing.T) {
	role := NewRole(uuid.New(), "TestRole")

	// Start with text permissions
	if !role.HasPermission(PermissionSendMessages) {
		t.Error("role should start with SendMessages permission")
	}
	if role.HasPermission(PermissionKickMembers) {
		t.Error("role should not start with KickMembers permission")
	}

	role.AddPermission(PermissionKickMembers)

	if !role.HasPermission(PermissionKickMembers) {
		t.Error("role should have KickMembers permission after adding")
	}
	if !role.HasPermission(PermissionSendMessages) {
		t.Error("role should still have SendMessages permission")
	}

	// Adding duplicate permission should be idempotent
	oldPerms := role.Permissions
	role.AddPermission(PermissionKickMembers)
	if role.Permissions != oldPerms {
		t.Error("permissions should not change when adding duplicate permission")
	}
}

func TestRoleRemovePermission(t *testing.T) {
	role := NewRole(uuid.New(), "TestRole")
	role.AddPermission(PermissionKickMembers)

	if !role.HasPermission(PermissionKickMembers) {
		t.Error("role should have KickMembers permission before removal")
	}

	role.RemovePermission(PermissionKickMembers)

	if role.HasPermission(PermissionKickMembers) {
		t.Error("role should not have KickMembers permission after removal")
	}
	if !role.HasPermission(PermissionSendMessages) {
		t.Error("role should still have other permissions")
	}
}

func TestRoleSetPermissions(t *testing.T) {
	role := NewRole(uuid.New(), "TestRole")

	newPerms := PermissionsModerator
	role.SetPermissions(newPerms)

	if role.Permissions != newPerms {
		t.Errorf("expected permissions %v, got %v", newPerms, role.Permissions)
	}
	if !role.HasPermission(PermissionKickMembers) {
		t.Error("role should have moderator permissions")
	}
}

func TestRoleSetColor(t *testing.T) {
	role := NewRole(uuid.New(), "TestRole")

	color := 0xFF5733 // Orange
	role.SetColor(color)

	if role.Color != color {
		t.Errorf("expected color %d, got %d", color, role.Color)
	}
}

func TestRoleGetColorHex(t *testing.T) {
	tests := []struct {
		name     string
		color    int
		expected string
	}{
		{"no color", 0, ""},
		{"red", 0xFF0000, "#FF0000"},
		{"green", 0x00FF00, "#00FF00"},
		{"blue", 0x0000FF, "#0000FF"},
		{"orange", 0xFF5733, "#FF5733"},
		{"white", 0xFFFFFF, "#FFFFFF"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			role := &Role{
				ID:       uuid.New(),
				ServerID: uuid.New(),
				Name:     "TestRole",
				Color:    tt.color,
			}

			result := role.GetColorHex()
			if result != tt.expected {
				t.Errorf("expected hex %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestPermissionCalculatorServerOwner(t *testing.T) {
	ownerID := uuid.New()
	serverID := uuid.New()
	everyoneRole := NewEveryoneRole(serverID)

	calc := NewPermissionCalculator(ownerID, everyoneRole)

	member := &ServerMember{
		UserID:   ownerID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{},
	}

	perms := calc.ComputeBasePermissions(member, []*Role{})

	// Server owner should have all permissions
	if !Permission(perms).has(PermissionAdministrator) {
		t.Error("server owner should have administrator permission")
	}
	if !Permission(perms).has(PermissionBanMembers) {
		t.Error("server owner should have ban members permission")
	}
	if !Permission(perms).has(PermissionManageServer) {
		t.Error("server owner should have manage server permission")
	}
}

func TestPermissionCalculatorRoleCombination(t *testing.T) {
	ownerID := uuid.New()
	userID := uuid.New()
	serverID := uuid.New()
	everyoneRole := NewEveryoneRole(serverID)

	role1 := NewRole(serverID, "Role1")
	role1.SetPermissions(PermissionSendMessages | PermissionReadMessageHistory)

	role2 := NewRole(serverID, "Role2")
	role2.SetPermissions(PermissionKickMembers | PermissionBanMembers)

	calc := NewPermissionCalculator(ownerID, everyoneRole)

	member := &ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{role1.ID, role2.ID},
	}

	perms := calc.ComputeBasePermissions(member, []*Role{role1, role2})

	// Should have combination of all role permissions plus @everyone
	if !Permission(perms).has(PermissionSendMessages) {
		t.Error("member should have SendMessages from role1")
	}
	if !Permission(perms).has(PermissionKickMembers) {
		t.Error("member should have KickMembers from role2")
	}
	if !Permission(perms).has(PermissionViewChannels) {
		t.Error("member should have ViewChannels from @everyone")
	}
}

func TestPermissionCalculatorAdminRole(t *testing.T) {
	ownerID := uuid.New()
	userID := uuid.New()
	serverID := uuid.New()
	everyoneRole := NewEveryoneRole(serverID)

	adminRole := NewRole(serverID, "Admin")
	adminRole.SetPermissions(PermissionAdministrator)

	calc := NewPermissionCalculator(ownerID, everyoneRole)

	member := &ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{adminRole.ID},
	}

	perms := calc.ComputeBasePermissions(member, []*Role{adminRole})

	// Admin role should grant all permissions
	if !Permission(perms).has(PermissionBanMembers) {
		t.Error("admin role should grant ban members permission")
	}
	if !Permission(perms).has(PermissionManageServer) {
		t.Error("admin role should grant manage server permission")
	}
	if !Permission(perms).has(PermissionSendMessages) {
		t.Error("admin role should grant send messages permission")
	}
}

func TestPermissionCalculatorComputeOverwrites(t *testing.T) {
	ownerID := uuid.New()
	userID := uuid.New()
	serverID := uuid.New()
	everyoneRole := NewEveryoneRole(serverID)

	basePerms := PermissionsText

	channel := &Channel{
		ID:       uuid.New(),
		ServerID: serverID,
		Name:     "test-channel",
		Type:     ChannelTypeText,
		PermissionOverwrites: []PermissionOverwrite{
			{
				ID:    userID,
				Type:  "member",
				Allow: int64(PermissionManageMessages),
				Deny:  int64(PermissionSendMessages),
			},
		},
	}

	calc := NewPermissionCalculator(ownerID, everyoneRole)
	member := &ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{},
	}

	perms := calc.ComputeOverwrites(basePerms, member, channel)

	// Should have ManageMessages (allowed) but not SendMessages (denied)
	if !Permission(perms).has(PermissionManageMessages) {
		t.Error("member should have ManageMessages from overwrite allow")
	}
	if Permission(perms).has(PermissionSendMessages) {
		t.Error("member should not have SendMessages due to overwrite deny")
	}
}

func TestPermissionCalculatorAdminBypassesOverwrites(t *testing.T) {
	ownerID := uuid.New()
	userID := uuid.New()
	serverID := uuid.New()
	everyoneRole := NewEveryoneRole(serverID)

	basePerms := PermissionAdministrator

	channel := &Channel{
		ID:       uuid.New(),
		ServerID: serverID,
		Name:     "test-channel",
		Type:     ChannelTypeText,
		PermissionOverwrites: []PermissionOverwrite{
			{
				ID:    userID,
				Type:  "member",
				Allow: 0,
				Deny:  int64(PermissionSendMessages),
			},
		},
	}

	calc := NewPermissionCalculator(ownerID, everyoneRole)
	member := &ServerMember{
		UserID:   userID,
		ServerID: serverID,
		RoleIDs:  []uuid.UUID{},
	}

	perms := calc.ComputeOverwrites(basePerms, member, channel)

	// Admin should bypass overwrites
	if perms != basePerms {
		t.Error("admin permissions should bypass overwrites")
	}
}

// Helper method for testing permission bits
func (p Permission) has(perm Permission) bool {
	if p&PermissionAdministrator != 0 {
		return true
	}
	return p&perm != 0
}
