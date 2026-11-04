package database

import (
	"fmt"
	"os"
)

// Channel types ContentCounts leaves out: models.ChannelTypeCategory (2),
// ChannelTypeDM (3) and ChannelTypeGroupDM (4).

// BackupTo writes a consistent copy of the whole database to path (which
// must not exist yet), using SQLite's VACUUM INTO: the copy is taken inside
// one read transaction, so it's safe while the server runs, and it comes out
// compacted, with nothing left in a write-ahead log. With one connection in
// the pool, other queries wait while it runs (well under a second for a
// typical server's database).
func (db *DB) BackupTo(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists", path)
	}
	if _, err := db.Exec(`VACUUM INTO ?`, path); err != nil {
		return fmt.Errorf("backing up the database: %w", err)
	}
	return nil
}

// ContentCounts is how many channels (text, voice and plugin; categories
// and direct messages aside) and messages
// (deleted ones aside) the database holds, across every server in it.
func (db *DB) ContentCounts() (channels, messages int, err error) {
	if err = db.QueryRow(`SELECT COUNT(*) FROM channels WHERE type NOT IN (2, 3, 4)`).Scan(&channels); err != nil {
		return 0, 0, err
	}
	err = db.QueryRow(`SELECT COUNT(*) FROM messages WHERE COALESCE(is_deleted, 0) = 0`).Scan(&messages)
	return channels, messages, err
}
