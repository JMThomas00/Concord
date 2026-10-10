package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PluginRecordRow is one member's stored record for a plugin: the JSON the
// plugin sent (sdk/wire.PluginRecord), after the server has checked it.
type PluginRecordRow struct {
	PluginID  string
	UserID    uuid.UUID
	Record    string
	UpdatedAt time.Time
}

// SavePluginRecord stores (or replaces) a member's record for a plugin.
func (db *DB) SavePluginRecord(pluginID string, userID uuid.UUID, record string) error {
	_, err := db.Exec(`
		INSERT INTO plugin_records (plugin_id, user_id, record, updated_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(plugin_id, user_id) DO UPDATE SET record = excluded.record, updated_at = excluded.updated_at`,
		pluginID, userID.String(), record, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to save plugin record: %w", err)
	}
	return nil
}

// GetPluginRecord returns a member's record for a plugin ("" if none).
func (db *DB) GetPluginRecord(pluginID string, userID uuid.UUID) (string, error) {
	var rec string
	err := db.QueryRow(`SELECT record FROM plugin_records WHERE plugin_id = ? AND user_id = ?`,
		pluginID, userID.String()).Scan(&rec)
	if err != nil {
		return "", nil // none yet
	}
	return rec, nil
}

// PluginRecordsForUser returns every plugin's record for a member.
func (db *DB) PluginRecordsForUser(userID uuid.UUID) ([]PluginRecordRow, error) {
	return db.pluginRecords(`SELECT plugin_id, user_id, record, updated_at FROM plugin_records WHERE user_id = ?`, userID.String())
}

// PluginRecordsForPlugin returns every member's record for a plugin, for
// its leaderboard, leaving out members who keep themselves off them.
func (db *DB) PluginRecordsForPlugin(pluginID string) ([]PluginRecordRow, error) {
	return db.pluginRecords(`
		SELECT r.plugin_id, r.user_id, r.record, r.updated_at FROM plugin_records r
		JOIN users u ON u.id = r.user_id
		WHERE r.plugin_id = ? AND COALESCE(u.leaderboard_hidden, 0) = 0`, pluginID)
}

func (db *DB) pluginRecords(query string, arg string) ([]PluginRecordRow, error) {
	rows, err := db.Query(query, arg)
	if err != nil {
		return nil, fmt.Errorf("failed to load plugin records: %w", err)
	}
	defer rows.Close()
	var out []PluginRecordRow
	for rows.Next() {
		var r PluginRecordRow
		var uid string
		if err := rows.Scan(&r.PluginID, &uid, &r.Record, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.UserID, _ = uuid.Parse(uid)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SetLeaderboardHidden keeps a member off (or puts them back on) every
// leaderboard on this server.
func (db *DB) SetLeaderboardHidden(userID uuid.UUID, hidden bool) error {
	v := 0
	if hidden {
		v = 1
	}
	if _, err := db.Exec(`UPDATE users SET leaderboard_hidden = ? WHERE id = ?`, v, userID.String()); err != nil {
		return fmt.Errorf("failed to update leaderboard visibility: %w", err)
	}
	return nil
}

// LeaderboardHidden reports whether a member keeps off the leaderboards.
func (db *DB) LeaderboardHidden(userID uuid.UUID) bool {
	var v int
	_ = db.QueryRow(`SELECT COALESCE(leaderboard_hidden, 0) FROM users WHERE id = ?`, userID.String()).Scan(&v)
	return v != 0
}
