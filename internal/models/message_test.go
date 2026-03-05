package models

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewMessage(t *testing.T) {
	channelID := uuid.New()
	authorID := uuid.New()
	content := "Hello, world!"

	msg := NewMessage(channelID, authorID, content)

	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.ChannelID != channelID {
		t.Errorf("expected channel_id %v, got %v", channelID, msg.ChannelID)
	}
	if msg.AuthorID != authorID {
		t.Errorf("expected author_id %v, got %v", authorID, msg.AuthorID)
	}
	if msg.Content != content {
		t.Errorf("expected content %q, got %q", content, msg.Content)
	}
	if msg.Type != MessageTypeDefault {
		t.Errorf("expected type %v, got %v", MessageTypeDefault, msg.Type)
	}
	if msg.ID == uuid.Nil {
		t.Error("message ID should not be nil UUID")
	}
	if msg.CreatedAt.IsZero() {
		t.Error("created_at should be set")
	}
}

func TestNewSystemMessage(t *testing.T) {
	channelID := uuid.New()
	content := "User joined the server"

	msg := NewSystemMessage(channelID, content, MessageTypeMemberJoin)

	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.Type != MessageTypeMemberJoin {
		t.Errorf("expected type %v, got %v", MessageTypeMemberJoin, msg.Type)
	}
	if msg.AuthorID != uuid.Nil {
		t.Error("system messages should have nil author")
	}
	if msg.Content != content {
		t.Errorf("expected content %q, got %q", content, msg.Content)
	}
}

func TestNewReply(t *testing.T) {
	channelID := uuid.New()
	authorID := uuid.New()
	replyToID := uuid.New()
	content := "This is a reply"

	msg := NewReply(channelID, authorID, replyToID, content)

	if msg == nil {
		t.Fatal("expected non-nil message")
	}
	if msg.ReplyToID == nil {
		t.Fatal("reply should have reply_to_id set")
	}
	if *msg.ReplyToID != replyToID {
		t.Errorf("expected reply_to_id %v, got %v", replyToID, *msg.ReplyToID)
	}
	if msg.Content != content {
		t.Errorf("expected content %q, got %q", content, msg.Content)
	}
	if !msg.IsReply() {
		t.Error("IsReply should return true")
	}
}

func TestMessageEdit(t *testing.T) {
	msg := NewMessage(uuid.New(), uuid.New(), "Original content")

	if msg.IsEdited() {
		t.Error("new message should not be edited")
	}

	newContent := "Edited content"
	msg.Edit(newContent)

	if !msg.IsEdited() {
		t.Error("message should be edited after Edit()")
	}
	if msg.Content != newContent {
		t.Errorf("expected content %q, got %q", newContent, msg.Content)
	}
	if msg.EditedAt == nil {
		t.Error("edited_at should be set after Edit()")
	}
}

func TestMessageIsSystemMessage(t *testing.T) {
	tests := []struct {
		name       string
		msgType    MessageType
		expectSys  bool
	}{
		{"default message", MessageTypeDefault, false},
		{"system message", MessageTypeSystem, true},
		{"member join", MessageTypeMemberJoin, true},
		{"member leave", MessageTypeMemberLeave, true},
		{"channel pinned", MessageTypeChannelPinned, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := &Message{
				ID:        uuid.New(),
				ChannelID: uuid.New(),
				AuthorID:  uuid.New(),
				Content:   "test",
				Type:      tt.msgType,
			}

			result := msg.IsSystemMessage()
			if result != tt.expectSys {
				t.Errorf("expected IsSystemMessage to be %v, got %v", tt.expectSys, result)
			}
		})
	}
}

func TestMessageParseMentions(t *testing.T) {
	msg := &Message{
		ID:        uuid.New(),
		ChannelID: uuid.New(),
		AuthorID:  uuid.New(),
	}

	// Test @everyone
	msg.Content = "Hello @everyone!"
	msg.ParseMentions()
	if !msg.MentionEveryone {
		t.Error("should detect @everyone")
	}

	// Test user mentions
	userID := uuid.New()
	msg.Content = "Hello <@" + userID.String() + "> how are you?"
	msg.ParseMentions()
	if len(msg.Mentions) != 1 {
		t.Errorf("expected 1 user mention, got %d", len(msg.Mentions))
	}
	if len(msg.Mentions) > 0 && msg.Mentions[0] != userID {
		t.Error("user mention ID doesn't match")
	}

	// Test role mentions
	roleID := uuid.New()
	msg.Content = "Hello <@&" + roleID.String() + ">"
	msg.ParseMentions()
	if len(msg.MentionRoles) != 1 {
		t.Errorf("expected 1 role mention, got %d", len(msg.MentionRoles))
	}
	if len(msg.MentionRoles) > 0 && msg.MentionRoles[0] != roleID {
		t.Error("role mention ID doesn't match")
	}

	// Test duplicate mentions
	msg.Content = "Hello <@" + userID.String() + "> and <@" + userID.String() + ">"
	msg.ParseMentions()
	if len(msg.Mentions) != 1 {
		t.Errorf("duplicate mentions should be deduplicated, got %d mentions", len(msg.Mentions))
	}
}

func TestMessageReactions(t *testing.T) {
	msg := NewMessage(uuid.New(), uuid.New(), "test")
	user1 := uuid.New()
	user2 := uuid.New()

	// Add reaction
	msg.AddReaction("👍", user1)
	if len(msg.Reactions) != 1 {
		t.Errorf("expected 1 reaction, got %d", len(msg.Reactions))
	}
	if msg.Reactions[0].Emoji != "👍" {
		t.Errorf("expected emoji 👍, got %s", msg.Reactions[0].Emoji)
	}
	if msg.Reactions[0].Count != 1 {
		t.Errorf("expected count 1, got %d", msg.Reactions[0].Count)
	}

	// Add same reaction from different user
	msg.AddReaction("👍", user2)
	if len(msg.Reactions) != 1 {
		t.Errorf("expected 1 reaction type, got %d", len(msg.Reactions))
	}
	if msg.Reactions[0].Count != 2 {
		t.Errorf("expected count 2, got %d", msg.Reactions[0].Count)
	}

	// Add different reaction
	msg.AddReaction("❤️", user1)
	if len(msg.Reactions) != 2 {
		t.Errorf("expected 2 reaction types, got %d", len(msg.Reactions))
	}

	// Remove reaction
	msg.RemoveReaction("👍", user1)
	if msg.Reactions[0].Count != 1 {
		t.Errorf("expected count 1 after removal, got %d", msg.Reactions[0].Count)
	}

	// Remove last user's reaction (should remove reaction type)
	msg.RemoveReaction("👍", user2)
	if len(msg.Reactions) != 1 {
		t.Errorf("expected 1 reaction type after removing all, got %d", len(msg.Reactions))
	}
	if msg.Reactions[0].Emoji != "❤️" {
		t.Errorf("expected remaining reaction to be ❤️, got %s", msg.Reactions[0].Emoji)
	}

	// Duplicate reaction from same user should be ignored
	currentCount := msg.Reactions[0].Count
	msg.AddReaction("❤️", user1)
	if msg.Reactions[0].Count != currentCount {
		t.Error("duplicate reaction from same user should be ignored")
	}
}

func TestMessagePinUnpin(t *testing.T) {
	msg := NewMessage(uuid.New(), uuid.New(), "test")

	if msg.IsPinned {
		t.Error("new message should not be pinned")
	}

	msg.Pin()
	if !msg.IsPinned {
		t.Error("message should be pinned after Pin()")
	}

	msg.Unpin()
	if msg.IsPinned {
		t.Error("message should not be pinned after Unpin()")
	}
}
