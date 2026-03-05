package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewUser(t *testing.T) {
	tests := []struct {
		name     string
		username string
		email    string
	}{
		{"simple user", "alice", "alice@test.com"},
		{"user with numbers", "user123", "user123@example.com"},
		{"single char", "a", "a@test.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := NewUser(tt.username, tt.email)

			if user == nil {
				t.Fatal("expected non-nil user")
			}
			if user.Username != tt.username {
				t.Errorf("expected username %q, got %q", tt.username, user.Username)
			}
			if user.Email != tt.email {
				t.Errorf("expected email %q, got %q", tt.email, user.Email)
			}
			if user.Status != StatusOffline {
				t.Errorf("expected status %v, got %v", StatusOffline, user.Status)
			}
			if user.ID == uuid.Nil {
				t.Error("user ID should not be nil UUID")
			}
			if len(user.Discriminator) != 4 {
				t.Errorf("discriminator should be 4 characters, got %d", len(user.Discriminator))
			}
			if user.CreatedAt.IsZero() {
				t.Error("created_at should be set")
			}
		})
	}
}

func TestUserFullUsername(t *testing.T) {
	tests := []struct {
		name          string
		username      string
		discriminator string
		expected      string
	}{
		{"single digit", "alice", "0001", "alice#0001"},
		{"four digits", "bob", "1234", "bob#1234"},
		{"max value", "charlie", "9999", "charlie#9999"},
		{"mid range", "dave", "0567", "dave#0567"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{
				Username:      tt.username,
				Discriminator: tt.discriminator,
			}

			result := user.FullUsername()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestUserGetDisplayName(t *testing.T) {
	tests := []struct {
		name        string
		username    string
		displayName string
		expected    string
	}{
		{"no display name", "alice", "", "alice"},
		{"with display name", "bob", "Robert", "Robert"},
		{"display name with spaces", "charlie", "Charlie Brown", "Charlie Brown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := &User{
				Username:    tt.username,
				DisplayName: tt.displayName,
			}

			result := user.GetDisplayName()
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestUserSetOnline(t *testing.T) {
	user := NewUser("testuser", "test@test.com")
	user.Status = StatusOffline

	user.SetOnline()

	if user.Status != StatusOnline {
		t.Errorf("expected status %v, got %v", StatusOnline, user.Status)
	}
	if user.LastSeenAt.IsZero() {
		t.Error("last_seen_at should be set")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("updated_at should be set")
	}
}

func TestUserSetOffline(t *testing.T) {
	user := NewUser("testuser", "test@test.com")
	user.Status = StatusOnline

	user.SetOffline()

	if user.Status != StatusOffline {
		t.Errorf("expected status %v, got %v", StatusOffline, user.Status)
	}
	if user.LastSeenAt.IsZero() {
		t.Error("last_seen_at should be set")
	}
	if user.UpdatedAt.IsZero() {
		t.Error("updated_at should be set")
	}
}

func TestUserStatusTransitions(t *testing.T) {
	tests := []struct {
		name        string
		fromStatus  UserStatus
		setterFunc  func(*User)
		finalStatus UserStatus
	}{
		{"online to offline", StatusOnline, (*User).SetOffline, StatusOffline},
		{"offline to online", StatusOffline, (*User).SetOnline, StatusOnline},
		{"idle to online", StatusIdle, (*User).SetOnline, StatusOnline},
		{"dnd to offline", StatusDND, (*User).SetOffline, StatusOffline},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			user := NewUser("user", "user@test.com")
			user.Status = tt.fromStatus

			tt.setterFunc(user)

			if user.Status != tt.finalStatus {
				t.Errorf("expected status %v, got %v", tt.finalStatus, user.Status)
			}
		})
	}
}

func TestServerMemberHasRole(t *testing.T) {
	serverID := uuid.New()
	userID := uuid.New()
	role1 := uuid.New()
	role2 := uuid.New()
	role3 := uuid.New()

	member := &ServerMember{
		ServerID: serverID,
		UserID:   userID,
		RoleIDs:  []uuid.UUID{role1, role2},
	}

	if !member.HasRole(role1) {
		t.Error("member should have role1")
	}
	if !member.HasRole(role2) {
		t.Error("member should have role2")
	}
	if member.HasRole(role3) {
		t.Error("member should not have role3")
	}
	if member.HasRole(uuid.Nil) {
		t.Error("member should not have nil role")
	}
}

func TestServerMemberAddRole(t *testing.T) {
	member := &ServerMember{
		ServerID: uuid.New(),
		UserID:   uuid.New(),
		RoleIDs:  []uuid.UUID{},
	}

	role1 := uuid.New()
	role2 := uuid.New()

	member.AddRole(role1)
	if len(member.RoleIDs) != 1 {
		t.Errorf("expected 1 role, got %d", len(member.RoleIDs))
	}
	if !member.HasRole(role1) {
		t.Error("member should have role1 after adding")
	}

	member.AddRole(role2)
	if len(member.RoleIDs) != 2 {
		t.Errorf("expected 2 roles, got %d", len(member.RoleIDs))
	}
	if !member.HasRole(role2) {
		t.Error("member should have role2 after adding")
	}

	// Adding duplicate should be idempotent
	member.AddRole(role1)
	if len(member.RoleIDs) != 2 {
		t.Errorf("expected 2 roles after duplicate add, got %d", len(member.RoleIDs))
	}
	if !member.HasRole(role1) {
		t.Error("member should still have role1")
	}
}

func TestServerMemberRemoveRole(t *testing.T) {
	role1 := uuid.New()
	role2 := uuid.New()
	role3 := uuid.New()

	member := &ServerMember{
		ServerID: uuid.New(),
		UserID:   uuid.New(),
		RoleIDs:  []uuid.UUID{role1, role2, role3},
	}

	member.RemoveRole(role2)
	if len(member.RoleIDs) != 2 {
		t.Errorf("expected 2 roles, got %d", len(member.RoleIDs))
	}
	if member.HasRole(role2) {
		t.Error("member should not have role2 after removal")
	}
	if !member.HasRole(role1) {
		t.Error("member should still have role1")
	}
	if !member.HasRole(role3) {
		t.Error("member should still have role3")
	}

	// Removing non-existent role should be safe
	nonExistent := uuid.New()
	member.RemoveRole(nonExistent)
	if len(member.RoleIDs) != 2 {
		t.Errorf("expected 2 roles after removing non-existent, got %d", len(member.RoleIDs))
	}
}

func TestServerMemberNickname(t *testing.T) {
	member := &ServerMember{
		ServerID: uuid.New(),
		UserID:   uuid.New(),
	}

	if member.Nickname != "" {
		t.Error("nickname should start as empty string")
	}

	member.Nickname = "NewNick"
	if member.Nickname != "NewNick" {
		t.Errorf("expected nickname %q, got %q", "NewNick", member.Nickname)
	}

	member.Nickname = "AnotherNick"
	if member.Nickname != "AnotherNick" {
		t.Errorf("expected nickname %q, got %q", "AnotherNick", member.Nickname)
	}
}

func TestServerMemberCustomTitle(t *testing.T) {
	member := &ServerMember{
		ServerID: uuid.New(),
		UserID:   uuid.New(),
	}

	if member.CustomTitle != "" {
		t.Error("custom title should start as empty string")
	}

	member.CustomTitle = "The Great"
	if member.CustomTitle != "The Great" {
		t.Errorf("expected custom title %q, got %q", "The Great", member.CustomTitle)
	}

	member.CustomTitle = "Champion"
	if member.CustomTitle != "Champion" {
		t.Errorf("expected custom title %q, got %q", "Champion", member.CustomTitle)
	}
}

func TestNewServerMember(t *testing.T) {
	userID := uuid.New()
	serverID := uuid.New()

	member := NewServerMember(userID, serverID)

	if member == nil {
		t.Fatal("expected non-nil member")
	}
	if member.UserID != userID {
		t.Errorf("expected user_id %v, got %v", userID, member.UserID)
	}
	if member.ServerID != serverID {
		t.Errorf("expected server_id %v, got %v", serverID, member.ServerID)
	}
	if len(member.RoleIDs) != 0 {
		t.Errorf("expected empty role IDs, got %d roles", len(member.RoleIDs))
	}
	if member.JoinedAt.IsZero() {
		t.Error("joined_at should be set")
	}
}
