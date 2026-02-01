package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
)

// MessageType represents the type of message
type MessageType int

const (
	MessageTypeDefault        MessageType = iota // Regular user message
	MessageTypeSystem                            // System message (join, leave, etc.)
	MessageTypeChannelPinned                     // Message was pinned
	MessageTypeMemberJoin                        // Member joined server
	MessageTypeMemberLeave                       // Member left server
)

// Message represents a chat message
type Message struct {
	ID              uuid.UUID    `json:"id"`
	ChannelID       uuid.UUID    `json:"channel_id"`
	AuthorID        uuid.UUID    `json:"author_id"`
	Content         string       `json:"content"`
	Type            MessageType  `json:"type"`
	CreatedAt       time.Time    `json:"created_at"`
	EditedAt        *time.Time   `json:"edited_at,omitempty"`
	IsPinned        bool         `json:"is_pinned"`
	Mentions        []uuid.UUID  `json:"mentions,omitempty"`        // User IDs mentioned
	MentionRoles    []uuid.UUID  `json:"mention_roles,omitempty"`   // Role IDs mentioned
	MentionEveryone bool         `json:"mention_everyone"`
	Attachments     []Attachment `json:"attachments,omitempty"`
	Embeds          []Embed      `json:"embeds,omitempty"`
	Reactions       []Reaction   `json:"reactions,omitempty"`
	ReplyToID       *uuid.UUID   `json:"reply_to_id,omitempty"`     // Message being replied to
}

// Attachment represents a file attached to a message
type Attachment struct {
	ID          uuid.UUID `json:"id"`
	Filename    string    `json:"filename"`
	Size        int64     `json:"size"`
	URL         string    `json:"url"`
	ContentType string    `json:"content_type,omitempty"`
}

// Embed represents rich embedded content (like link previews)
type Embed struct {
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	URL         string `json:"url,omitempty"`
	Color       int    `json:"color,omitempty"`
	Timestamp   string `json:"timestamp,omitempty"`
	Footer      string `json:"footer,omitempty"`
}

// Reaction represents an emoji reaction on a message
type Reaction struct {
	Emoji   string      `json:"emoji"`
	Count   int         `json:"count"`
	UserIDs []uuid.UUID `json:"user_ids"`
}

// NewMessage creates a new text message
func NewMessage(channelID, authorID uuid.UUID, content string) *Message {
	msg := &Message{
		ID:        uuid.New(),
		ChannelID: channelID,
		AuthorID:  authorID,
		Content:   content,
		Type:      MessageTypeDefault,
		CreatedAt: time.Now(),
	}
	msg.ParseMentions()
	return msg
}

// NewSystemMessage creates a new system message
func NewSystemMessage(channelID uuid.UUID, content string, msgType MessageType) *Message {
	return &Message{
		ID:        uuid.New(),
		ChannelID: channelID,
		AuthorID:  uuid.Nil, // System messages have no author
		Content:   content,
		Type:      msgType,
		CreatedAt: time.Now(),
	}
}

// NewReply creates a new message that is a reply to another message
func NewReply(channelID, authorID, replyToID uuid.UUID, content string) *Message {
	msg := NewMessage(channelID, authorID, content)
	msg.ReplyToID = &replyToID
	return msg
}

// Edit updates the message content
func (m *Message) Edit(newContent string) {
	m.Content = newContent
	now := time.Now()
	m.EditedAt = &now
	m.ParseMentions()
}

// IsEdited returns true if the message has been edited
func (m *Message) IsEdited() bool {
	return m.EditedAt != nil
}

// IsSystemMessage returns true if this is a system-generated message
func (m *Message) IsSystemMessage() bool {
	return m.Type != MessageTypeDefault
}

// IsReply returns true if this message is a reply to another message
func (m *Message) IsReply() bool {
	return m.ReplyToID != nil
}

// ParseMentions extracts user mentions from the message content
// Mentions use the format <@user_id> or <@!user_id> (with nickname)
func (m *Message) ParseMentions() {
	m.Mentions = []uuid.UUID{}
	m.MentionRoles = []uuid.UUID{}
	m.MentionEveryone = false

	content := m.Content

	// Check for @everyone or @here
	if strings.Contains(content, "@everyone") || strings.Contains(content, "@here") {
		m.MentionEveryone = true
	}

	// Parse user mentions: <@user_id> or <@!user_id>
	// This is a simple implementation; a real parser would use regex
	for {
		start := strings.Index(content, "<@")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], ">")
		if end == -1 {
			break
		}
		end += start

		mention := content[start+2 : end]
		// Remove optional ! for nickname mentions
		mention = strings.TrimPrefix(mention, "!")

		// Try to parse as UUID
		if id, err := uuid.Parse(mention); err == nil {
			// Check if already in mentions
			found := false
			for _, mid := range m.Mentions {
				if mid == id {
					found = true
					break
				}
			}
			if !found {
				m.Mentions = append(m.Mentions, id)
			}
		}

		content = content[end+1:]
	}

	// Parse role mentions: <@&role_id>
	content = m.Content
	for {
		start := strings.Index(content, "<@&")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], ">")
		if end == -1 {
			break
		}
		end += start

		mention := content[start+3 : end]
		if id, err := uuid.Parse(mention); err == nil {
			found := false
			for _, rid := range m.MentionRoles {
				if rid == id {
					found = true
					break
				}
			}
			if !found {
				m.MentionRoles = append(m.MentionRoles, id)
			}
		}

		content = content[end+1:]
	}
}

// AddReaction adds a reaction to the message
func (m *Message) AddReaction(emoji string, userID uuid.UUID) {
	for i, r := range m.Reactions {
		if r.Emoji == emoji {
			// Check if user already reacted
			for _, uid := range r.UserIDs {
				if uid == userID {
					return
				}
			}
			m.Reactions[i].UserIDs = append(m.Reactions[i].UserIDs, userID)
			m.Reactions[i].Count++
			return
		}
	}
	// New reaction
	m.Reactions = append(m.Reactions, Reaction{
		Emoji:   emoji,
		Count:   1,
		UserIDs: []uuid.UUID{userID},
	})
}

// RemoveReaction removes a user's reaction from the message
func (m *Message) RemoveReaction(emoji string, userID uuid.UUID) {
	for i, r := range m.Reactions {
		if r.Emoji == emoji {
			for j, uid := range r.UserIDs {
				if uid == userID {
					m.Reactions[i].UserIDs = append(r.UserIDs[:j], r.UserIDs[j+1:]...)
					m.Reactions[i].Count--
					if m.Reactions[i].Count == 0 {
						m.Reactions = append(m.Reactions[:i], m.Reactions[i+1:]...)
					}
					return
				}
			}
		}
	}
}

// Pin marks the message as pinned
func (m *Message) Pin() {
	m.IsPinned = true
}

// Unpin removes the pinned status from the message
func (m *Message) Unpin() {
	m.IsPinned = false
}
