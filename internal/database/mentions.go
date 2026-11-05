package database

// The mentions inbox: every message that called for a member, newest
// first. Read or unread comes from the read positions (reads.go; a thread
// reply from the thread's), so the inbox agrees with the @ badges.

import (
	"fmt"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Kinds of mention, in the order one wins when a message is several.
const (
	MentionWhisper  = "whisper"  // a whisper to them
	MentionDirect   = "mention"  // @name or <@id>
	MentionReply    = "reply"    // a reply to their message
	MentionEveryone = "everyone" // @everyone
)

// Mention is one inbox entry.
type Mention struct {
	Message *models.Message // with ThreadID set for a reply in a thread
	Kind    string
	Unread  bool
}

// MentionsFor is a page of the user's mentions in a server, newest first:
// those before Seq before (0: from the newest), at most limit. Own
// messages, deleted ones, other people's whispers and channels that aren't
// text (categories, voice) are left out; the caller checks who may view
// each channel.
func (db *DB) MentionsFor(userID, serverID uuid.UUID, before int64, limit int) ([]Mention, error) {
	u := userID.String()
	if before <= 0 {
		before = 1 << 62
	}
	rows, err := db.Query(`
		SELECT m.id, m.channel_id, m.author_id, m.content, m.type, m.created_at, m.edited_at,
		       m.is_whisper, m.recipient_id, m.reply_to_id, m.thread_id, COALESCE(m.seq, 0),
		       COALESCE(m.mention_everyone, 0),
		       EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = ?),
		       EXISTS (SELECT 1 FROM messages p WHERE p.id = m.reply_to_id AND p.author_id = ?),
		       CASE WHEN m.thread_id IS NULL
		            THEN COALESCE(m.seq, 0) > COALESCE((SELECT r.read_seq FROM channel_reads r WHERE r.user_id = ? AND r.channel_id = m.channel_id), 0)
		            ELSE COALESCE(m.seq, 0) > COALESCE((SELECT f.read_seq FROM thread_follows f WHERE f.user_id = ? AND f.thread_id = m.thread_id), 0)
		       END
		FROM messages m JOIN channels c ON c.id = m.channel_id
		WHERE c.server_id = ? AND c.type NOT IN (2, 3, 4)
		  AND m.seq < ? AND m.author_id != ? AND COALESCE(m.is_deleted, 0) = 0
		  AND (COALESCE(m.is_whisper, 0) = 0 OR m.recipient_id = ?)
		  AND (COALESCE(m.mention_everyone, 0) = 1 OR m.recipient_id = ?
		       OR EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = ?)
		       OR EXISTS (SELECT 1 FROM messages p WHERE p.id = m.reply_to_id AND p.author_id = ?))
		ORDER BY m.seq DESC
		LIMIT ?`,
		u, u, u, u, serverID.String(), before, u, u, u, u, u, limit)
	if err != nil {
		return nil, fmt.Errorf("reading mentions: %w", err)
	}
	var out []Mention
	for rows.Next() {
		m := &models.Message{}
		var id, ch, author string
		var recipient, replyTo, thread *string
		var everyone, named, reply, unread bool
		if err := rows.Scan(&id, &ch, &author, &m.Content, &m.Type, &m.CreatedAt, &m.EditedAt,
			&m.IsWhisper, &recipient, &replyTo, &thread, &m.Seq, &everyone, &named, &reply, &unread); err != nil {
			rows.Close()
			return nil, err
		}
		m.ID, _ = uuid.Parse(id)
		m.ChannelID, _ = uuid.Parse(ch)
		m.AuthorID, _ = uuid.Parse(author)
		m.RecipientID, m.ReplyToID, m.ThreadID = parseIDPtr(recipient), parseIDPtr(replyTo), parseIDPtr(thread)
		m.MentionEveryone = everyone
		kind := MentionEveryone
		switch {
		case m.IsWhisper:
			kind = MentionWhisper
		case named:
			kind = MentionDirect
		case reply:
			kind = MentionReply
		}
		out = append(out, Mention{Message: m, Kind: kind, Unread: unread})
	}
	rows.Close()
	return out, rows.Err()
}

func parseIDPtr(s *string) *uuid.UUID {
	if s == nil {
		return nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil
	}
	return &id
}
