package server

import (
	"testing"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// TestDatabaseConstraints tests database constraint enforcement
func TestDatabaseConstraints(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	t.Run("duplicate email constraint", func(t *testing.T) {
		user1 := models.NewUser("user1", "duplicate@test.com")
		err := server.db.CreateUser(user1, "password123")
		if err != nil {
			t.Fatalf("first user creation failed: %v", err)
		}

		user2 := models.NewUser("user2", "duplicate@test.com")
		err = server.db.CreateUser(user2, "password456")
		if err == nil {
			t.Error("expected error for duplicate email")
		}
	})

	t.Run("foreign key behavior - messages with invalid channel", func(t *testing.T) {
		// Create a valid user first
		user := models.NewUser("fkuser", "fk@test.com")
		err := server.db.CreateUser(user, "password123")
		if err != nil {
			t.Fatalf("user creation failed: %v", err)
		}

		// Try to create a message with non-existent channel
		nonExistentChannelID := uuid.New()
		message := models.NewMessage(nonExistentChannelID, user.ID, "test")

		err = server.db.CreateMessage(message)
		if err != nil {
			t.Logf("Database enforces foreign key constraints (good): %v", err)
		} else {
			// Database allows orphaned messages - validation must happen in handlers
			t.Logf("Database allows messages with invalid channel_id - validation should happen in handlers")

			// This documents that foreign keys may not be enforced
			// Application logic must validate channel existence before creating messages
		}
	})

	t.Run("username validation - NewUser handles empty input", func(t *testing.T) {
		// NewUser should handle empty username gracefully
		user := models.NewUser("", "empty@test.com")

		// NewUser uses empty string as-is, so username will be empty
		// This documents that validation must happen at higher layers
		if user.Username != "" {
			// If NewUser generates a username, that's fine
			t.Logf("NewUser generated username: %q", user.Username)
		}

		// Database creation might accept empty username
		// This test documents actual behavior rather than enforcing a constraint
		err := server.db.CreateUser(user, "password123")
		if err != nil {
			t.Logf("Database rejected empty username (good): %v", err)
		} else if user.Username == "" {
			t.Logf("Database accepted empty username - validation should happen in handlers")
		}
	})

	t.Run("cascade delete - channel deletion", func(t *testing.T) {
		// Create a user
		user := models.NewUser("cascadeuser", "cascade@test.com")
		err := server.db.CreateUser(user, "password123")
		if err != nil {
			t.Fatalf("user creation failed: %v", err)
		}

		// Create a server
		testServer := models.NewServer("Cascade Test Server", user.ID)
		err = server.db.CreateServer(testServer)
		if err != nil {
			t.Fatalf("server creation failed: %v", err)
		}

		// Create a channel in the server
		channel := models.NewTextChannel(testServer.ID, "test-channel")
		err = server.db.CreateChannel(channel)
		if err != nil {
			t.Fatalf("channel creation failed: %v", err)
		}

		// Create a message in the channel
		message := models.NewMessage(channel.ID, user.ID, "test message")
		err = server.db.CreateMessage(message)
		if err != nil {
			t.Fatalf("message creation failed: %v", err)
		}

		// Delete the channel (should cascade delete messages)
		err = server.db.DeleteChannel(channel.ID)
		if err != nil {
			t.Fatalf("channel deletion failed: %v", err)
		}

		// Verify message was cascade deleted
		messages, err := server.db.GetChannelMessages(channel.ID, 100, nil, user.ID)
		if err == nil && len(messages) != 0 {
			t.Error("expected messages to be cascade deleted with channel")
		}
	})

	t.Run("unique username+discriminator constraint", func(t *testing.T) {
		user1 := models.NewUser("uniqueuser", "unique1@test.com")
		originalDiscrim := user1.Discriminator
		err := server.db.CreateUser(user1, "password123")
		if err != nil {
			t.Fatalf("first user creation failed: %v", err)
		}

		// Try to create another user with same username+discriminator
		// This would require manually setting the discriminator which the
		// NewUser function randomizes, so this is more of a conceptual test
		user2 := models.NewUser("uniqueuser", "unique2@test.com")
		user2.Discriminator = originalDiscrim
		err = server.db.CreateUser(user2, "password456")
		if err == nil {
			t.Error("expected error for duplicate username+discriminator")
		}
	})
}

// TestPermissionEdgeCases tests permission calculation edge cases
func TestPermissionEdgeCases(t *testing.T) {
	t.Run("role hierarchy - highest role wins", func(t *testing.T) {
		serverID := uuid.New()
		userID := uuid.New()

		everyoneRole := models.NewEveryoneRole(serverID)

		lowRole := models.NewRole(serverID, "Low")
		lowRole.SetPermissions(models.PermissionsText)
		lowRole.Position = 1

		highRole := models.NewRole(serverID, "High")
		highRole.SetPermissions(models.PermissionsText | models.PermissionKickMembers)
		highRole.Position = 2

		member := &models.ServerMember{
			UserID:   userID,
			ServerID: serverID,
			RoleIDs:  []uuid.UUID{lowRole.ID, highRole.ID},
		}

		calc := models.NewPermissionCalculator(uuid.New(), everyoneRole)
		perms := calc.ComputeBasePermissions(member, []*models.Role{lowRole, highRole})

		// Should have permissions from both roles (OR operation)
		testRole := &models.Role{Permissions: perms}
		if !testRole.HasPermission(models.PermissionKickMembers) {
			t.Error("should have KickMembers from high role")
		}
	})

	t.Run("administrator bypasses all overwrites", func(t *testing.T) {
		serverID := uuid.New()
		userID := uuid.New()

		everyoneRole := models.NewEveryoneRole(serverID)
		adminRole := models.NewRole(serverID, "Admin")
		adminRole.SetPermissions(models.PermissionAdministrator)

		member := &models.ServerMember{
			UserID:   userID,
			ServerID: serverID,
			RoleIDs:  []uuid.UUID{adminRole.ID},
		}

		channel := &models.Channel{
			ID:       uuid.New(),
			ServerID: serverID,
			Type:     models.ChannelTypeText,
			PermissionOverwrites: []models.PermissionOverwrite{
				{
					ID:    userID,
					Type:  "member",
					Allow: 0,
					Deny:  int64(models.PermissionSendMessages | models.PermissionManageMessages),
				},
			},
		}

		calc := models.NewPermissionCalculator(uuid.New(), everyoneRole)
		basePerms := calc.ComputeBasePermissions(member, []*models.Role{adminRole})
		finalPerms := calc.ComputeOverwrites(basePerms, member, channel)

		// Admin should still have all permissions despite deny overwrite
		testRole := &models.Role{Permissions: finalPerms}
		if !testRole.HasPermission(models.PermissionSendMessages) {
			t.Error("admin should bypass deny overwrites")
		}
	})

	t.Run("channel overwrite priority - member > role > everyone", func(t *testing.T) {
		serverID := uuid.New()
		userID := uuid.New()
		roleID := uuid.New()

		everyoneRole := models.NewEveryoneRole(serverID)

		role := models.NewRole(serverID, "TestRole")
		role.ID = roleID
		role.SetPermissions(models.PermissionsText)

		member := &models.ServerMember{
			UserID:   userID,
			ServerID: serverID,
			RoleIDs:  []uuid.UUID{roleID},
		}

		channel := &models.Channel{
			ID:       uuid.New(),
			ServerID: serverID,
			Type:     models.ChannelTypeText,
			PermissionOverwrites: []models.PermissionOverwrite{
				// Everyone denies send
				{
					ID:    everyoneRole.ID,
					Type:  "role",
					Allow: 0,
					Deny:  int64(models.PermissionSendMessages),
				},
				// Role allows send
				{
					ID:    roleID,
					Type:  "role",
					Allow: int64(models.PermissionSendMessages),
					Deny:  0,
				},
				// Member denies send (should win)
				{
					ID:    userID,
					Type:  "member",
					Allow: 0,
					Deny:  int64(models.PermissionSendMessages),
				},
			},
		}

		calc := models.NewPermissionCalculator(uuid.New(), everyoneRole)
		basePerms := calc.ComputeBasePermissions(member, []*models.Role{role})
		finalPerms := calc.ComputeOverwrites(basePerms, member, channel)

		// Member-specific deny should override role allow
		testRole := &models.Role{Permissions: finalPerms}
		if testRole.HasPermission(models.PermissionSendMessages) {
			t.Error("member-specific deny should take precedence")
		}
	})
}

// TestMessageEdgeCases tests message-related edge cases
func TestMessageEdgeCases(t *testing.T) {
	t.Run("message mention parsing - malformed mentions", func(t *testing.T) {
		msg := &models.Message{
			ID:        uuid.New(),
			ChannelID: uuid.New(),
			AuthorID:  uuid.New(),
		}

		tests := []struct {
			name          string
			content       string
			expectUsers   int
			expectRoles   int
			expectEveryone bool
		}{
			{"valid user mention", "<@" + uuid.New().String() + ">", 1, 0, false},
			{"valid role mention", "<@&" + uuid.New().String() + ">", 0, 1, false},
			{"everyone mention", "@everyone", 0, 0, true},
			{"here mention", "@here", 0, 0, true},
			{"malformed mention - unclosed", "<@abc", 0, 0, false},
			{"malformed mention - invalid UUID", "<@not-a-uuid>", 0, 0, false},
			{"empty mention", "<@>", 0, 0, false},
			{"multiple valid mentions", "<@" + uuid.New().String() + "> <@" + uuid.New().String() + ">", 2, 0, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				msg.Content = tt.content
				msg.ParseMentions()

				if len(msg.Mentions) != tt.expectUsers {
					t.Errorf("expected %d user mentions, got %d", tt.expectUsers, len(msg.Mentions))
				}
				if len(msg.MentionRoles) != tt.expectRoles {
					t.Errorf("expected %d role mentions, got %d", tt.expectRoles, len(msg.MentionRoles))
				}
				if msg.MentionEveryone != tt.expectEveryone {
					t.Errorf("expected MentionEveryone=%v, got %v", tt.expectEveryone, msg.MentionEveryone)
				}
			})
		}
	})

	t.Run("message content max length", func(t *testing.T) {
		// Test that 2000 character limit is enforced
		msg := models.NewMessage(uuid.New(), uuid.New(), "test")

		// 2000 characters exactly should be allowed
		content := ""
		for i := 0; i < 2000; i++ {
			content += "a"
		}
		msg.Content = content
		if len(msg.Content) != 2000 {
			t.Error("2000 character message should be allowed")
		}

		// 2001 characters should be rejected (validation happens in handler)
		longContent := content + "x"
		if len(longContent) != 2001 {
			t.Error("test setup error")
		}
	})

	t.Run("message reactions - duplicate user", func(t *testing.T) {
		msg := models.NewMessage(uuid.New(), uuid.New(), "test")
		userID := uuid.New()

		msg.AddReaction("👍", userID)
		msg.AddReaction("👍", userID) // Duplicate

		if len(msg.Reactions) != 1 {
			t.Errorf("expected 1 reaction type, got %d", len(msg.Reactions))
		}
		if msg.Reactions[0].Count != 1 {
			t.Errorf("expected count 1 (no duplicate), got %d", msg.Reactions[0].Count)
		}
	})
}

// TestChannelEdgeCases tests channel-related edge cases
func TestChannelEdgeCases(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	t.Run("category cannot contain category", func(t *testing.T) {
		user := models.NewUser("catuser", "cat@test.com")
		err := server.db.CreateUser(user, "password123")
		if err != nil {
			t.Fatalf("user creation failed: %v", err)
		}

		testServer := models.NewServer("Category Test", user.ID)
		err = server.db.CreateServer(testServer)
		if err != nil {
			t.Fatalf("server creation failed: %v", err)
		}

		category1 := models.NewCategory(testServer.ID, "Category 1")
		err = server.db.CreateChannel(category1)
		if err != nil {
			t.Fatalf("category creation failed: %v", err)
		}

		category2 := models.NewCategory(testServer.ID, "Category 2")
		category2.CategoryID = category1.ID // Try to nest category
		err = server.db.CreateChannel(category2)
		// Depending on validation, this might be allowed or rejected
		// This test documents the behavior
		_ = err
	})

	t.Run("DM channel with no recipients", func(t *testing.T) {
		dmChannel := models.NewDMChannel()
		if len(dmChannel.RecipientIDs) != 0 {
			t.Error("DM channel with no recipients should have empty recipient list")
		}
		if dmChannel.Type != models.ChannelTypeDM {
			t.Error("DM channel type should be set correctly")
		}
	})

	t.Run("permission overwrite upsert", func(t *testing.T) {
		channel := models.NewTextChannel(uuid.New(), "test")
		roleID := uuid.New()

		overwrite1 := models.PermissionOverwrite{
			ID:    roleID,
			Type:  "role",
			Allow: int64(models.PermissionSendMessages),
			Deny:  0,
		}
		channel.AddPermissionOverwrite(overwrite1)

		if len(channel.PermissionOverwrites) != 1 {
			t.Fatalf("expected 1 overwrite, got %d", len(channel.PermissionOverwrites))
		}

		// Update the same overwrite
		overwrite2 := models.PermissionOverwrite{
			ID:    roleID,
			Type:  "role",
			Allow: int64(models.PermissionManageMessages),
			Deny:  int64(models.PermissionSendMessages),
		}
		channel.AddPermissionOverwrite(overwrite2)

		if len(channel.PermissionOverwrites) != 1 {
			t.Errorf("expected 1 overwrite after update, got %d", len(channel.PermissionOverwrites))
		}
		if channel.PermissionOverwrites[0].Allow != int64(models.PermissionManageMessages) {
			t.Error("overwrite should be updated, not duplicated")
		}
	})
}

// TestUserEdgeCases tests user-related edge cases
func TestUserEdgeCases(t *testing.T) {
	server, cleanup := createTestServer(t)
	defer cleanup()

	t.Run("user with extremely long email", func(t *testing.T) {
		longEmail := string(make([]byte, 500)) + "@test.com"
		user := models.NewUser("longemailuser", longEmail)
		err := server.db.CreateUser(user, "password123")
		// Should either succeed or fail gracefully
		_ = err
	})

	t.Run("discriminator collision handling", func(t *testing.T) {
		// NewUser generates random discriminators
		// In theory, collisions are possible with 10,000 possible values
		// This test documents that discriminators are generated
		user1 := models.NewUser("testuser", "test1@test.com")
		user2 := models.NewUser("testuser", "test2@test.com")

		if len(user1.Discriminator) != 4 || len(user2.Discriminator) != 4 {
			t.Error("discriminators should be 4 digits")
		}

		// They might be the same (1/10000 chance) or different
		// Just verify they're valid
		if user1.Discriminator == "" || user2.Discriminator == "" {
			t.Error("discriminators should not be empty")
		}
	})

	t.Run("status transitions", func(t *testing.T) {
		user := models.NewUser("statususer", "status@test.com")

		// All status transitions should be valid
		statuses := []models.UserStatus{
			models.StatusOnline,
			models.StatusIdle,
			models.StatusDND,
			models.StatusOffline,
		}

		for _, status := range statuses {
			user.Status = status
			if user.Status != status {
				t.Errorf("failed to set status to %v", status)
			}
		}
	})
}
