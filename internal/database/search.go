package database

// Message search: SQLite's full-text index (FTS5) over message text,
// kept in step by triggers. Its row key is messages.seq, not the hidden
// rowid, because VACUUM (and so a backup made with VACUUM INTO) may
// renumber rowids of a table whose key is text.

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Snippet markers around matched words (control characters: never typed).
const (
	SnippetOpen  = "\x02"
	SnippetClose = "\x03"
)

// migrateSearch creates the index and its triggers, and indexes every
// existing message once.
func migrateSearch(db *DB) error {
	if _, err := db.Exec(`
		CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(
			content, content='messages', content_rowid='seq',
			tokenize='unicode61 remove_diacritics 2');
		CREATE TRIGGER IF NOT EXISTS messages_fts_insert AFTER INSERT ON messages WHEN new.seq IS NOT NULL BEGIN
			INSERT INTO messages_fts(rowid, content) VALUES (new.seq, new.content);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_fts_delete AFTER DELETE ON messages WHEN old.seq IS NOT NULL BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.seq, old.content);
		END;
		CREATE TRIGGER IF NOT EXISTS messages_fts_update AFTER UPDATE OF content ON messages WHEN old.seq IS NOT NULL BEGIN
			INSERT INTO messages_fts(messages_fts, rowid, content) VALUES ('delete', old.seq, old.content);
			INSERT INTO messages_fts(rowid, content) VALUES (new.seq, new.content);
		END;`); err != nil {
		return fmt.Errorf("creating the search index: %w", err)
	}
	return runOnce(db, "2026-11-05-search-index", func() error {
		_, err := db.Exec(`INSERT INTO messages_fts(messages_fts) VALUES ('rebuild')`)
		return err
	})
}

// SearchQuery is a search within some channels of a server.
type SearchQuery struct {
	UserID    uuid.UUID   // who's searching: their whispers only
	Channels  []uuid.UUID // the channels they may see (required)
	Match     string      // an FTS5 expression ("" : filters only, newest first)
	Authors   []uuid.UUID // from any of these
	HasLink   bool
	HasFile   bool
	AfterSeq  int64 // seq >= this (0: no limit)
	BeforeSeq int64 // seq < this (0: no limit)
	Newest    bool  // newest first instead of best match
	Offset    int
	Limit     int
}

// SearchHit is one message found.
type SearchHit struct {
	Message *models.Message
	Snippet string  // the text around the matches, marked with SnippetOpen/Close
	Score   float64 // lower is a better match (bm25); 0 without words
}

// SearchMessages runs a search: undeleted messages, whispers only to or
// from the searcher, thread replies included.
func (db *DB) SearchMessages(q SearchQuery) ([]SearchHit, error) {
	if len(q.Channels) == 0 {
		return nil, nil
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(q.Channels)), ",")
	args := []any{}
	from := "messages m"
	cols := "'' , 0.0"
	where := []string{"m.channel_id IN (" + ph + ")"}
	if q.Match != "" {
		from = "messages_fts JOIN messages m ON m.seq = messages_fts.rowid"
		cols = "snippet(messages_fts, 0, '" + SnippetOpen + "', '" + SnippetClose + "', '…', 16), bm25(messages_fts)"
		where = append([]string{"messages_fts MATCH ?"}, where...)
		args = append(args, q.Match)
	}
	for _, c := range q.Channels {
		args = append(args, c.String())
	}
	u := q.UserID.String()
	where = append(where,
		"COALESCE(m.is_deleted, 0) = 0",
		"(COALESCE(m.is_whisper, 0) = 0 OR m.author_id = ? OR m.recipient_id = ?)")
	args = append(args, u, u)
	if len(q.Authors) > 0 {
		where = append(where, "m.author_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(q.Authors)), ",")+")")
		for _, a := range q.Authors {
			args = append(args, a.String())
		}
	}
	if q.HasLink {
		where = append(where, "(m.content LIKE '%http://%' OR m.content LIKE '%https://%')")
	}
	if q.HasFile {
		where = append(where, "EXISTS (SELECT 1 FROM attachments a WHERE a.message_id = m.id)")
	}
	if q.AfterSeq > 0 {
		where = append(where, "m.seq >= ?")
		args = append(args, q.AfterSeq)
	}
	if q.BeforeSeq > 0 {
		where = append(where, "m.seq < ?")
		args = append(args, q.BeforeSeq)
	}
	order := "m.seq DESC"
	if q.Match != "" && !q.Newest {
		order = "bm25(messages_fts), m.seq DESC"
	}
	args = append(args, q.Limit, q.Offset)
	rows, err := db.Query(`
		SELECT m.id, m.channel_id, m.author_id, m.content, m.type, m.created_at, m.edited_at,
		       m.is_whisper, m.recipient_id, m.reply_to_id, m.thread_id, COALESCE(m.seq, 0), `+cols+`
		FROM `+from+`
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY `+order+`
		LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("searching: %w", err)
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		m := &models.Message{}
		var id, ch, author string
		var recipient, replyTo, thread *string
		var h SearchHit
		if err := rows.Scan(&id, &ch, &author, &m.Content, &m.Type, &m.CreatedAt, &m.EditedAt,
			&m.IsWhisper, &recipient, &replyTo, &thread, &m.Seq, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		m.ID, _ = uuid.Parse(id)
		m.ChannelID, _ = uuid.Parse(ch)
		m.AuthorID, _ = uuid.Parse(author)
		m.RecipientID, m.ReplyToID, m.ThreadID = parseIDPtr(recipient), parseIDPtr(replyTo), parseIDPtr(thread)
		h.Message = m
		out = append(out, h)
	}
	return out, rows.Err()
}

// SeqAt is the first message's Seq at or after t: how a date becomes a
// position (stored times are text in the server's zone, so they aren't
// compared in SQL). Arrival order and time order agree, so it's a binary
// search over Seq.
func (db *DB) SeqAt(t time.Time) (int64, error) {
	var hi int64
	if err := db.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM messages`).Scan(&hi); err != nil {
		return 0, err
	}
	lo := int64(1)
	hi++ // the answer is in [lo, hi]; hi means "after every message"
	for lo < hi {
		mid := lo + (hi-lo)/2
		var at time.Time
		err := db.QueryRow(`SELECT created_at FROM messages WHERE seq >= ? ORDER BY seq LIMIT 1`, mid).Scan(&at)
		if err != nil || !at.Before(t) {
			hi = mid
		} else {
			lo = mid + 1
		}
	}
	return lo, nil
}

// UserIDsByName is everyone a "from:name" can mean in a server: members by
// username or nickname, and anyone (a plugin's account) with that username.
func (db *DB) UserIDsByName(serverID uuid.UUID, names []string) ([]uuid.UUID, error) {
	ids, err := db.MemberIDsByName(serverID, names)
	if err != nil || len(names) == 0 {
		return ids, err
	}
	ph := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	args := make([]any, len(names))
	for i, n := range names {
		args[i] = strings.ToLower(n)
	}
	rows, err := db.Query(`SELECT id FROM users WHERE LOWER(username) IN (`+ph+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	have := map[uuid.UUID]bool{}
	for _, id := range ids {
		have[id] = true
	}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		if id, err := uuid.Parse(s); err == nil && !have[id] {
			have[id] = true
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}
