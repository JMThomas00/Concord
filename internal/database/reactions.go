package database

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Reactions: one row per person, emoji and message (message_reactions,
// keyed on all three), so reacting twice with the same emoji is one
// reaction.

// AddReaction records userID reacting to a message with emoji. added is
// false if they already had.
func (db *DB) AddReaction(messageID, userID uuid.UUID, emoji string) (added bool, err error) {
	res, err := db.Exec(`INSERT OR IGNORE INTO message_reactions (message_id, user_id, emoji, created_at) VALUES (?, ?, ?, ?)`,
		messageID.String(), userID.String(), emoji, time.Now().UTC())
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RemoveReaction takes userID's reaction with emoji off a message. removed
// is false if they hadn't reacted with it.
func (db *DB) RemoveReaction(messageID, userID uuid.UUID, emoji string) (removed bool, err error) {
	res, err := db.Exec(`DELETE FROM message_reactions WHERE message_id = ? AND user_id = ? AND emoji = ?`,
		messageID.String(), userID.String(), emoji)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReactionEmoji lists the different emoji a message has been reacted with.
func (db *DB) ReactionEmoji(messageID uuid.UUID) ([]string, error) {
	rows, err := db.Query(`SELECT DISTINCT emoji FROM message_reactions WHERE message_id = ?`, messageID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// getMessageReactions loads the reactions on a set of messages: per
// message, each emoji in the order it was first used, with who reacted in
// the order they did.
func (db *DB) getMessageReactions(messageIDs []uuid.UUID) (map[uuid.UUID][]models.Reaction, error) {
	result := make(map[uuid.UUID][]models.Reaction)
	if len(messageIDs) == 0 {
		return result, nil
	}
	placeholders := make([]string, len(messageIDs))
	args := make([]interface{}, len(messageIDs))
	for i, id := range messageIDs {
		placeholders[i] = "?"
		args[i] = id.String()
	}
	rows, err := db.Query(fmt.Sprintf(`
		SELECT message_id, user_id, emoji FROM message_reactions
		WHERE message_id IN (%s) ORDER BY created_at, rowid`, strings.Join(placeholders, ",")), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var msgStr, userStr, emoji string
		if err := rows.Scan(&msgStr, &userStr, &emoji); err != nil {
			return nil, err
		}
		msgID, err1 := uuid.Parse(msgStr)
		userID, err2 := uuid.Parse(userStr)
		if err1 != nil || err2 != nil {
			continue
		}
		list := result[msgID]
		i := 0
		for i < len(list) && list[i].Emoji != emoji {
			i++
		}
		if i == len(list) {
			list = append(list, models.Reaction{Emoji: emoji})
		}
		list[i].UserIDs = append(list[i].UserIDs, userID)
		list[i].Count = len(list[i].UserIDs)
		result[msgID] = list
	}
	return result, rows.Err()
}
