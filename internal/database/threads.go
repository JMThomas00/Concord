package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// Threads (2026-10-09): a reply in a thread has messages.thread_id set to the
// thread's first message, which itself has none. A thread's summary is
// worked out from its replies when asked for, so there's nothing to keep in
// step; thread_follows records who follows a thread and when they last read
// it.

const liveMessage = "(is_deleted = 0 OR is_deleted IS NULL)"

// ThreadStat is what a channel shows of a thread without loading it.
type ThreadStat struct {
	ReplyCount   int
	LastReplyID  uuid.UUID
	LastReplyAt  time.Time
	Participants []uuid.UUID // in the order they first posted, at most 5
}

// parseNullUUID reads an optional UUID column.
func parseNullUUID(s sql.NullString) *uuid.UUID {
	if !s.Valid || s.String == "" {
		return nil
	}
	id, err := uuid.Parse(s.String)
	if err != nil {
		return nil
	}
	return &id
}

func inList(ids []uuid.UUID) (string, []any) {
	marks := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		marks[i] = "?"
		args[i] = id.String()
	}
	return strings.Join(marks, ","), args
}

// GetThreadStats summarises the threads started by the given messages; a
// message that starts none isn't in the result. Deleted replies don't
// count, and neither do whispers the user can't see.
func (db *DB) GetThreadStats(threadIDs []uuid.UUID, userID uuid.UUID) (map[uuid.UUID]*ThreadStat, error) {
	out := make(map[uuid.UUID]*ThreadStat)
	if len(threadIDs) == 0 {
		return out, nil
	}
	in, args := inList(threadIDs)
	visible := liveMessage + " AND (is_whisper = 0 OR author_id = ? OR recipient_id = ?)"
	args = append(args, userID.String(), userID.String())

	// Each author's first reply, oldest first: gives the count, the
	// participants in order, and (with the latest reply below) the rest.
	rows, err := db.Query(fmt.Sprintf(`
		SELECT thread_id, author_id, COUNT(*)
		FROM messages WHERE thread_id IN (%s) AND %s
		GROUP BY thread_id, author_id
		ORDER BY MIN(created_at)`, in, visible), args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var threadStr, authorStr string
		var n int
		if err := rows.Scan(&threadStr, &authorStr, &n); err != nil {
			rows.Close()
			return nil, err
		}
		tid, err1 := uuid.Parse(threadStr)
		aid, err2 := uuid.Parse(authorStr)
		if err1 != nil || err2 != nil {
			continue
		}
		st := out[tid]
		if st == nil {
			st = &ThreadStat{}
			out[tid] = st
		}
		st.ReplyCount += n
		if len(st.Participants) < 5 {
			st.Participants = append(st.Participants, aid)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The latest reply in each thread (a second query: the pool has one
	// connection, so queries never nest).
	rows, err = db.Query(fmt.Sprintf(`
		SELECT thread_id, id, created_at FROM messages m
		WHERE thread_id IN (%s) AND %s
		ORDER BY created_at DESC`, in, visible), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var threadStr, idStr string
		var at time.Time
		if err := rows.Scan(&threadStr, &idStr, &at); err != nil {
			return nil, err
		}
		tid, _ := uuid.Parse(threadStr)
		if st := out[tid]; st != nil && st.LastReplyID == uuid.Nil {
			st.LastReplyID, _ = uuid.Parse(idStr)
			st.LastReplyAt = at
		}
	}
	return out, rows.Err()
}

// GetThreadMessages returns a thread's replies the user can see, oldest
// first: the newest limit of them.
func (db *DB) GetThreadMessages(threadID uuid.UUID, limit int, userID uuid.UUID) ([]*models.Message, error) {
	rows, err := db.Query(`
		SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, is_whisper, recipient_id, reply_to_id
		FROM messages
		WHERE thread_id = ? AND `+liveMessage+` AND (is_whisper = 0 OR author_id = ? OR recipient_id = ?)
		ORDER BY created_at DESC
		LIMIT ?`, threadID.String(), userID.String(), userID.String(), limit)
	if err != nil {
		return nil, err
	}
	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var idStr, channelIDStr, authorIDStr string
		var editedAt sql.NullTime
		var recipientID, replyToID sql.NullString
		if err := rows.Scan(&idStr, &channelIDStr, &authorIDStr, &msg.Content, &msg.Type, &msg.CreatedAt,
			&editedAt, &msg.IsPinned, &msg.IsWhisper, &recipientID, &replyToID); err != nil {
			rows.Close()
			return nil, err
		}
		msg.ID, _ = uuid.Parse(idStr)
		msg.ChannelID, _ = uuid.Parse(channelIDStr)
		msg.AuthorID, _ = uuid.Parse(authorIDStr)
		if editedAt.Valid {
			msg.EditedAt = &editedAt.Time
		}
		msg.RecipientID = parseNullUUID(recipientID)
		msg.ReplyToID = parseNullUUID(replyToID)
		tid := threadID
		msg.ThreadID = &tid
		messages = append(messages, msg)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(messages) > 0 {
		ids := make([]uuid.UUID, len(messages))
		for i, m := range messages {
			ids[i] = m.ID
		}
		atts, err := db.getMessageAttachments(ids)
		if err != nil {
			return nil, err
		}
		for _, m := range messages {
			m.Attachments = atts[m.ID]
		}
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// FollowThread makes the user follow a thread. read also marks it read up
// to now (they just posted in it); otherwise their read position is kept.
func (db *DB) FollowThread(userID, threadID uuid.UUID, read bool) error {
	if read {
		_, err := db.Exec(`
			INSERT INTO thread_follows (user_id, thread_id, last_read_at) VALUES (?, ?, ?)
			ON CONFLICT(user_id, thread_id) DO UPDATE SET last_read_at = excluded.last_read_at`,
			userID.String(), threadID.String(), time.Now().UTC())
		return err
	}
	_, err := db.Exec(`INSERT OR IGNORE INTO thread_follows (user_id, thread_id) VALUES (?, ?)`,
		userID.String(), threadID.String())
	return err
}

// MarkThreadRead records that the user has read a thread up to now. Only a
// thread they follow keeps a read position.
func (db *DB) MarkThreadRead(userID, threadID uuid.UUID) error {
	_, err := db.Exec(`UPDATE thread_follows SET last_read_at = ? WHERE user_id = ? AND thread_id = ?`,
		time.Now().UTC(), userID.String(), threadID.String())
	return err
}

// ThreadFollow is one user's follow of a thread.
type ThreadFollow struct {
	LastReadAt *time.Time // nil: never read
}

// GetThreadFollows returns which of the threads the user follows, and how
// far they've read each.
func (db *DB) GetThreadFollows(userID uuid.UUID, threadIDs []uuid.UUID) (map[uuid.UUID]ThreadFollow, error) {
	out := make(map[uuid.UUID]ThreadFollow)
	if len(threadIDs) == 0 {
		return out, nil
	}
	in, args := inList(threadIDs)
	rows, err := db.Query(fmt.Sprintf(`SELECT thread_id, last_read_at FROM thread_follows WHERE user_id = ? AND thread_id IN (%s)`, in),
		append([]any{userID.String()}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var threadStr string
		var read sql.NullTime
		if err := rows.Scan(&threadStr, &read); err != nil {
			return nil, err
		}
		tid, err := uuid.Parse(threadStr)
		if err != nil {
			continue
		}
		f := ThreadFollow{}
		if read.Valid {
			t := read.Time
			f.LastReadAt = &t
		}
		out[tid] = f
	}
	return out, rows.Err()
}

// GetThreadFollowers lists everyone who follows a thread.
func (db *DB) GetThreadFollowers(threadID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Query(`SELECT user_id FROM thread_follows WHERE thread_id = ?`, threadID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if id, err := uuid.Parse(s); err == nil {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}
