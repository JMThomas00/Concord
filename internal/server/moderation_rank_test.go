package server

import (
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// The rank rule for kick, ban, mute, timeout and voice moderation is checked
// by the server, not only by the client's menus: a modified client with Kick
// Members could otherwise kick an administrator or the owner.
func TestCheckOutranks(t *testing.T) {
	srv, _ := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "rank-owner")
	s := models.NewServer("Ranks", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	role := func(name string, perms models.Permission, pos int) *models.Role {
		r := models.NewRole(s.ID, name)
		r.Permissions, r.Position = perms, pos
		if err := srv.db.CreateRole(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	admin := role("Admin", models.PermissionAdministrator, 10)
	senior := role("Senior Mod", models.PermissionsModerator, 6)
	mod := role("Mod", models.PermissionsModerator, 5)
	member := func(name string, roles ...*models.Role) uuid.UUID {
		u, _ := createTestUserAndToken(t, srv, name)
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
		for _, r := range roles {
			srv.db.AddMemberRole(u.ID, s.ID, r.ID)
		}
		return u.ID
	}
	srv.db.AddServerMember(models.NewServerMember(owner.ID, s.ID))
	admin1, admin2 := member("admin1", admin), member("admin2", admin)
	seniorMod := member("senior", senior)
	mod1, mod2 := member("mod1", mod), member("mod2", mod)
	plain := member("plain")

	cases := []struct {
		name          string
		actor, target uuid.UUID
		ok            bool
	}{
		{"owner over admin", owner.ID, admin1, true},
		{"admin over the owner", admin1, owner.ID, false},
		{"mod over the owner", mod1, owner.ID, false},
		{"admin over a mod", admin1, mod1, true},
		{"admin over an equal admin", admin1, admin2, false},
		{"mod over an admin", mod1, admin1, false},
		{"senior mod over a mod", seniorMod, mod1, true},
		{"mod over a senior mod", mod1, seniorMod, false},
		{"mod over an equal mod", mod1, mod2, false},
		{"mod over a plain member", mod1, plain, true},
		{"plain member over a mod", plain, mod1, false},
		{"anyone over themselves", mod1, mod1, false},
	}
	for _, c := range cases {
		err := srv.handlers.checkOutranks(c.actor, s.ID, c.target)
		if (err == nil) != c.ok {
			t.Errorf("%s: got %v, want ok=%v", c.name, err, c.ok)
		}
	}
}

// Over the wire: a moderator's mute of the owner is refused, and the owner
// stays unmuted.
func TestModeratorCannotMuteTheOwner(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "mute-owner")
	mod, token := createTestUserAndToken(t, srv, "mute-mod")
	s := models.NewServer("Mute Ranks", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	modRole := models.NewRole(s.ID, "Mod")
	modRole.Permissions, modRole.Position = models.PermissionsModerator, 5
	if err := srv.db.CreateRole(modRole); err != nil {
		t.Fatal(err)
	}
	srv.db.AddServerMember(models.NewServerMember(owner.ID, s.ID))
	srv.db.AddServerMember(models.NewServerMember(mod.ID, s.ID))
	srv.db.AddMemberRole(mod.ID, s.ID, modRole.ID)
	general := models.NewTextChannel(s.ID, "general")
	if err := srv.db.CreateChannel(general); err != nil {
		t.Fatal(err)
	}

	c := newTestWSClient(t, wsURL)
	c.identify(token)
	c.send(protocol.OpMuteMember, protocol.MuteMemberRequest{ServerID: s.ID, ChannelID: general.ID, UserID: owner.ID, Mute: true})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventServerMemberUpdate)
	})
	if got.Type != "" {
		t.Fatalf("the mute went through: %s", got.Type)
	}
	if m, err := srv.db.GetServerMember(s.ID, owner.ID); err != nil || m.IsMuted {
		t.Fatalf("owner muted after a refused mute: %+v, %v", m, err)
	}
}

// Pinning needs Pin Messages (or Manage Messages): a plain member is refused,
// a moderator isn't.
func TestPinNeedsPermission(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "pin-owner")
	plain, plainToken := createTestUserAndToken(t, srv, "pin-plain")
	mod, modToken := createTestUserAndToken(t, srv, "pin-mod")
	s := models.NewServer("Pins", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	everyone := models.NewEveryoneRole(s.ID)
	if err := srv.db.CreateRole(everyone); err != nil {
		t.Fatal(err)
	}
	modRole := models.NewRole(s.ID, "Mod")
	modRole.Permissions, modRole.Position = models.PermissionsModerator, 5
	if err := srv.db.CreateRole(modRole); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*models.User{owner, plain, mod} {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
		srv.db.AddMemberRole(u.ID, s.ID, everyone.ID)
	}
	srv.db.AddMemberRole(mod.ID, s.ID, modRole.ID)
	general := models.NewTextChannel(s.ID, "general")
	if err := srv.db.CreateChannel(general); err != nil {
		t.Fatal(err)
	}
	msg := models.NewMessage(general.ID, owner.ID, "pin me")
	if err := srv.db.CreateMessage(msg); err != nil {
		t.Fatal(err)
	}

	pin := func(token string) *protocol.Message {
		c := newTestWSClient(t, wsURL)
		c.identify(token)
		c.send(protocol.OpPinMessage, protocol.PinMessageRequest{ChannelID: general.ID, MessageID: msg.ID})
		return c.readUntil(5*time.Second, func(m *protocol.Message) bool {
			return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventMessagePin)
		})
	}
	if got := pin(plainToken); got.Type != "" {
		t.Fatalf("a plain member pinned: %s", got.Type)
	}
	if m, _ := srv.db.GetMessage(msg.ID); m == nil || m.IsPinned {
		t.Fatalf("message pinned after a refused pin: %+v", m)
	}
	if got := pin(modToken); got.Type != protocol.EventMessagePin {
		t.Fatalf("a moderator's pin was refused: %s", got.Data)
	}
}

// Removing the Administrator role is refused only for the last admin. The
// count once passed GetMemberRoles its arguments the wrong way round, found
// no admins at all, and refused every removal.
func TestRemoveAdminRoleWhenAnotherAdminRemains(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, ownerToken := createTestUserAndToken(t, srv, "admins-owner")
	a1, _ := createTestUserAndToken(t, srv, "admins-one")
	a2, _ := createTestUserAndToken(t, srv, "admins-two")
	s := models.NewServer("Admins", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	admin := models.NewRole(s.ID, "Admin")
	admin.Permissions, admin.Position = models.PermissionAdministrator, 10
	if err := srv.db.CreateRole(admin); err != nil {
		t.Fatal(err)
	}
	srv.db.AddServerMember(models.NewServerMember(owner.ID, s.ID))
	for _, u := range []*models.User{a1, a2} {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
		srv.db.AddMemberRole(u.ID, s.ID, admin.ID)
	}

	c := newTestWSClient(t, wsURL)
	c.identify(ownerToken)
	c.send(protocol.OpRoleRemove, protocol.RoleRemoveRequest{ServerID: s.ID, UserID: a1.ID, RoleName: "Admin"})
	got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventServerMemberUpdate)
	})
	if got.Type == "" {
		t.Fatalf("removing one of two admins was refused: %s", got.Data)
	}
}

// Deafening needs Deafen Members and moving needs Move Members (both used to
// check Mute Members); muting still needs only Mute Members.
func TestDeafenAndMoveNeedTheirOwnPermissions(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "dm-owner")
	mod, token := createTestUserAndToken(t, srv, "dm-mod")
	target, _ := createTestUserAndToken(t, srv, "dm-target")
	s := models.NewServer("Deafen Move", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	muter := models.NewRole(s.ID, "Muter")
	muter.Permissions, muter.Position = models.PermissionMuteMembers, 5
	if err := srv.db.CreateRole(muter); err != nil {
		t.Fatal(err)
	}
	for _, u := range []*models.User{owner, mod, target} {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
	}
	srv.db.AddMemberRole(mod.ID, s.ID, muter.ID)
	lounge := models.NewVoiceChannel(s.ID, "Lounge")
	if err := srv.db.CreateChannel(lounge); err != nil {
		t.Fatal(err)
	}

	c := newTestWSClient(t, wsURL)
	c.identify(token)
	reply := func(op protocol.OpCode, payload interface{}) *protocol.Message {
		t.Helper()
		c.send(op, payload)
		return c.readUntil(5*time.Second, func(m *protocol.Message) bool {
			return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == protocol.EventVoiceStateUpdate)
		})
	}
	if got := reply(protocol.OpVoiceServerMute, protocol.VoiceServerMutePayload{ServerID: s.ID, UserID: target.ID, Muted: false, Deafened: true}); got.Type != "" {
		t.Fatalf("deafened without Deafen Members: %s", got.Type)
	}
	if got := reply(protocol.OpMoveVoice, protocol.MoveVoicePayload{ServerID: s.ID, UserID: target.ID, ChannelID: lounge.ID}); got.Type != "" {
		t.Fatalf("moved without Move Members: %s", got.Type)
	}
	if got := reply(protocol.OpVoiceServerMute, protocol.VoiceServerMutePayload{ServerID: s.ID, UserID: target.ID, Muted: true}); got.Type != protocol.EventVoiceStateUpdate {
		t.Fatalf("a voice mute with Mute Members was refused: %s", got.Data)
	}
}

// Only administrators (and the owner) make or unmake administrators: Manage
// Roles alone can't give, take, create, change or delete a role carrying
// Administrator, though it still manages every other role.
func TestOnlyAdminsMakeAdmins(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level integration test")
	}
	srv, wsURL := startPlainTestServer(t)
	owner, _ := createTestUserAndToken(t, srv, "aa-owner")
	manager, managerToken := createTestUserAndToken(t, srv, "aa-manager")
	admin, adminToken := createTestUserAndToken(t, srv, "aa-admin")
	plain, _ := createTestUserAndToken(t, srv, "aa-plain")
	s := models.NewServer("Admins Only", owner.ID)
	if err := srv.db.CreateServer(s); err != nil {
		t.Fatal(err)
	}
	newRole := func(name string, p models.Permission, pos int) *models.Role {
		r := models.NewRole(s.ID, name)
		r.Permissions, r.Position = p, pos
		if err := srv.db.CreateRole(r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	adminRole := newRole("Admin", models.PermissionAdministrator, 30)
	managerRole := newRole("Role Manager", models.PermissionManageRoles, 20)
	helper := newRole("Helper", models.PermissionsText, 10)
	for _, u := range []*models.User{owner, manager, admin, plain} {
		srv.db.AddServerMember(models.NewServerMember(u.ID, s.ID))
	}
	srv.db.AddMemberRole(manager.ID, s.ID, managerRole.ID)
	srv.db.AddMemberRole(admin.ID, s.ID, adminRole.ID)
	// A second administrator role, so the last-admin guards don't decide
	// the strip and delete cases below on their own.
	coAdmin := newRole("Co-Admin", models.PermissionAdministrator, 25)
	srv.db.AddMemberRole(owner.ID, s.ID, coAdmin.ID)

	hasRole := func(userID uuid.UUID, r *models.Role) bool {
		roles, _ := srv.db.GetMemberRoles(s.ID, userID)
		for _, x := range roles {
			if x.ID == r.ID {
				return true
			}
		}
		return false
	}
	isAdminRole := func(id uuid.UUID) bool {
		r, err := srv.db.GetRoleByID(id)
		return err == nil && r.Permissions&models.PermissionAdministrator != 0
	}
	connect := func(token string) func(protocol.OpCode, interface{}, protocol.EventType) bool {
		c := newTestWSClient(t, wsURL)
		c.identify(token)
		// ask sends a request and reports whether it succeeded (want arrived
		// before an error).
		return func(op protocol.OpCode, payload interface{}, want protocol.EventType) bool {
			t.Helper()
			c.send(op, payload)
			got := c.readUntil(5*time.Second, func(m *protocol.Message) bool {
				return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == want)
			})
			return got.Type == want
		}
	}

	ask := connect(managerToken)
	if ask(protocol.OpRoleAssign, protocol.RoleAssignRequest{ServerID: s.ID, UserID: manager.ID, RoleName: "Admin"}, protocol.EventServerMemberUpdate) || hasRole(manager.ID, adminRole) {
		t.Error("a role manager made themselves an admin")
	}
	if ask(protocol.OpRoleRemove, protocol.RoleRemoveRequest{ServerID: s.ID, UserID: admin.ID, RoleName: "Admin", Force: true}, protocol.EventServerMemberUpdate) || !hasRole(admin.ID, adminRole) {
		t.Error("a role manager took an admin's role away")
	}
	if ask(protocol.OpCreateRole, protocol.CreateRoleRequest{ServerID: s.ID, Name: "Sneaky", Permissions: uint64(models.PermissionAdministrator)}, protocol.EventRoleCreate) {
		t.Error("a role manager created an administrator role")
	}
	if ask(protocol.OpUpdateRole, protocol.UpdateRoleRequest{ServerID: s.ID, RoleID: helper.ID, Name: "Helper", Permissions: uint64(models.PermissionAdministrator)}, protocol.EventRoleUpdate) || isAdminRole(helper.ID) {
		t.Error("a role manager turned a role into an administrator role")
	}
	if ask(protocol.OpUpdateRole, protocol.UpdateRoleRequest{ServerID: s.ID, RoleID: adminRole.ID, Name: "Admin", Permissions: uint64(models.PermissionsText)}, protocol.EventRoleUpdate) || !isAdminRole(adminRole.ID) {
		t.Error("a role manager stripped the administrator role")
	}
	if ask(protocol.OpDeleteRole, protocol.DeleteRoleRequest{ServerID: s.ID, RoleID: adminRole.ID}, protocol.EventRoleDelete) || !isAdminRole(adminRole.ID) {
		t.Error("a role manager deleted the administrator role")
	}
	// Everything else is still theirs to manage.
	if !ask(protocol.OpRoleAssign, protocol.RoleAssignRequest{ServerID: s.ID, UserID: plain.ID, RoleName: "Helper"}, protocol.EventServerMemberUpdate) {
		t.Error("a role manager couldn't give an ordinary role")
	}

	// An administrator can make another.
	if !connect(adminToken)(protocol.OpRoleAssign, protocol.RoleAssignRequest{ServerID: s.ID, UserID: plain.ID, RoleName: "Admin"}, protocol.EventServerMemberUpdate) || !hasRole(plain.ID, adminRole) {
		t.Error("an admin couldn't make another admin")
	}
}
