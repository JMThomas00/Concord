package database

// Read positions: how far each member has read each channel, as the Seq
// of the last message read (channel_reads). Thread replies have their own
// (thread_follows, threads.go) and don't count here.

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ChannelUnread is one channel's unread state for one member.
type ChannelUnread struct {
	ChannelID uuid.UUID
	ReadSeq   int64 // the last message read
	Unread    int   // messages after it, from other people
	Mentions  int   // ...that mention them, @everyone, reply to them, or whisper to them
}

// MarkChannelRead records that the user has read the channel up to its
// newest message. It never moves a position back, and returns the
// position now.
func (db *DB) MarkChannelRead(userID, channelID uuid.UUID) (int64, error) {
	var seq int64
	err := db.QueryRow(`
		INSERT INTO channel_reads (user_id, channel_id, read_seq)
		VALUES (?, ?, (SELECT COALESCE(MAX(seq), 0) FROM messages WHERE channel_id = ?))
		ON CONFLICT(user_id, channel_id) DO UPDATE SET read_seq = MAX(read_seq, excluded.read_seq)
		RETURNING read_seq`,
		userID.String(), channelID.String(), channelID.String()).Scan(&seq)
	return seq, err
}

// markServerRead marks every channel of a server read for a user (a new
// member).
func (db *DB) markServerRead(userID, serverID uuid.UUID) error {
	_, err := db.Exec(`
		INSERT INTO channel_reads (user_id, channel_id, read_seq)
		SELECT ?, c.id, (SELECT COALESCE(MAX(seq), 0) FROM messages)
		FROM channels c WHERE c.server_id = ?
		ON CONFLICT(user_id, channel_id) DO UPDATE SET read_seq = MAX(read_seq, excluded.read_seq)`,
		userID.String(), serverID.String())
	return err
}

// ServerUnreads is the user's unread state in every channel of a server
// that has a read position or anything unread. A channel with no read
// position (one made after they joined) counts all of its messages. Own
// messages, thread replies, deleted messages and other people's whispers
// don't count. Counts stop at 1000.
func (db *DB) ServerUnreads(userID, serverID uuid.UUID) ([]ChannelUnread, error) {
	u := userID.String()
	rows, err := db.Query(`
		SELECT c.id,
		       COALESCE(r.read_seq, 0),
		       (SELECT COUNT(*) FROM (SELECT 1 FROM messages m
		            WHERE m.channel_id = c.id AND m.seq > COALESCE(r.read_seq, 0) AND m.thread_id IS NULL
		              AND COALESCE(m.is_deleted, 0) = 0 AND m.author_id != ?
		              AND (COALESCE(m.is_whisper, 0) = 0 OR m.recipient_id = ?)
		            LIMIT 1000)),
		       (SELECT COUNT(*) FROM (SELECT 1 FROM messages m
		            WHERE m.channel_id = c.id AND m.seq > COALESCE(r.read_seq, 0) AND m.thread_id IS NULL
		              AND COALESCE(m.is_deleted, 0) = 0 AND m.author_id != ?
		              AND (COALESCE(m.is_whisper, 0) = 0 OR m.recipient_id = ?)
		              AND (COALESCE(m.mention_everyone, 0) = 1 OR m.recipient_id = ?
		                   OR EXISTS (SELECT 1 FROM message_mentions mm WHERE mm.message_id = m.id AND mm.user_id = ?)
		                   OR EXISTS (SELECT 1 FROM messages p WHERE p.id = m.reply_to_id AND p.author_id = ?))
		            LIMIT 1000))
		FROM channels c
		LEFT JOIN channel_reads r ON r.channel_id = c.id AND r.user_id = ?
		WHERE c.server_id = ? AND c.type NOT IN (2, 3, 4)`,
		u, u, u, u, u, u, u, u, serverID.String())
	if err != nil {
		return nil, fmt.Errorf("reading unread counts: %w", err)
	}
	defer rows.Close()
	var out []ChannelUnread
	for rows.Next() {
		var cu ChannelUnread
		var id string
		if err := rows.Scan(&id, &cu.ReadSeq, &cu.Unread, &cu.Mentions); err != nil {
			return nil, err
		}
		cu.ChannelID, _ = uuid.Parse(id)
		out = append(out, cu)
	}
	return out, rows.Err()
}

// MemberIDsByName finds the server's members whose username or nickname
// is one of names (compared without case): who an "@name" means.
func (db *DB) MemberIDsByName(serverID uuid.UUID, names []string) ([]uuid.UUID, error) {
	if len(names) == 0 {
		return nil, nil
	}
	ph := make([]string, len(names))
	args := []any{serverID.String()}
	for i, n := range names {
		ph[i] = "?"
		args = append(args, strings.ToLower(n))
	}
	in := strings.Join(ph, ", ")
	args = append(args, args[1:]...)
	rows, err := db.Query(fmt.Sprintf(`
		SELECT DISTINCT sm.user_id FROM server_members sm JOIN users u ON u.id = sm.user_id
		WHERE sm.server_id = ? AND (LOWER(u.username) IN (%s) OR LOWER(COALESCE(sm.nickname, '')) IN (%s))`, in, in), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if u, err := uuid.Parse(id); err == nil {
			out = append(out, u)
		}
	}
	return out, rows.Err()
}
