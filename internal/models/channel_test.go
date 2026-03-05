package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewTextChannel(t *testing.T) {
	serverID := uuid.New()
	channelName := "general"

	channel := NewTextChannel(serverID, channelName)

	if channel == nil {
		t.Fatal("expected non-nil channel")
	}
	if channel.ServerID != serverID {
		t.Errorf("expected server_id %v, got %v", serverID, channel.ServerID)
	}
	if channel.Name != channelName {
		t.Errorf("expected name %q, got %q", channelName, channel.Name)
	}
	if channel.Type != ChannelTypeText {
		t.Errorf("expected type %v, got %v", ChannelTypeText, channel.Type)
	}
	if channel.ID == uuid.Nil {
		t.Error("channel ID should not be nil UUID")
	}
	if channel.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
}

func TestNewVoiceChannel(t *testing.T) {
	serverID := uuid.New()
	channelName := "General Voice"

	channel := NewVoiceChannel(serverID, channelName)

	if channel == nil {
		t.Fatal("expected non-nil channel")
	}
	if channel.Type != ChannelTypeVoice {
		t.Errorf("expected type %v, got %v", ChannelTypeVoice, channel.Type)
	}
	if channel.Name != channelName {
		t.Errorf("expected name %q, got %q", channelName, channel.Name)
	}
}

func TestNewCategory(t *testing.T) {
	serverID := uuid.New()
	categoryName := "Admin"

	category := NewCategory(serverID, categoryName)

	if category == nil {
		t.Fatal("expected non-nil category")
	}
	if category.Type != ChannelTypeCategory {
		t.Errorf("expected type %v, got %v", ChannelTypeCategory, category.Type)
	}
	if category.Name != categoryName {
		t.Errorf("expected name %q, got %q", categoryName, category.Name)
	}
}

func TestNewDMChannel(t *testing.T) {
	user1 := uuid.New()
	user2 := uuid.New()

	channel := NewDMChannel(user1, user2)

	if channel == nil {
		t.Fatal("expected non-nil channel")
	}
	if channel.Type != ChannelTypeDM {
		t.Errorf("expected type %v, got %v", ChannelTypeDM, channel.Type)
	}
	if channel.Name != "" {
		t.Errorf("DM channel should have empty name, got %q", channel.Name)
	}
	if len(channel.RecipientIDs) != 2 {
		t.Errorf("expected 2 recipients, got %d", len(channel.RecipientIDs))
	}
	if channel.RecipientIDs[0] != user1 || channel.RecipientIDs[1] != user2 {
		t.Error("recipient IDs don't match")
	}
}

func TestChannelIsTextBased(t *testing.T) {
	tests := []struct {
		name         string
		channelType  ChannelType
		expectText   bool
	}{
		{"text channel", ChannelTypeText, true},
		{"voice channel", ChannelTypeVoice, false},
		{"category", ChannelTypeCategory, false},
		{"DM channel", ChannelTypeDM, true},
		{"group DM", ChannelTypeGroupDM, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{
				ID:   uuid.New(),
				Name: "test",
				Type: tt.channelType,
			}

			result := channel.IsTextBased()
			if result != tt.expectText {
				t.Errorf("expected IsTextBased to be %v, got %v", tt.expectText, result)
			}
		})
	}
}

func TestChannelIsVoiceBased(t *testing.T) {
	tests := []struct {
		name         string
		channelType  ChannelType
		expectVoice  bool
	}{
		{"text channel", ChannelTypeText, false},
		{"voice channel", ChannelTypeVoice, true},
		{"category", ChannelTypeCategory, false},
		{"DM channel", ChannelTypeDM, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{
				ID:   uuid.New(),
				Name: "test",
				Type: tt.channelType,
			}

			result := channel.IsVoiceBased()
			if result != tt.expectVoice {
				t.Errorf("expected IsVoiceBased to be %v, got %v", tt.expectVoice, result)
			}
		})
	}
}

func TestChannelIsDM(t *testing.T) {
	tests := []struct {
		name        string
		channelType ChannelType
		expectDM    bool
	}{
		{"text channel", ChannelTypeText, false},
		{"voice channel", ChannelTypeVoice, false},
		{"DM channel", ChannelTypeDM, true},
		{"group DM", ChannelTypeGroupDM, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			channel := &Channel{
				ID:   uuid.New(),
				Name: "test",
				Type: tt.channelType,
			}

			result := channel.IsDM()
			if result != tt.expectDM {
				t.Errorf("expected IsDM to be %v, got %v", tt.expectDM, result)
			}
		})
	}
}

func TestChannelSetCategory(t *testing.T) {
	channel := NewTextChannel(uuid.New(), "test")
	categoryID := uuid.New()

	if channel.CategoryID != uuid.Nil {
		t.Error("channel should start with no category")
	}

	channel.SetCategory(categoryID)

	if channel.CategoryID != categoryID {
		t.Errorf("expected category_id %v, got %v", categoryID, channel.CategoryID)
	}
}

func TestChannelAddPermissionOverwrite(t *testing.T) {
	channel := NewTextChannel(uuid.New(), "test")
	roleID := uuid.New()

	overwrite := PermissionOverwrite{
		ID:    roleID,
		Type:  "role",
		Allow: int64(PermissionSendMessages),
		Deny:  int64(PermissionManageMessages),
	}

	channel.AddPermissionOverwrite(overwrite)

	if len(channel.PermissionOverwrites) != 1 {
		t.Errorf("expected 1 overwrite, got %d", len(channel.PermissionOverwrites))
	}
	if channel.PermissionOverwrites[0].ID != roleID {
		t.Error("overwrite ID doesn't match")
	}

	// Update existing overwrite
	updatedOverwrite := PermissionOverwrite{
		ID:    roleID,
		Type:  "role",
		Allow: int64(PermissionSendMessages | PermissionAddReactions),
		Deny:  0,
	}

	channel.AddPermissionOverwrite(updatedOverwrite)

	if len(channel.PermissionOverwrites) != 1 {
		t.Errorf("expected 1 overwrite after update, got %d", len(channel.PermissionOverwrites))
	}
	if channel.PermissionOverwrites[0].Allow != int64(PermissionSendMessages|PermissionAddReactions) {
		t.Error("overwrite should be updated")
	}
	if channel.PermissionOverwrites[0].Deny != 0 {
		t.Error("deny should be cleared")
	}
}

func TestChannelRemovePermissionOverwrite(t *testing.T) {
	channel := NewTextChannel(uuid.New(), "test")
	role1 := uuid.New()
	role2 := uuid.New()

	overwrite1 := PermissionOverwrite{
		ID:    role1,
		Type:  "role",
		Allow: int64(PermissionSendMessages),
		Deny:  0,
	}
	overwrite2 := PermissionOverwrite{
		ID:    role2,
		Type:  "role",
		Allow: int64(PermissionManageMessages),
		Deny:  0,
	}

	channel.AddPermissionOverwrite(overwrite1)
	channel.AddPermissionOverwrite(overwrite2)

	if len(channel.PermissionOverwrites) != 2 {
		t.Errorf("expected 2 overwrites, got %d", len(channel.PermissionOverwrites))
	}

	channel.RemovePermissionOverwrite(role1)

	if len(channel.PermissionOverwrites) != 1 {
		t.Errorf("expected 1 overwrite after removal, got %d", len(channel.PermissionOverwrites))
	}
	if channel.PermissionOverwrites[0].ID != role2 {
		t.Error("wrong overwrite was removed")
	}

	// Removing non-existent overwrite should be safe
	nonExistent := uuid.New()
	channel.RemovePermissionOverwrite(nonExistent)
	if len(channel.PermissionOverwrites) != 1 {
		t.Errorf("expected 1 overwrite after removing non-existent, got %d", len(channel.PermissionOverwrites))
	}
}
