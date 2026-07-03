package database

import (
	"database/sql"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"github.com/concord-chat/concord/internal/models"
)

// DebugEnabled gates verbose debug logging in the database package.
// Set to true via SetDebug when the server is started with --debug or --log-level debug.
var DebugEnabled bool

// SetDebug enables or disables debug logging for the database package.
func SetDebug(enabled bool) {
	DebugEnabled = enabled
}

// debugf logs a formatted message only when DebugEnabled is true.
func debugf(format string, args ...any) {
	if DebugEnabled {
		log.Printf("DEBUG "+format, args...)
	}
}

// DB wraps the SQLite database connection
type DB struct {
	*sql.DB
}

// New creates a new database connection and initializes schema
func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite only supports one writer
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	wrapper := &DB{db}
	if err := wrapper.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Run database migrations
	if err := wrapper.MigrateChannelSortOrder(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	if err := wrapper.MigrateOrphanedCategories(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate orphaned categories: %w", err)
	}

	if err := wrapper.MigrateRoleDisplayOrder(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate role display_order: %w", err)
	}

	if err := wrapper.MigrateServerMemberCustomTitle(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate server_members custom_title: %w", err)
	}

	if err := wrapper.MigrateMessageWhisperFields(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate message whisper fields: %w", err)
	}

	if err := wrapper.MigrateMessageSoftDelete(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate message soft delete: %w", err)
	}

	if err := wrapper.MigrateChannelIsLocked(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate channel is_locked: %w", err)
	}

	if err := wrapper.MigrateMemberKickCount(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate member kick_count: %w", err)
	}

	if err := wrapper.MigrateServerMembersIsBanned(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate member is_banned: %w", err)
	}

	if err := wrapper.MigrateMutesServerWide(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate mutes server-wide: %w", err)
	}

	if err := wrapper.MigrateVoiceStates(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate voice_states: %w", err)
	}

	if err := wrapper.MigrateVoiceChannelSettings(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate voice channel settings: %w", err)
	}

	// Clear any stale voice state from a previous server run
	if err := wrapper.ClearAllVoiceStates(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to clear stale voice states: %w", err)
	}

	return wrapper, nil
}

// initSchema creates the database tables if they don't exist
func (db *DB) initSchema() error {
	schema := `
	-- Users table
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		discriminator TEXT NOT NULL,
		display_name TEXT,
		email TEXT UNIQUE,
		password_hash TEXT NOT NULL,
		avatar_hash TEXT,
		status TEXT DEFAULT 'offline',
		status_text TEXT,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		last_seen_at DATETIME,
		is_bot INTEGER DEFAULT 0,
		UNIQUE(username, discriminator)
	);

	-- Servers table
	CREATE TABLE IF NOT EXISTS servers (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT,
		icon_hash TEXT,
		owner_id TEXT NOT NULL REFERENCES users(id),
		default_channel_id TEXT,
		system_channel_id TEXT,
		rules_channel_id TEXT,
		max_members INTEGER DEFAULT 1000,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		verification_level INTEGER DEFAULT 0,
		explicit_content_filter INTEGER DEFAULT 0,
		invites_enabled INTEGER DEFAULT 1,
		default_invite_max_age INTEGER DEFAULT 86400,
		default_invite_max_uses INTEGER DEFAULT 0
	);

	-- Channels table
	CREATE TABLE IF NOT EXISTS channels (
		id TEXT PRIMARY KEY,
		server_id TEXT REFERENCES servers(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		topic TEXT,
		type INTEGER NOT NULL,
		position INTEGER DEFAULT 0,
		category_id TEXT REFERENCES channels(id),
		is_nsfw INTEGER DEFAULT 0,
		is_locked INTEGER DEFAULT 0,
		rate_limit_per_user INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	-- Messages table
	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		author_id TEXT NOT NULL REFERENCES users(id),
		content TEXT NOT NULL,
		type INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		edited_at DATETIME,
		is_pinned INTEGER DEFAULT 0,
		reply_to_id TEXT REFERENCES messages(id)
	);

	-- Roles table
	CREATE TABLE IF NOT EXISTS roles (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		color INTEGER DEFAULT 0,
		permissions INTEGER NOT NULL,
		position INTEGER DEFAULT 0,
		is_hoisted INTEGER DEFAULT 0,
		is_mentionable INTEGER DEFAULT 1,
		is_default INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL
	);

	-- Server members junction table
	CREATE TABLE IF NOT EXISTS server_members (
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		nickname TEXT,
		joined_at DATETIME NOT NULL,
		is_muted INTEGER DEFAULT 0,
		is_deafened INTEGER DEFAULT 0,
		PRIMARY KEY (user_id, server_id)
	);

	-- Member roles junction table
	CREATE TABLE IF NOT EXISTS member_roles (
		user_id TEXT NOT NULL,
		server_id TEXT NOT NULL,
		role_id TEXT NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		PRIMARY KEY (user_id, server_id, role_id),
		FOREIGN KEY (user_id, server_id) REFERENCES server_members(user_id, server_id) ON DELETE CASCADE
	);

	-- DM channel recipients
	CREATE TABLE IF NOT EXISTS dm_recipients (
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (channel_id, user_id)
	);

	-- Invites table
	CREATE TABLE IF NOT EXISTS invites (
		code TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		channel_id TEXT REFERENCES channels(id),
		inviter_id TEXT NOT NULL REFERENCES users(id),
		max_age INTEGER DEFAULT 86400,
		max_uses INTEGER DEFAULT 0,
		uses INTEGER DEFAULT 0,
		created_at DATETIME NOT NULL,
		expires_at DATETIME,
		is_revoked INTEGER DEFAULT 0
	);

	-- Message mentions
	CREATE TABLE IF NOT EXISTS message_mentions (
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		PRIMARY KEY (message_id, user_id)
	);

	-- Message reactions
	CREATE TABLE IF NOT EXISTS message_reactions (
		message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		emoji TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		PRIMARY KEY (message_id, user_id, emoji)
	);

	-- Permission overwrites
	CREATE TABLE IF NOT EXISTS permission_overwrites (
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		target_id TEXT NOT NULL,
		target_type TEXT NOT NULL,
		allow INTEGER DEFAULT 0,
		deny INTEGER DEFAULT 0,
		PRIMARY KEY (channel_id, target_id)
	);

	-- Sessions table for authentication
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		token_hash TEXT NOT NULL UNIQUE,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		last_used_at DATETIME,
		ip_address TEXT,
		user_agent TEXT
	);

	-- Bans table
	CREATE TABLE IF NOT EXISTS bans (
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		reason TEXT,
		banned_by TEXT NOT NULL REFERENCES users(id),
		banned_at DATETIME NOT NULL,
		PRIMARY KEY (server_id, user_id)
	);

	-- Timeouts table (temporary bans)
	CREATE TABLE IF NOT EXISTS timeouts (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		reason TEXT,
		duration INTEGER NOT NULL,
		issued_by TEXT NOT NULL REFERENCES users(id),
		issued_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL
	);

	-- Mutes table (timed server mutes)
	CREATE TABLE IF NOT EXISTS mutes (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
		duration INTEGER NOT NULL,
		issued_by TEXT NOT NULL REFERENCES users(id),
		issued_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL
	);

	-- Message retention policies (server defaults + channel overrides)
	CREATE TABLE IF NOT EXISTS message_retention_policies (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
		time_retention_days INTEGER,
		system_time_retention_days INTEGER,
		max_message_count INTEGER,
		preserve_pinned INTEGER DEFAULT 1,
		created_at DATETIME NOT NULL,
		updated_at DATETIME NOT NULL,
		created_by TEXT REFERENCES users(id),
		UNIQUE(server_id, channel_id)
	);

	-- Audit log for pruning operations
	CREATE TABLE IF NOT EXISTS message_prune_history (
		id TEXT PRIMARY KEY,
		server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
		channel_id TEXT REFERENCES channels(id) ON DELETE SET NULL,
		messages_deleted INTEGER NOT NULL,
		time_based_count INTEGER DEFAULT 0,
		count_based_count INTEGER DEFAULT 0,
		trigger_type TEXT NOT NULL,
		triggered_by TEXT REFERENCES users(id),
		executed_at DATETIME NOT NULL,
		duration_ms INTEGER
	);

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_author ON messages(author_id);
	CREATE INDEX IF NOT EXISTS idx_channels_server ON channels(server_id);
	CREATE INDEX IF NOT EXISTS idx_server_members_server ON server_members(server_id);
	CREATE INDEX IF NOT EXISTS idx_roles_server ON roles(server_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash);
	CREATE INDEX IF NOT EXISTS idx_invites_server ON invites(server_id);
	CREATE INDEX IF NOT EXISTS idx_timeouts_server_user ON timeouts(server_id, user_id);
	CREATE INDEX IF NOT EXISTS idx_timeouts_expires ON timeouts(expires_at);
	CREATE INDEX IF NOT EXISTS idx_mutes_server_user ON mutes(server_id, user_id);
	CREATE INDEX IF NOT EXISTS idx_mutes_expires ON mutes(expires_at);
	CREATE INDEX IF NOT EXISTS idx_retention_server ON message_retention_policies(server_id, channel_id);
	CREATE INDEX IF NOT EXISTS idx_messages_channel_pinned_created ON messages(channel_id, is_pinned, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_prune_history_server ON message_prune_history(server_id, executed_at DESC);
	`

	_, err := db.Exec(schema)
	return err
}

// MigrateChannelSortOrder adds sort_order column and migrates existing position values
func (db *DB) MigrateChannelSortOrder() error {
	// Check if column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('channels')
		WHERE name='sort_order'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for sort_order column: %w", err)
	}

	if count > 0 {
		return nil // Already migrated
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Add column
	_, err = tx.Exec(`ALTER TABLE channels ADD COLUMN sort_order INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add sort_order column: %w", err)
	}

	// Migrate: normalize positions within each parent
	rows, err := tx.Query(`
		SELECT id, server_id, category_id, position, type
		FROM channels
		ORDER BY server_id, COALESCE(category_id, ''), position, id
	`)
	if err != nil {
		return fmt.Errorf("failed to query channels: %w", err)
	}
	defer rows.Close()

	type channelPos struct {
		id         string
		serverID   string
		categoryID string
		sortOrder  int
	}

	var channels []channelPos
	orderMap := make(map[string]int) // key: "serverID|categoryID"

	for rows.Next() {
		var id, serverID, categoryID string
		var position, channelType int
		err = rows.Scan(&id, &serverID, &categoryID, &position, &channelType)
		if err != nil {
			return fmt.Errorf("failed to scan channel: %w", err)
		}

		key := serverID + "|" + categoryID
		order := orderMap[key]
		orderMap[key] = order + 10

		channels = append(channels, channelPos{
			id:         id,
			serverID:   serverID,
			categoryID: categoryID,
			sortOrder:  order,
		})
	}

	if err = rows.Err(); err != nil {
		return fmt.Errorf("error iterating channels: %w", err)
	}

	// Update all channels with new sort_order
	stmt, err := tx.Prepare(`UPDATE channels SET sort_order = ? WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("failed to prepare update statement: %w", err)
	}
	defer stmt.Close()

	for _, ch := range channels {
		_, err = stmt.Exec(ch.sortOrder, ch.id)
		if err != nil {
			return fmt.Errorf("failed to update sort_order for channel %s: %w", ch.id, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// MigrateOrphanedCategories fixes categories that were accidentally created with a category_id set.
// Categories must always be at root level. Clear category_id for any that have one.
func (db *DB) MigrateOrphanedCategories() error {
	_, err := db.Exec(`
		UPDATE channels
		SET category_id = NULL, updated_at = ?
		WHERE type = 2 AND category_id IS NOT NULL
	`, time.Now())
	if err != nil {
		return fmt.Errorf("failed to clear orphaned categories: %w", err)
	}
	return nil
}

// MigrateRoleDisplayOrder adds display_order column to roles table
func (db *DB) MigrateRoleDisplayOrder() error {
	// Check if column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('roles')
		WHERE name='display_order'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for display_order column: %w", err)
	}

	if count > 0 {
		return nil // Already migrated
	}

	// Add column
	_, err = db.Exec(`ALTER TABLE roles ADD COLUMN display_order INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add display_order column: %w", err)
	}

	return nil
}

// MigrateServerMemberCustomTitle adds custom_title column to server_members table
func (db *DB) MigrateServerMemberCustomTitle() error {
	// Check if column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('server_members')
		WHERE name='custom_title'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for custom_title column: %w", err)
	}

	if count > 0 {
		log.Println("[MIGRATION] custom_title column already exists in server_members table")
		return nil // Already migrated
	}

	// Add column
	log.Println("[MIGRATION] Adding custom_title column to server_members table...")
	_, err = db.Exec(`ALTER TABLE server_members ADD COLUMN custom_title TEXT DEFAULT ''`)
	if err != nil {
		return fmt.Errorf("failed to add custom_title column: %w", err)
	}

	log.Println("[MIGRATION] Successfully added custom_title column to server_members table")
	return nil
}

// MigrateMessageWhisperFields adds is_whisper and recipient_id to messages table
func (db *DB) MigrateMessageWhisperFields() error {
	// Check if is_whisper column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name='is_whisper'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for is_whisper column: %w", err)
	}

	if count == 0 {
		// Add is_whisper column
		_, err = db.Exec(`ALTER TABLE messages ADD COLUMN is_whisper INTEGER DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("failed to add is_whisper column: %w", err)
		}
	}

	// Check if recipient_id column exists
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name='recipient_id'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for recipient_id column: %w", err)
	}

	if count == 0 {
		// Add recipient_id column
		_, err = db.Exec(`ALTER TABLE messages ADD COLUMN recipient_id TEXT REFERENCES users(id)`)
		if err != nil {
			return fmt.Errorf("failed to add recipient_id column: %w", err)
		}
	}

	return nil
}

// MigrateMessageSoftDelete adds soft-delete columns to messages table
func (db *DB) MigrateMessageSoftDelete() error {
	// Check if is_deleted column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name='is_deleted'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for is_deleted column: %w", err)
	}

	if count == 0 {
		// Add is_deleted column
		_, err = db.Exec(`ALTER TABLE messages ADD COLUMN is_deleted INTEGER DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("failed to add is_deleted column: %w", err)
		}
	}

	// Check if deleted_at column exists
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name='deleted_at'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for deleted_at column: %w", err)
	}

	if count == 0 {
		// Add deleted_at column
		_, err = db.Exec(`ALTER TABLE messages ADD COLUMN deleted_at DATETIME`)
		if err != nil {
			return fmt.Errorf("failed to add deleted_at column: %w", err)
		}
	}

	// Check if deleted_by column exists
	err = db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('messages')
		WHERE name='deleted_by'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for deleted_by column: %w", err)
	}

	if count == 0 {
		// Add deleted_by column
		_, err = db.Exec(`ALTER TABLE messages ADD COLUMN deleted_by TEXT REFERENCES users(id)`)
		if err != nil {
			return fmt.Errorf("failed to add deleted_by column: %w", err)
		}
	}

	return nil
}

// MigrateChannelIsLocked adds the is_locked column to the channels table
func (db *DB) MigrateChannelIsLocked() error {
	// Check if is_locked column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('channels')
		WHERE name='is_locked'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for is_locked column: %w", err)
	}

	if count == 0 {
		// Add is_locked column
		_, err = db.Exec(`ALTER TABLE channels ADD COLUMN is_locked INTEGER DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("failed to add is_locked column: %w", err)
		}
		log.Println("Migration: Added is_locked column to channels table")
	}

	return nil
}

// MigrateMemberKickCount adds kick_count column to server_members table
func (db *DB) MigrateMemberKickCount() error {
	// Check if kick_count column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('server_members')
		WHERE name='kick_count'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for kick_count column: %w", err)
	}

	if count > 0 {
		log.Println("[MIGRATION] kick_count column already exists in server_members table")
		return nil // Already migrated
	}

	// Add column
	log.Println("[MIGRATION] Adding kick_count column to server_members table...")
	_, err = db.Exec(`ALTER TABLE server_members ADD COLUMN kick_count INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add kick_count column: %w", err)
	}

	log.Println("[MIGRATION] Successfully added kick_count column to server_members table")
	return nil
}

// MigrateMutesServerWide makes mutes.channel_id nullable for server-wide mutes
func (db *DB) MigrateMutesServerWide() error {
	// Check if mutes table has the right schema
	// SQLite doesn't support ALTER COLUMN, so we need to recreate the table
	var hasNotNull int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('mutes')
		WHERE name='channel_id' AND "notnull"=1
	`).Scan(&hasNotNull)
	if err != nil {
		return fmt.Errorf("failed to check mutes table schema: %w", err)
	}

	if hasNotNull == 0 {
		log.Println("[MIGRATION] mutes table already supports server-wide mutes")
		return nil // Already migrated
	}

	log.Println("[MIGRATION] Migrating mutes table to support server-wide mutes...")

	// Create new table with nullable channel_id
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS mutes_new (
			id TEXT PRIMARY KEY,
			server_id TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			channel_id TEXT REFERENCES channels(id) ON DELETE CASCADE,
			muted_by TEXT NOT NULL REFERENCES users(id),
			muted_at DATETIME NOT NULL,
			muted_until DATETIME,
			reason TEXT,
			duration INTEGER NOT NULL,
			issued_by TEXT NOT NULL REFERENCES users(id),
			issued_at DATETIME NOT NULL,
			expires_at DATETIME NOT NULL,
			UNIQUE(server_id, user_id, channel_id)
		)
	`)
	if err != nil {
		return fmt.Errorf("failed to create mutes_new table: %w", err)
	}

	// Copy existing data
	_, err = db.Exec(`
		INSERT INTO mutes_new (id, server_id, user_id, channel_id, muted_by, muted_at, duration, issued_by, issued_at, expires_at)
		SELECT id, server_id, user_id, channel_id, issued_by, issued_at, duration, issued_by, issued_at, expires_at
		FROM mutes
	`)
	if err != nil {
		return fmt.Errorf("failed to copy mutes data: %w", err)
	}

	// Drop old table
	_, err = db.Exec(`DROP TABLE mutes`)
	if err != nil {
		return fmt.Errorf("failed to drop old mutes table: %w", err)
	}

	// Rename new table
	_, err = db.Exec(`ALTER TABLE mutes_new RENAME TO mutes`)
	if err != nil {
		return fmt.Errorf("failed to rename mutes_new table: %w", err)
	}

	log.Println("[MIGRATION] Successfully migrated mutes table to support server-wide mutes")
	return nil
}

// MigrateServerMembersIsBanned adds is_banned column to server_members table
func (db *DB) MigrateServerMembersIsBanned() error {
	// Check if is_banned column exists
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM pragma_table_info('server_members')
		WHERE name='is_banned'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check for is_banned column: %w", err)
	}

	if count == 0 {
		// Add is_banned column
		_, err = db.Exec(`ALTER TABLE server_members ADD COLUMN is_banned INTEGER DEFAULT 0`)
		if err != nil {
			return fmt.Errorf("failed to add is_banned column: %w", err)
		}
		log.Println("Migration: Added is_banned column to server_members table")

		// Sync existing bans: create server_member records for banned users that don't have one
		// This handles the case where users were banned before the migration
		_, err = db.Exec(`
			INSERT OR IGNORE INTO server_members (user_id, server_id, joined_at, is_banned)
			SELECT user_id, server_id, banned_at, 1
			FROM bans
			WHERE NOT EXISTS (
				SELECT 1 FROM server_members sm
				WHERE sm.user_id = bans.user_id AND sm.server_id = bans.server_id
			)
		`)
		if err != nil {
			log.Printf("Warning: Failed to sync bans to server_members: %v", err)
		} else {
			log.Println("Migration: Synced existing bans to server_members table")
		}
	}

	return nil
}

// --- User Operations ---

// CreateUser inserts a new user into the database
func (db *DB) CreateUser(user *models.User, passwordHash string) error {
	_, err := db.Exec(`
		INSERT INTO users (id, username, discriminator, display_name, email, password_hash, 
			status, created_at, updated_at, is_bot)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID.String(), user.Username, user.Discriminator, user.DisplayName,
		user.Email, passwordHash, user.Status, user.CreatedAt, user.UpdatedAt, user.IsBot)
	return err
}

// GetUserByID retrieves a user by their ID
func (db *DB) GetUserByID(id uuid.UUID) (*models.User, error) {
	user := &models.User{}
	var idStr string
	var displayName, avatarHash, statusText sql.NullString
	var lastSeenAt sql.NullTime

	err := db.QueryRow(`
		SELECT id, username, discriminator, display_name, email, avatar_hash,
			status, status_text, created_at, updated_at, last_seen_at, is_bot
		FROM users WHERE id = ?`, id.String()).Scan(
		&idStr, &user.Username, &user.Discriminator, &displayName,
		&user.Email, &avatarHash, &user.Status, &statusText,
		&user.CreatedAt, &user.UpdatedAt, &lastSeenAt, &user.IsBot)
	if err != nil {
		return nil, err
	}

	user.ID, _ = uuid.Parse(idStr)
	if displayName.Valid {
		user.DisplayName = displayName.String
	}
	if avatarHash.Valid {
		user.AvatarHash = avatarHash.String
	}
	if statusText.Valid {
		user.StatusText = statusText.String
	}
	if lastSeenAt.Valid {
		user.LastSeenAt = lastSeenAt.Time
	}

	return user, nil
}

// GetUserByEmail retrieves a user by their email
func (db *DB) GetUserByEmail(email string) (*models.User, string, error) {
	user := &models.User{}
	var idStr, passwordHash string
	var displayName, avatarHash, statusText sql.NullString
	var lastSeenAt sql.NullTime

	err := db.QueryRow(`
		SELECT id, username, discriminator, display_name, email, password_hash, avatar_hash,
			status, status_text, created_at, updated_at, last_seen_at, is_bot
		FROM users WHERE email = ?`, email).Scan(
		&idStr, &user.Username, &user.Discriminator, &displayName,
		&user.Email, &passwordHash, &avatarHash, &user.Status, &statusText,
		&user.CreatedAt, &user.UpdatedAt, &lastSeenAt, &user.IsBot)
	if err != nil {
		return nil, "", err
	}

	user.ID, _ = uuid.Parse(idStr)
	if displayName.Valid {
		user.DisplayName = displayName.String
	}
	if avatarHash.Valid {
		user.AvatarHash = avatarHash.String
	}
	if statusText.Valid {
		user.StatusText = statusText.String
	}
	if lastSeenAt.Valid {
		user.LastSeenAt = lastSeenAt.Time
	}

	return user, passwordHash, nil
}

// UpdateUserStatus updates a user's online status
func (db *DB) UpdateUserStatus(userID uuid.UUID, status models.UserStatus, statusText string) error {
	_, err := db.Exec(`
		UPDATE users SET status = ?, status_text = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ?`,
		status, statusText, time.Now(), time.Now(), userID.String())
	return err
}

// --- Server Operations ---

// CreateServer inserts a new server
func (db *DB) CreateServer(server *models.Server) error {
	_, err := db.Exec(`
		INSERT INTO servers (id, name, description, owner_id, max_members, 
			created_at, updated_at, invites_enabled, default_invite_max_age, default_invite_max_uses)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		server.ID.String(), server.Name, server.Description, server.OwnerID.String(),
		server.MaxMembers, server.CreatedAt, server.UpdatedAt, server.InvitesEnabled,
		server.DefaultInviteMaxAge, server.DefaultInviteMaxUses)
	return err
}

// GetServerByID retrieves a server by ID
func (db *DB) GetServerByID(id uuid.UUID) (*models.Server, error) {
	server := &models.Server{}
	var idStr, ownerIDStr string
	var iconHash, defaultChanID, systemChanID, rulesChanID sql.NullString

	err := db.QueryRow(`
		SELECT id, name, description, icon_hash, owner_id, default_channel_id,
			system_channel_id, rules_channel_id, max_members, created_at, updated_at,
			verification_level, explicit_content_filter, invites_enabled,
			default_invite_max_age, default_invite_max_uses
		FROM servers WHERE id = ?`, id.String()).Scan(
		&idStr, &server.Name, &server.Description, &iconHash,
		&ownerIDStr, &defaultChanID, &systemChanID, &rulesChanID,
		&server.MaxMembers, &server.CreatedAt, &server.UpdatedAt,
		&server.VerificationLevel, &server.ExplicitContentFilter,
		&server.InvitesEnabled, &server.DefaultInviteMaxAge, &server.DefaultInviteMaxUses)
	if err != nil {
		return nil, err
	}

	server.ID, _ = uuid.Parse(idStr)
	server.OwnerID, _ = uuid.Parse(ownerIDStr)
	if iconHash.Valid {
		server.IconHash = iconHash.String
	}
	if defaultChanID.Valid {
		server.DefaultChannelID, _ = uuid.Parse(defaultChanID.String)
	}
	if systemChanID.Valid {
		server.SystemChannelID, _ = uuid.Parse(systemChanID.String)
	}
	if rulesChanID.Valid {
		server.RulesChannelID, _ = uuid.Parse(rulesChanID.String)
	}

	return server, nil
}

// GetAllServers retrieves all servers in the database
func (db *DB) GetAllServers() ([]*models.Server, error) {
	rows, err := db.Query(`
		SELECT id, name, description, icon_hash, owner_id, default_channel_id,
			system_channel_id, rules_channel_id, max_members, created_at, updated_at,
			verification_level, explicit_content_filter, invites_enabled,
			default_invite_max_age, default_invite_max_uses
		FROM servers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []*models.Server
	for rows.Next() {
		server := &models.Server{}
		var idStr, ownerIDStr string
		var iconHash, defaultChanID, systemChanID, rulesChanID sql.NullString

		err := rows.Scan(
			&idStr, &server.Name, &server.Description, &iconHash,
			&ownerIDStr, &defaultChanID, &systemChanID, &rulesChanID,
			&server.MaxMembers, &server.CreatedAt, &server.UpdatedAt,
			&server.VerificationLevel, &server.ExplicitContentFilter,
			&server.InvitesEnabled, &server.DefaultInviteMaxAge, &server.DefaultInviteMaxUses,
		)
		if err != nil {
			return nil, err
		}

		server.ID, _ = uuid.Parse(idStr)
		server.OwnerID, _ = uuid.Parse(ownerIDStr)
		if iconHash.Valid {
			server.IconHash = iconHash.String
		}
		if defaultChanID.Valid {
			server.DefaultChannelID, _ = uuid.Parse(defaultChanID.String)
		}
		if systemChanID.Valid {
			server.SystemChannelID, _ = uuid.Parse(systemChanID.String)
		}
		if rulesChanID.Valid {
			server.RulesChannelID, _ = uuid.Parse(rulesChanID.String)
		}

		servers = append(servers, server)
	}

	return servers, rows.Err()
}

// GetUserServers retrieves all servers a user is a member of
func (db *DB) GetUserServers(userID uuid.UUID) ([]*models.Server, error) {
	rows, err := db.Query(`
		SELECT s.id, s.name, s.description, s.icon_hash, s.owner_id, s.default_channel_id,
			s.system_channel_id, s.max_members, s.created_at, s.updated_at
		FROM servers s
		JOIN server_members sm ON s.id = sm.server_id
		WHERE sm.user_id = ?`, userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var servers []*models.Server
	for rows.Next() {
		server := &models.Server{}
		var idStr, ownerIDStr string
		var iconHash, defaultChanID, systemChanID sql.NullString

		err := rows.Scan(&idStr, &server.Name, &server.Description, &iconHash,
			&ownerIDStr, &defaultChanID, &systemChanID, &server.MaxMembers,
			&server.CreatedAt, &server.UpdatedAt)
		if err != nil {
			return nil, err
		}

		server.ID, _ = uuid.Parse(idStr)
		server.OwnerID, _ = uuid.Parse(ownerIDStr)
		if iconHash.Valid {
			server.IconHash = iconHash.String
		}
		if defaultChanID.Valid {
			server.DefaultChannelID, _ = uuid.Parse(defaultChanID.String)
		}
		if systemChanID.Valid {
			server.SystemChannelID, _ = uuid.Parse(systemChanID.String)
		}

		servers = append(servers, server)
	}

	return servers, rows.Err()
}

// --- Channel Operations ---

// CreateChannel inserts a new channel
func (db *DB) CreateChannel(channel *models.Channel) error {
	var serverID, categoryID sql.NullString
	if channel.ServerID != uuid.Nil {
		serverID.String = channel.ServerID.String()
		serverID.Valid = true
	}
	if channel.CategoryID != uuid.Nil {
		categoryID.String = channel.CategoryID.String()
		categoryID.Valid = true
	}

	// Calculate sort_order: find max in parent + 10
	var maxOrder int
	query := `
		SELECT COALESCE(MAX(sort_order), -10)
		FROM channels
		WHERE server_id = ? AND COALESCE(category_id, '') = COALESCE(?, '')
	`
	err := db.QueryRow(query, channel.ServerID.String(), categoryID).Scan(&maxOrder)
	if err != nil {
		return fmt.Errorf("failed to calculate sort_order: %w", err)
	}

	channel.SortOrder = maxOrder + 10
	channel.Position = channel.SortOrder // Keep in sync

	_, err = db.Exec(`
		INSERT INTO channels (id, server_id, name, topic, type, position, sort_order, category_id,
			is_nsfw, is_locked, rate_limit_per_user, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel.ID.String(), serverID, channel.Name, channel.Topic, channel.Type,
		channel.Position, channel.SortOrder, categoryID, channel.IsNSFW, channel.IsLocked, channel.RateLimitPerUser,
		channel.CreatedAt, channel.UpdatedAt)
	return err
}

// GetServerChannels retrieves all channels for a server
func (db *DB) GetServerChannels(serverID uuid.UUID) ([]*models.Channel, error) {
	rows, err := db.Query(`
		SELECT id, server_id, name, topic, type, position, sort_order, category_id,
			is_nsfw, is_locked, rate_limit_per_user, created_at, updated_at
		FROM channels WHERE server_id = ?
		ORDER BY sort_order`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channels []*models.Channel
	for rows.Next() {
		ch := &models.Channel{}
		var idStr, serverIDStr string
		var categoryID sql.NullString

		err := rows.Scan(&idStr, &serverIDStr, &ch.Name, &ch.Topic, &ch.Type,
			&ch.Position, &ch.SortOrder, &categoryID, &ch.IsNSFW, &ch.IsLocked, &ch.RateLimitPerUser,
			&ch.CreatedAt, &ch.UpdatedAt)
		if err != nil {
			return nil, err
		}

		ch.ID, _ = uuid.Parse(idStr)
		ch.ServerID, _ = uuid.Parse(serverIDStr)
		if categoryID.Valid {
			ch.CategoryID, _ = uuid.Parse(categoryID.String)
		}

		channels = append(channels, ch)
	}

	return channels, rows.Err()
}

// GetChannelByID retrieves a channel by its ID
func (db *DB) GetChannelByID(channelID uuid.UUID) (*models.Channel, error) {
	var ch models.Channel
	var idStr, serverIDStr string
	var topic sql.NullString
	var categoryID sql.NullString

	err := db.QueryRow(`
		SELECT id, server_id, name, topic, type, position, sort_order, category_id,
			is_nsfw, is_locked, rate_limit_per_user, created_at, updated_at
		FROM channels WHERE id = ?`, channelID.String()).
		Scan(&idStr, &serverIDStr, &ch.Name, &topic, &ch.Type, &ch.Position, &ch.SortOrder,
			&categoryID, &ch.IsNSFW, &ch.IsLocked, &ch.RateLimitPerUser, &ch.CreatedAt, &ch.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("channel not found")
	}
	if err != nil {
		return nil, err
	}

	ch.ID, _ = uuid.Parse(idStr)
	ch.ServerID, _ = uuid.Parse(serverIDStr)
	if topic.Valid {
		ch.Topic = topic.String
	}
	if categoryID.Valid {
		ch.CategoryID, _ = uuid.Parse(categoryID.String)
	}

	return &ch, nil
}

// UpdateChannel updates an existing channel
func (db *DB) UpdateChannel(channel *models.Channel) error {
	var categoryID sql.NullString
	if channel.CategoryID != uuid.Nil {
		categoryID.String = channel.CategoryID.String()
		categoryID.Valid = true
	}

	_, err := db.Exec(`
		UPDATE channels
		SET name = ?, topic = ?, type = ?, category_id = ?, position = ?, sort_order = ?, is_nsfw = ?,
			is_locked = ?, rate_limit_per_user = ?, updated_at = ?
		WHERE id = ?`,
		channel.Name, channel.Topic, channel.Type, categoryID, channel.Position, channel.SortOrder, channel.IsNSFW,
		channel.IsLocked, channel.RateLimitPerUser, time.Now(), channel.ID.String())

	return err
}

// DeleteChannel deletes a channel and all its messages
func (db *DB) DeleteChannel(channelID uuid.UUID) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Find all child channels (channels with category_id = channelID)
	rows, err := tx.Query(`SELECT id FROM channels WHERE category_id = ?`, channelID.String())
	if err != nil {
		return err
	}
	var childIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		childIDs = append(childIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}

	// Delete messages for all children
	for _, childID := range childIDs {
		_, err = tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, childID)
		if err != nil {
			return err
		}
	}

	// Delete child channels
	for _, childID := range childIDs {
		_, err = tx.Exec(`DELETE FROM channels WHERE id = ?`, childID)
		if err != nil {
			return err
		}
	}

	// Delete messages for the channel itself
	_, err = tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, channelID.String())
	if err != nil {
		return err
	}

	// Delete the channel itself
	_, err = tx.Exec(`DELETE FROM channels WHERE id = ?`, channelID.String())
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetChildChannelIDs returns all channels that have the given category as their parent.
func (db *DB) GetChildChannelIDs(categoryID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := db.Query(`SELECT id FROM channels WHERE category_id = ?`, categoryID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			return nil, err
		}
		id, _ := uuid.Parse(idStr)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// GetServerMember retrieves a server member relationship
func (db *DB) GetServerMember(serverID, userID uuid.UUID) (*models.ServerMember, error) {
	var member models.ServerMember
	var serverIDStr, userIDStr string
	var nickname, customTitle sql.NullString

	err := db.QueryRow(`
		SELECT server_id, user_id, nickname, custom_title, joined_at, is_muted, is_deafened,
		       COALESCE(is_banned, 0), COALESCE(kick_count, 0)
		FROM server_members WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String()).
		Scan(&serverIDStr, &userIDStr, &nickname, &customTitle, &member.JoinedAt, &member.IsMuted, &member.IsDeafened, &member.IsBanned, &member.KickCount)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("member not found")
	}
	if err != nil {
		return nil, err
	}

	member.ServerID, _ = uuid.Parse(serverIDStr)
	member.UserID, _ = uuid.Parse(userIDStr)
	if nickname.Valid {
		member.Nickname = nickname.String
	}
	if customTitle.Valid {
		member.CustomTitle = customTitle.String
	}

	debugf("GetServerMember: userID=%s, IsBanned=%v, KickCount=%d", member.UserID, member.IsBanned, member.KickCount)

	// Query roles
	rows, err := db.Query(`
		SELECT role_id FROM member_roles
		WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	member.RoleIDs = []uuid.UUID{}
	for rows.Next() {
		var roleIDStr string
		if err := rows.Scan(&roleIDStr); err != nil {
			return nil, err
		}
		roleID, _ := uuid.Parse(roleIDStr)
		member.RoleIDs = append(member.RoleIDs, roleID)
	}

	return &member, nil
}

// --- Message Operations ---

// CreateMessage inserts a new message
func (db *DB) CreateMessage(msg *models.Message) error {
	var replyToID sql.NullString
	if msg.ReplyToID != nil {
		replyToID.String = msg.ReplyToID.String()
		replyToID.Valid = true
	}

	var recipientID sql.NullString
	if msg.RecipientID != nil {
		recipientID.String = msg.RecipientID.String()
		recipientID.Valid = true
	}

	_, err := db.Exec(`
		INSERT INTO messages (id, channel_id, author_id, content, type, created_at, is_pinned, is_whisper, recipient_id, reply_to_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID.String(), msg.ChannelID.String(), msg.AuthorID.String(),
		msg.Content, msg.Type, msg.CreatedAt, msg.IsPinned, msg.IsWhisper, recipientID, replyToID)
	if err != nil {
		return err
	}

	// Insert mentions
	for _, userID := range msg.Mentions {
		_, err = db.Exec(`INSERT OR IGNORE INTO message_mentions (message_id, user_id) VALUES (?, ?)`,
			msg.ID.String(), userID.String())
		if err != nil {
			return err
		}
	}

	return nil
}

// GetChannelMessages retrieves messages for a channel with pagination
// GetMessage retrieves a single message by ID
func (db *DB) GetMessage(messageID uuid.UUID) (*models.Message, error) {
	query := `
		SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, reply_to_id
		FROM messages
		WHERE id = ?`

	msg := &models.Message{}
	var idStr, channelIDStr, authorIDStr string
	var editedAt sql.NullTime
	var replyToID sql.NullString

	err := db.QueryRow(query, messageID.String()).Scan(
		&idStr, &channelIDStr, &authorIDStr, &msg.Content,
		&msg.Type, &msg.CreatedAt, &editedAt, &msg.IsPinned, &replyToID)
	if err != nil {
		return nil, err
	}

	msg.ID = uuid.MustParse(idStr)
	msg.ChannelID = uuid.MustParse(channelIDStr)
	msg.AuthorID = uuid.MustParse(authorIDStr)
	if editedAt.Valid {
		msg.EditedAt = &editedAt.Time
	}
	if replyToID.Valid {
		replyID := uuid.MustParse(replyToID.String)
		msg.ReplyToID = &replyID
	}

	return msg, nil
}

// GetPinnedMessages retrieves all pinned messages for a channel
func (db *DB) GetPinnedMessages(channelID uuid.UUID) ([]*models.Message, error) {
	query := `
		SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, reply_to_id
		FROM messages
		WHERE channel_id = ? AND is_pinned = 1
		ORDER BY created_at DESC`

	rows, err := db.Query(query, channelID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var idStr, channelIDStr, authorIDStr string
		var editedAt sql.NullTime
		var replyToID sql.NullString

		err := rows.Scan(&idStr, &channelIDStr, &authorIDStr, &msg.Content,
			&msg.Type, &msg.CreatedAt, &editedAt, &msg.IsPinned, &replyToID)
		if err != nil {
			return nil, err
		}

		msg.ID = uuid.MustParse(idStr)
		msg.ChannelID = uuid.MustParse(channelIDStr)
		msg.AuthorID = uuid.MustParse(authorIDStr)
		if editedAt.Valid {
			msg.EditedAt = &editedAt.Time
		}
		if replyToID.Valid {
			replyID := uuid.MustParse(replyToID.String)
			msg.ReplyToID = &replyID
		}

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// SetMessagePinned updates the is_pinned status of a message
func (db *DB) SetMessagePinned(messageID uuid.UUID, pinned bool) error {
	query := `UPDATE messages SET is_pinned = ? WHERE id = ?`
	pinnedInt := 0
	if pinned {
		pinnedInt = 1
	}
	_, err := db.Exec(query, pinnedInt, messageID.String())
	return err
}

// UpdateMessage updates the content and edited_at timestamp of a message
func (db *DB) UpdateMessage(msg *models.Message) error {
	query := `UPDATE messages SET content = ?, edited_at = ? WHERE id = ?`
	_, err := db.Exec(query, msg.Content, msg.EditedAt, msg.ID.String())
	return err
}

// SoftDeleteMessage marks a message as deleted (soft delete)
func (db *DB) SoftDeleteMessage(messageID, deletedBy uuid.UUID) error {
	query := `UPDATE messages SET is_deleted = 1, deleted_at = ?, deleted_by = ? WHERE id = ?`
	_, err := db.Exec(query, time.Now(), deletedBy.String(), messageID.String())
	return err
}

func (db *DB) GetChannelMessages(channelID uuid.UUID, limit int, before *uuid.UUID, userID uuid.UUID) ([]*models.Message, error) {
	var query string
	var args []interface{}

	// Filter: show regular messages to everyone, whispers only to sender/recipient, hide deleted messages
	whisperFilter := "(is_whisper = 0 OR author_id = ? OR recipient_id = ?)"
	deletedFilter := "(is_deleted = 0 OR is_deleted IS NULL)"

	if before != nil {
		query = `
			SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, is_whisper, recipient_id, reply_to_id
			FROM messages
			WHERE channel_id = ? AND created_at < (SELECT created_at FROM messages WHERE id = ?) AND ` + whisperFilter + ` AND ` + deletedFilter + `
			ORDER BY created_at DESC
			LIMIT ?`
		args = []interface{}{channelID.String(), before.String(), userID.String(), userID.String(), limit}
	} else {
		query = `
			SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, is_whisper, recipient_id, reply_to_id
			FROM messages
			WHERE channel_id = ? AND ` + whisperFilter + ` AND ` + deletedFilter + `
			ORDER BY created_at DESC
			LIMIT ?`
		args = []interface{}{channelID.String(), userID.String(), userID.String(), limit}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.Message
	for rows.Next() {
		msg := &models.Message{}
		var idStr, channelIDStr, authorIDStr string
		var editedAt sql.NullTime
		var recipientID sql.NullString
		var replyToID sql.NullString

		err := rows.Scan(&idStr, &channelIDStr, &authorIDStr, &msg.Content,
			&msg.Type, &msg.CreatedAt, &editedAt, &msg.IsPinned, &msg.IsWhisper, &recipientID, &replyToID)
		if err != nil {
			return nil, err
		}

		msg.ID, _ = uuid.Parse(idStr)
		msg.ChannelID, _ = uuid.Parse(channelIDStr)
		msg.AuthorID, _ = uuid.Parse(authorIDStr)
		if editedAt.Valid {
			msg.EditedAt = &editedAt.Time
		}
		if recipientID.Valid {
			id, _ := uuid.Parse(recipientID.String)
			msg.RecipientID = &id
		}
		if replyToID.Valid {
			id, _ := uuid.Parse(replyToID.String)
			msg.ReplyToID = &id
		}

		messages = append(messages, msg)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// The query fetches the most-recent N rows with DESC order (needed for
	// correct LIMIT behaviour). Reverse to chronological order (oldest first)
	// so the client can simply append new real-time messages to the end.
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// --- Message Retention Policy Operations ---

// GetRetentionPolicy returns the effective retention policy for a channel.
// If a channel-specific policy exists, it returns that; otherwise returns the server default.
func (db *DB) GetRetentionPolicy(serverID, channelID uuid.UUID) (*models.MessageRetentionPolicy, error) {
	// Try channel-specific policy first
	policy, err := db.GetRetentionPolicyDirect(serverID, &channelID)
	if err == nil && policy != nil {
		return policy, nil
	}

	// Fall back to server default (channel_id = NULL)
	return db.GetRetentionPolicyDirect(serverID, nil)
}

// GetRetentionPolicyDirect fetches an exact policy (server default or channel override)
func (db *DB) GetRetentionPolicyDirect(serverID uuid.UUID, channelID *uuid.UUID) (*models.MessageRetentionPolicy, error) {
	query := `
		SELECT id, server_id, channel_id, time_retention_days, system_time_retention_days,
		       max_message_count, preserve_pinned, created_at, updated_at, created_by
		FROM message_retention_policies
		WHERE server_id = ? AND `

	var args []interface{}
	args = append(args, serverID.String())

	if channelID == nil {
		query += "channel_id IS NULL"
	} else {
		query += "channel_id = ?"
		args = append(args, channelID.String())
	}

	policy := &models.MessageRetentionPolicy{}
	var idStr, serverIDStr string
	var channelIDStr sql.NullString
	var timeRetentionDays, systemTimeRetentionDays, maxMessageCount sql.NullInt64
	var preservePinned int
	var createdByStr sql.NullString

	err := db.QueryRow(query, args...).Scan(
		&idStr, &serverIDStr, &channelIDStr,
		&timeRetentionDays, &systemTimeRetentionDays, &maxMessageCount,
		&preservePinned, &policy.CreatedAt, &policy.UpdatedAt, &createdByStr,
	)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	policy.ID, _ = uuid.Parse(idStr)
	policy.ServerID, _ = uuid.Parse(serverIDStr)

	if channelIDStr.Valid {
		cID, _ := uuid.Parse(channelIDStr.String)
		policy.ChannelID = &cID
	}

	if timeRetentionDays.Valid {
		days := int(timeRetentionDays.Int64)
		policy.TimeRetentionDays = &days
	}

	if systemTimeRetentionDays.Valid {
		days := int(systemTimeRetentionDays.Int64)
		policy.SystemTimeRetentionDays = &days
	}

	if maxMessageCount.Valid {
		count := int(maxMessageCount.Int64)
		policy.MaxMessageCount = &count
	}

	policy.PreservePinned = preservePinned == 1

	if createdByStr.Valid {
		createdBy, _ := uuid.Parse(createdByStr.String)
		policy.CreatedBy = createdBy
	}

	return policy, nil
}

// UpsertRetentionPolicy creates or updates a retention policy
func (db *DB) UpsertRetentionPolicy(policy *models.MessageRetentionPolicy) error {
	var channelIDStr sql.NullString
	if policy.ChannelID != nil {
		channelIDStr.Valid = true
		channelIDStr.String = policy.ChannelID.String()
	}

	var timeRetentionDays, systemTimeRetentionDays, maxMessageCount sql.NullInt64
	if policy.TimeRetentionDays != nil {
		timeRetentionDays.Valid = true
		timeRetentionDays.Int64 = int64(*policy.TimeRetentionDays)
	}
	if policy.SystemTimeRetentionDays != nil {
		systemTimeRetentionDays.Valid = true
		systemTimeRetentionDays.Int64 = int64(*policy.SystemTimeRetentionDays)
	}
	if policy.MaxMessageCount != nil {
		maxMessageCount.Valid = true
		maxMessageCount.Int64 = int64(*policy.MaxMessageCount)
	}

	preservePinned := 0
	if policy.PreservePinned {
		preservePinned = 1
	}

	// SQLite treats NULL != NULL, so ON CONFLICT doesn't work for NULL channel_id
	// We need to explicitly handle the server default (NULL channel_id) case
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete existing policy (if any) for this server/channel combination
	if policy.ChannelID != nil {
		_, err = tx.Exec(`DELETE FROM message_retention_policies WHERE server_id = ? AND channel_id = ?`,
			policy.ServerID.String(), channelIDStr.String)
	} else {
		_, err = tx.Exec(`DELETE FROM message_retention_policies WHERE server_id = ? AND channel_id IS NULL`,
			policy.ServerID.String())
	}
	if err != nil {
		return err
	}

	// Insert new policy
	_, err = tx.Exec(`
		INSERT INTO message_retention_policies (
			id, server_id, channel_id, time_retention_days, system_time_retention_days,
			max_message_count, preserve_pinned, created_at, updated_at, created_by
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		policy.ID.String(), policy.ServerID.String(), channelIDStr,
		timeRetentionDays, systemTimeRetentionDays, maxMessageCount,
		preservePinned, policy.CreatedAt, policy.UpdatedAt, policy.CreatedBy.String(),
	)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// DeleteRetentionPolicy removes a channel-specific override
func (db *DB) DeleteRetentionPolicy(serverID, channelID uuid.UUID) error {
	_, err := db.Exec(`
		DELETE FROM message_retention_policies
		WHERE server_id = ? AND channel_id = ?`,
		serverID.String(), channelID.String(),
	)
	return err
}

// ListChannelOverrides returns all channel-specific retention policies for a server
func (db *DB) ListChannelOverrides(serverID uuid.UUID) ([]*models.MessageRetentionPolicy, error) {
	rows, err := db.Query(`
		SELECT id, server_id, channel_id, time_retention_days, system_time_retention_days,
		       max_message_count, preserve_pinned, created_at, updated_at, created_by
		FROM message_retention_policies
		WHERE server_id = ? AND channel_id IS NOT NULL
		ORDER BY created_at DESC`,
		serverID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var policies []*models.MessageRetentionPolicy
	for rows.Next() {
		policy := &models.MessageRetentionPolicy{}
		var idStr, serverIDStr string
		var channelIDStr sql.NullString
		var timeRetentionDays, systemTimeRetentionDays, maxMessageCount sql.NullInt64
		var preservePinned int
		var createdByStr sql.NullString

		err := rows.Scan(
			&idStr, &serverIDStr, &channelIDStr,
			&timeRetentionDays, &systemTimeRetentionDays, &maxMessageCount,
			&preservePinned, &policy.CreatedAt, &policy.UpdatedAt, &createdByStr,
		)
		if err != nil {
			return nil, err
		}

		policy.ID, _ = uuid.Parse(idStr)
		policy.ServerID, _ = uuid.Parse(serverIDStr)

		if channelIDStr.Valid {
			cID, _ := uuid.Parse(channelIDStr.String)
			policy.ChannelID = &cID
		}

		if timeRetentionDays.Valid {
			days := int(timeRetentionDays.Int64)
			policy.TimeRetentionDays = &days
		}

		if systemTimeRetentionDays.Valid {
			days := int(systemTimeRetentionDays.Int64)
			policy.SystemTimeRetentionDays = &days
		}

		if maxMessageCount.Valid {
			count := int(maxMessageCount.Int64)
			policy.MaxMessageCount = &count
		}

		policy.PreservePinned = preservePinned == 1

		if createdByStr.Valid {
			createdBy, _ := uuid.Parse(createdByStr.String)
			policy.CreatedBy = createdBy
		}

		policies = append(policies, policy)
	}

	return policies, rows.Err()
}

// PruneMessagesByAge deletes messages older than the specified time
func (db *DB) PruneMessagesByAge(channelID uuid.UUID, olderThan time.Time, preservePinned bool, systemOnly bool) (int, error) {
	query := `DELETE FROM messages WHERE channel_id = ? AND created_at < ?`
	args := []interface{}{channelID.String(), olderThan}

	if preservePinned {
		query += " AND is_pinned = 0"
	}

	if systemOnly {
		// MessageTypeDefault = 0, so system messages have type != 0
		query += " AND type != 0"
	}

	result, err := db.Exec(query, args...)
	if err != nil {
		return 0, err
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// PruneMessagesByCount keeps only the last N messages in a channel
func (db *DB) PruneMessagesByCount(channelID uuid.UUID, keepLast int, preservePinned bool, systemOnly bool) (int, error) {
	// Find the timestamp of the Nth newest message
	query := `
		SELECT created_at FROM messages
		WHERE channel_id = ?`
	args := []interface{}{channelID.String()}

	if preservePinned {
		query += " AND is_pinned = 0"
	}

	if systemOnly {
		query += " AND type != 0"
	}

	query += " ORDER BY created_at DESC LIMIT 1 OFFSET ?"
	args = append(args, keepLast)

	var cutoffTime time.Time
	err := db.QueryRow(query, args...).Scan(&cutoffTime)
	if err == sql.ErrNoRows {
		// Fewer than N messages exist, nothing to prune
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	// Delete everything older than the cutoff
	deleteQuery := `DELETE FROM messages WHERE channel_id = ? AND created_at < ?`
	deleteArgs := []interface{}{channelID.String(), cutoffTime}

	if preservePinned {
		deleteQuery += " AND is_pinned = 0"
	}

	if systemOnly {
		deleteQuery += " AND type != 0"
	}

	result, err := db.Exec(deleteQuery, deleteArgs...)
	if err != nil {
		return 0, err
	}

	affected, _ := result.RowsAffected()
	return int(affected), nil
}

// PruneChannelMessages applies the retention policy to a single channel
func (db *DB) PruneChannelMessages(serverID, channelID uuid.UUID) (*models.PruneStats, error) {
	start := time.Now()
	stats := &models.PruneStats{
		ChannelID: channelID,
	}

	// Get effective policy
	policy, err := db.GetRetentionPolicy(serverID, channelID)
	if err != nil || policy == nil {
		// No policy configured, skip pruning
		return stats, nil
	}

	// Pass 1: Time-based pruning for regular messages
	if policy.TimeRetentionDays != nil && *policy.TimeRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -*policy.TimeRetentionDays)
		deleted, err := db.PruneMessagesByAge(channelID, cutoff, policy.PreservePinned, false)
		if err != nil {
			return stats, err
		}
		stats.TimeBasedDeleted += deleted
	}

	// Pass 1.5: Time-based pruning for system messages (separate retention)
	if policy.SystemTimeRetentionDays != nil && *policy.SystemTimeRetentionDays > 0 {
		cutoff := time.Now().AddDate(0, 0, -*policy.SystemTimeRetentionDays)
		deleted, err := db.PruneMessagesByAge(channelID, cutoff, policy.PreservePinned, true)
		if err != nil {
			return stats, err
		}
		stats.TimeBasedDeleted += deleted
	}

	// Pass 2: Count-based pruning (operates on survivors from time-based)
	if policy.MaxMessageCount != nil && *policy.MaxMessageCount > 0 {
		deleted, err := db.PruneMessagesByCount(channelID, *policy.MaxMessageCount, policy.PreservePinned, false)
		if err != nil {
			return stats, err
		}
		stats.CountBasedDeleted = deleted
	}

	stats.TotalDeleted = stats.TimeBasedDeleted + stats.CountBasedDeleted
	stats.DurationMs = time.Since(start).Milliseconds()

	return stats, nil
}

// PruneServerMessages prunes all channels in a server according to their policies
func (db *DB) PruneServerMessages(serverID uuid.UUID) (map[uuid.UUID]*models.PruneStats, error) {
	// Get all channels for this server
	rows, err := db.Query(`
		SELECT id FROM channels WHERE server_id = ? AND type = 0`,
		serverID.String(),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var channelIDs []uuid.UUID
	for rows.Next() {
		var idStr string
		if err := rows.Scan(&idStr); err != nil {
			return nil, err
		}
		id, _ := uuid.Parse(idStr)
		channelIDs = append(channelIDs, id)
	}

	// Prune each channel
	results := make(map[uuid.UUID]*models.PruneStats)
	for _, channelID := range channelIDs {
		stats, err := db.PruneChannelMessages(serverID, channelID)
		if err != nil {
			log.Printf("Failed to prune channel %s: %v", channelID, err)
			continue
		}
		if stats.TotalDeleted > 0 {
			results[channelID] = stats
		}
	}

	return results, nil
}

// RecordPruneHistory saves a pruning operation to the audit log
func (db *DB) RecordPruneHistory(history *models.MessagePruneHistory) error {
	var channelIDStr, triggeredByStr sql.NullString

	if history.ChannelID != nil {
		channelIDStr.Valid = true
		channelIDStr.String = history.ChannelID.String()
	}

	if history.TriggeredBy != nil {
		triggeredByStr.Valid = true
		triggeredByStr.String = history.TriggeredBy.String()
	}

	_, err := db.Exec(`
		INSERT INTO message_prune_history (
			id, server_id, channel_id, messages_deleted, time_based_count,
			count_based_count, trigger_type, triggered_by, executed_at, duration_ms
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		history.ID.String(), history.ServerID.String(), channelIDStr,
		history.MessagesDeleted, history.TimeBasedCount, history.CountBasedCount,
		history.TriggerType, triggeredByStr, history.ExecutedAt, history.DurationMs,
	)

	return err
}

// GetPruneHistory retrieves recent pruning operations for a server
func (db *DB) GetPruneHistory(serverID uuid.UUID, limit int) ([]*models.MessagePruneHistory, error) {
	rows, err := db.Query(`
		SELECT id, server_id, channel_id, messages_deleted, time_based_count,
		       count_based_count, trigger_type, triggered_by, executed_at, duration_ms
		FROM message_prune_history
		WHERE server_id = ?
		ORDER BY executed_at DESC
		LIMIT ?`,
		serverID.String(), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []*models.MessagePruneHistory
	for rows.Next() {
		h := &models.MessagePruneHistory{}
		var idStr, serverIDStr string
		var channelIDStr, triggeredByStr sql.NullString

		err := rows.Scan(
			&idStr, &serverIDStr, &channelIDStr,
			&h.MessagesDeleted, &h.TimeBasedCount, &h.CountBasedCount,
			&h.TriggerType, &triggeredByStr, &h.ExecutedAt, &h.DurationMs,
		)
		if err != nil {
			return nil, err
		}

		h.ID, _ = uuid.Parse(idStr)
		h.ServerID, _ = uuid.Parse(serverIDStr)

		if channelIDStr.Valid {
			cID, _ := uuid.Parse(channelIDStr.String)
			h.ChannelID = &cID
		}

		if triggeredByStr.Valid {
			tID, _ := uuid.Parse(triggeredByStr.String)
			h.TriggeredBy = &tID
		}

		history = append(history, h)
	}

	return history, rows.Err()
}

// --- Member Operations ---

// AddServerMember adds a user to a server
func (db *DB) AddServerMember(member *models.ServerMember) error {
	_, err := db.Exec(`
		INSERT INTO server_members (user_id, server_id, nickname, joined_at, is_muted, is_deafened)
		VALUES (?, ?, ?, ?, ?, ?)`,
		member.UserID.String(), member.ServerID.String(), member.Nickname,
		member.JoinedAt, member.IsMuted, member.IsDeafened)
	return err
}

// RemoveServerMember removes a user from a server
func (db *DB) RemoveServerMember(userID, serverID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM server_members WHERE user_id = ? AND server_id = ?`,
		userID.String(), serverID.String())
	return err
}

// SetMemberBanned sets the is_banned field for a server member
func (db *DB) SetMemberBanned(userID, serverID uuid.UUID, banned bool) error {
	bannedInt := 0
	if banned {
		bannedInt = 1
	}
	_, err := db.Exec(`UPDATE server_members SET is_banned = ? WHERE user_id = ? AND server_id = ?`,
		bannedInt, userID.String(), serverID.String())
	return err
}

// IncrementMemberKickCount increments the kick_count for a server member
func (db *DB) IncrementMemberKickCount(userID, serverID uuid.UUID) error {
	result, err := db.Exec(`UPDATE server_members SET kick_count = kick_count + 1 WHERE user_id = ? AND server_id = ?`,
		userID.String(), serverID.String())
	if err != nil {
		debugf("IncrementMemberKickCount: ERROR updating kick_count: %v", err)
		return err
	}
	rowsAffected, _ := result.RowsAffected()
	debugf("IncrementMemberKickCount: userID=%s, rows affected=%d", userID, rowsAffected)

	// Read back the new value to verify
	var kickCount int
	err = db.QueryRow(`SELECT kick_count FROM server_members WHERE user_id = ? AND server_id = ?`,
		userID.String(), serverID.String()).Scan(&kickCount)
	if err == nil {
		debugf("IncrementMemberKickCount: New kick_count value in DB: %d", kickCount)
	}

	return nil
}

// GetTotalMemberCount returns the number of distinct non-banned users across all servers.
func (db *DB) GetTotalMemberCount() int {
	var count int
	db.QueryRow(`SELECT COUNT(DISTINCT user_id) FROM server_members WHERE COALESCE(is_banned,0)=0`).Scan(&count)
	return count
}

// GetServerMembers retrieves all members of a server, including their role IDs
func (db *DB) GetServerMembers(serverID uuid.UUID) ([]*models.ServerMember, error) {
	rows, err := db.Query(`
		SELECT user_id, server_id, nickname, custom_title, joined_at, is_muted, is_deafened,
		       COALESCE(is_banned, 0), COALESCE(kick_count, 0)
		FROM server_members WHERE server_id = ?`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*models.ServerMember
	memberIndex := make(map[string]*models.ServerMember)
	for rows.Next() {
		m := &models.ServerMember{}
		var userIDStr, serverIDStr string

		err := rows.Scan(&userIDStr, &serverIDStr, &m.Nickname, &m.CustomTitle, &m.JoinedAt, &m.IsMuted, &m.IsDeafened, &m.IsBanned, &m.KickCount)
		if err != nil {
			return nil, err
		}

		m.UserID, _ = uuid.Parse(userIDStr)
		m.ServerID, _ = uuid.Parse(serverIDStr)
		m.RoleIDs = []uuid.UUID{}

		members = append(members, m)
		memberIndex[userIDStr] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Load all member roles for this server in one query
	roleRows, err := db.Query(`
		SELECT user_id, role_id FROM member_roles WHERE server_id = ?`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer roleRows.Close()

	for roleRows.Next() {
		var userIDStr, roleIDStr string
		if err := roleRows.Scan(&userIDStr, &roleIDStr); err != nil {
			return nil, err
		}
		if m, ok := memberIndex[userIDStr]; ok {
			roleID, _ := uuid.Parse(roleIDStr)
			m.RoleIDs = append(m.RoleIDs, roleID)
		}
	}

	return members, roleRows.Err()
}

// GetUsersByIDs retrieves multiple users by their IDs in a single query
func (db *DB) GetUsersByIDs(ids []uuid.UUID) ([]*models.User, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	// Build placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id.String()
	}

	query := fmt.Sprintf(`
		SELECT id, username, discriminator, display_name, email, avatar_hash,
			status, status_text, created_at, updated_at, last_seen_at, is_bot
		FROM users WHERE id IN (%s)`, strings.Join(placeholders, ","))

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.User
	for rows.Next() {
		user := &models.User{}
		var idStr string
		var displayName, avatarHash, statusText sql.NullString
		var lastSeenAt sql.NullTime

		err := rows.Scan(&idStr, &user.Username, &user.Discriminator, &displayName,
			&user.Email, &avatarHash, &user.Status, &statusText,
			&user.CreatedAt, &user.UpdatedAt, &lastSeenAt, &user.IsBot)
		if err != nil {
			return nil, err
		}

		user.ID, _ = uuid.Parse(idStr)
		if displayName.Valid {
			user.DisplayName = displayName.String
		}
		if avatarHash.Valid {
			user.AvatarHash = avatarHash.String
		}
		if statusText.Valid {
			user.StatusText = statusText.String
		}
		if lastSeenAt.Valid {
			user.LastSeenAt = lastSeenAt.Time
		}
		users = append(users, user)
	}

	return users, rows.Err()
}

// AddMemberRole assigns a role to a server member
func (db *DB) AddMemberRole(userID, serverID, roleID uuid.UUID) error {
	_, err := db.Exec(`
		INSERT INTO member_roles (user_id, server_id, role_id)
		VALUES (?, ?, ?)
		ON CONFLICT DO NOTHING`,
		userID.String(), serverID.String(), roleID.String())
	return err
}

// RemoveMemberRole removes a role from a server member
func (db *DB) RemoveMemberRole(userID, serverID, roleID uuid.UUID) error {
	_, err := db.Exec(`
		DELETE FROM member_roles
		WHERE user_id = ? AND server_id = ? AND role_id = ?`,
		userID.String(), serverID.String(), roleID.String())
	return err
}

// --- Role Operations ---

// CreateRole inserts a new role
func (db *DB) CreateRole(role *models.Role) error {
	_, err := db.Exec(`
		INSERT INTO roles (id, server_id, name, color, permissions, position, display_order,
			is_hoisted, is_mentionable, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role.ID.String(), role.ServerID.String(), role.Name, role.Color,
		int64(role.Permissions), role.Position, role.DisplayOrder, role.IsHoisted, role.IsMentionable,
		role.IsDefault, role.CreatedAt, role.UpdatedAt)
	return err
}

// UpdateRole updates an existing role
func (db *DB) UpdateRole(role *models.Role) error {
	_, err := db.Exec(`
		UPDATE roles
		SET name = ?, permissions = ?, color = ?, display_order = ?,
			is_hoisted = ?, is_mentionable = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND server_id = ?`,
		role.Name, int64(role.Permissions), role.Color, role.DisplayOrder,
		role.IsHoisted, role.IsMentionable, role.ID.String(), role.ServerID.String())
	return err
}

// DeleteRole deletes a role and removes it from all members
func (db *DB) DeleteRole(roleID, serverID uuid.UUID) error {
	// Begin transaction
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if role is @everyone (IsDefault = true)
	var isDefault bool
	err = tx.QueryRow("SELECT is_default FROM roles WHERE id = ?", roleID.String()).Scan(&isDefault)
	if err != nil {
		return err
	}
	if isDefault {
		return fmt.Errorf("cannot delete @everyone role")
	}

	// Remove role from all members
	_, err = tx.Exec("DELETE FROM member_roles WHERE role_id = ?", roleID.String())
	if err != nil {
		return err
	}

	// Delete role
	_, err = tx.Exec("DELETE FROM roles WHERE id = ? AND server_id = ?", roleID.String(), serverID.String())
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetServerRoles retrieves all roles for a server
func (db *DB) GetServerRoles(serverID uuid.UUID) ([]*models.Role, error) {
	rows, err := db.Query(`
		SELECT id, server_id, name, color, permissions, position, display_order,
			is_hoisted, is_mentionable, is_default, created_at, updated_at
		FROM roles WHERE server_id = ?
		ORDER BY position DESC`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*models.Role
	for rows.Next() {
		r := &models.Role{}
		var idStr, serverIDStr string
		var permInt int64

		err := rows.Scan(&idStr, &serverIDStr, &r.Name, &r.Color, &permInt,
			&r.Position, &r.DisplayOrder, &r.IsHoisted, &r.IsMentionable, &r.IsDefault,
			&r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return nil, err
		}

		r.ID, _ = uuid.Parse(idStr)
		r.ServerID, _ = uuid.Parse(serverIDStr)
		r.Permissions = models.Permission(permInt)

		roles = append(roles, r)
	}

	return roles, rows.Err()
}

// GetRoleByID retrieves a role by its ID
func (db *DB) GetRoleByID(roleID uuid.UUID) (*models.Role, error) {
	var r models.Role
	var idStr, serverIDStr string
	var permInt int64

	err := db.QueryRow(`
		SELECT id, server_id, name, color, permissions, position, display_order,
			is_hoisted, is_mentionable, is_default, created_at, updated_at
		FROM roles WHERE id = ?`, roleID.String()).
		Scan(&idStr, &serverIDStr, &r.Name, &r.Color, &permInt,
			&r.Position, &r.DisplayOrder, &r.IsHoisted, &r.IsMentionable, &r.IsDefault,
			&r.CreatedAt, &r.UpdatedAt)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("role not found")
	}
	if err != nil {
		return nil, err
	}

	r.ID, _ = uuid.Parse(idStr)
	r.ServerID, _ = uuid.Parse(serverIDStr)
	r.Permissions = models.Permission(permInt)

	return &r, nil
}

// --- Session Operations ---

// CreateSession creates a new authentication session
func (db *DB) CreateSession(userID uuid.UUID, tokenHash, ipAddress, userAgent string, expiresAt time.Time) (string, error) {
	sessionID := uuid.New().String()
	_, err := db.Exec(`
		INSERT INTO sessions (id, user_id, token_hash, created_at, expires_at, ip_address, user_agent)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, userID.String(), tokenHash, time.Now(), expiresAt, ipAddress, userAgent)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// GetSessionByToken retrieves a session by token hash
func (db *DB) GetSessionByToken(tokenHash string) (uuid.UUID, error) {
	var userIDStr string
	var expiresAt time.Time

	err := db.QueryRow(`
		SELECT user_id, expires_at FROM sessions 
		WHERE token_hash = ? AND expires_at > ?`,
		tokenHash, time.Now()).Scan(&userIDStr, &expiresAt)
	if err != nil {
		return uuid.Nil, err
	}

	// Update last used
	db.Exec(`UPDATE sessions SET last_used_at = ? WHERE token_hash = ?`, time.Now(), tokenHash)

	userID, _ := uuid.Parse(userIDStr)
	return userID, nil
}

// DeleteSession removes a session
func (db *DB) DeleteSession(tokenHash string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteUserSessions removes all sessions for a user
func (db *DB) DeleteUserSessions(userID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID.String())
	return err
}

// --- Server Initialization ---

// EnsureDefaultServer ensures a default server exists, creating it if necessary
// Returns the default server and its @everyone role
func (db *DB) EnsureDefaultServer(serverName string) (*models.Server, *models.Role, error) {
	// Try to find existing default server (first server created)
	var serverIDStr string
	err := db.QueryRow(`SELECT id FROM servers ORDER BY created_at ASC LIMIT 1`).Scan(&serverIDStr)

	if err == sql.ErrNoRows {
		// No server exists, create default server
		now := time.Now()

		// Create a system user to be the owner (using a fixed UUID for consistency)
		systemUserID := uuid.MustParse("00000000-0000-0000-0000-000000000001")

		// Check if system user exists, create if not
		_, err := db.GetUserByID(systemUserID)
		if err != nil {
			// System user doesn't exist, create it
			systemUser := &models.User{
				ID:            systemUserID,
				Username:      "system",
				Discriminator: "0000",
				DisplayName:   "System",
				Email:         "system@concord.local",
				Status:        models.StatusOffline,
				CreatedAt:     now,
				UpdatedAt:     now,
			}
			// Use empty password hash since system user can't log in
			if err := db.CreateUser(systemUser, ""); err != nil {
				return nil, nil, fmt.Errorf("failed to create system user: %w", err)
			}
		}

		// Create default server
		server := models.NewServer(serverName, systemUserID)
		if err := db.CreateServer(server); err != nil {
			return nil, nil, fmt.Errorf("failed to create default server: %w", err)
		}

		// Create @everyone role
		everyoneRole := models.NewEveryoneRole(server.ID)
		if err := db.CreateRole(everyoneRole); err != nil {
			return nil, nil, fmt.Errorf("failed to create @everyone role: %w", err)
		}

		// Create default channel
		generalChannel := models.NewTextChannel(server.ID, "general")
		if err := db.CreateChannel(generalChannel); err != nil {
			return nil, nil, fmt.Errorf("failed to create default channel: %w", err)
		}

		// Set as default channel (update directly in database)
		_, err = db.Exec(`UPDATE servers SET default_channel_id = ?, updated_at = ? WHERE id = ?`,
			generalChannel.ID.String(), time.Now(), server.ID.String())
		if err != nil {
			return nil, nil, fmt.Errorf("failed to update server default channel: %w", err)
		}
		server.DefaultChannelID = generalChannel.ID

		return server, everyoneRole, nil
	} else if err != nil {
		return nil, nil, fmt.Errorf("failed to query servers: %w", err)
	}

	// Server exists, retrieve it
	serverID, _ := uuid.Parse(serverIDStr)
	server, err := db.GetServerByID(serverID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get server: %w", err)
	}

	// Get @everyone role
	roles, err := db.GetServerRoles(serverID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get server roles: %w", err)
	}

	var everyoneRole *models.Role
	for _, role := range roles {
		if role.IsDefault {
			everyoneRole = role
			break
		}
	}

	if everyoneRole == nil {
		// @everyone role missing, create it
		everyoneRole = models.NewEveryoneRole(serverID)
		if err := db.CreateRole(everyoneRole); err != nil {
			return nil, nil, fmt.Errorf("failed to create @everyone role: %w", err)
		}
	}

	return server, everyoneRole, nil
}

// CountRealUsers returns the number of non-system users registered on this server.
func (db *DB) CountRealUsers() (int, error) {
	systemUserID := "00000000-0000-0000-0000-000000000001"
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE id != ?`, systemUserID).Scan(&count)
	return count, err
}

// UpdateServerOwner updates the owner_id of a server.
func (db *DB) UpdateServerOwner(serverID, ownerID uuid.UUID) error {
	_, err := db.Exec(`UPDATE servers SET owner_id = ?, updated_at = ? WHERE id = ?`,
		ownerID.String(), time.Now(), serverID.String())
	return err
}

// GetOrCreateAdminRole returns the existing "Admin" role for serverID, creating it if absent.
func (db *DB) GetOrCreateAdminRole(serverID uuid.UUID) (*models.Role, error) {
	roles, err := db.GetServerRoles(serverID)
	if err != nil {
		return nil, err
	}
	for _, r := range roles {
		if r.Name == "Admin" && r.Permissions&models.PermissionAdministrator != 0 {
			return r, nil
		}
	}
	// Create a new Admin role with gold color (#FFD700 = 16766720)
	now := time.Now()
	adminRole := &models.Role{
		ID:            uuid.New(),
		ServerID:      serverID,
		Name:          "Admin",
		Color:         0xFFD700,
		Permissions:   models.PermissionsAdmin,
		Position:      100,
		IsHoisted:     true,
		IsMentionable: true,
		IsDefault:     false,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := db.CreateRole(adminRole); err != nil {
		return nil, fmt.Errorf("failed to create admin role: %w", err)
	}
	return adminRole, nil
}

// --- Moderation ---

// AddBan adds a ban record for a user on a server.
func (db *DB) AddBan(serverID, userID, bannedByID uuid.UUID, reason string) error {
	_, err := db.Exec(`
		INSERT INTO bans (server_id, user_id, reason, banned_by, banned_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(server_id, user_id) DO UPDATE SET reason=excluded.reason, banned_by=excluded.banned_by, banned_at=excluded.banned_at`,
		serverID.String(), userID.String(), reason, bannedByID.String(), time.Now())
	return err
}

// IsBanned reports whether userID is banned from serverID.
func (db *DB) IsBanned(serverID, userID uuid.UUID) (bool, error) {
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM bans WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String()).Scan(&count)
	return count > 0, err
}

// RemoveBan removes a ban for a user from a server
func (db *DB) RemoveBan(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM bans WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String())
	return err
}

// GetBannedUserByUsername finds a banned user by username on a specific server
func (db *DB) GetBannedUserByUsername(serverID uuid.UUID, username string) (*models.User, error) {
	var userIDStr string
	err := db.QueryRow(`
		SELECT user_id FROM bans WHERE server_id = ? AND user_id IN (
			SELECT id FROM users WHERE LOWER(username) = LOWER(?)
		)`,
		serverID.String(), username).Scan(&userIDStr)

	if err != nil {
		return nil, err
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return nil, err
	}

	return db.GetUserByID(userID)
}

// SetMemberMuted sets the server-mute state for a member.
func (db *DB) SetMemberMuted(serverID, userID uuid.UUID, muted bool) error {
	val := 0
	if muted {
		val = 1
	}
	_, err := db.Exec(`UPDATE server_members SET is_muted = ? WHERE server_id = ? AND user_id = ?`,
		val, serverID.String(), userID.String())
	return err
}

// UpdateServerMemberTitle updates a member's custom title
func (db *DB) UpdateServerMemberTitle(serverID, userID uuid.UUID, title string) error {
	_, err := db.Exec(`
		UPDATE server_members
		SET custom_title = ?
		WHERE server_id = ? AND user_id = ?
	`, title, serverID.String(), userID.String())
	return err
}

// AddTimeout adds a temporary ban (timeout) for a user
func (db *DB) AddTimeout(serverID, userID, channelID, issuedBy uuid.UUID, duration int, reason string) error {
	id := uuid.New()
	now := time.Now()
	expiresAt := now.Add(time.Duration(duration) * time.Minute)

	_, err := db.Exec(`
		INSERT INTO timeouts (id, server_id, user_id, channel_id, reason, duration, issued_by, issued_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), serverID.String(), userID.String(), channelID.String(), reason, duration, issuedBy.String(), now, expiresAt)
	return err
}

// IsTimedOut reports whether userID has an active timeout on serverID
func (db *DB) IsTimedOut(serverID, userID uuid.UUID) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM timeouts
		WHERE server_id = ? AND user_id = ? AND expires_at > ?`,
		serverID.String(), userID.String(), time.Now()).Scan(&count)
	return count > 0, err
}

// RemoveTimeout removes an active timeout for a user
func (db *DB) RemoveTimeout(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM timeouts WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String())
	return err
}

// CleanupExpiredTimeouts removes expired timeouts from the database
func (db *DB) CleanupExpiredTimeouts() ([]struct{ ServerID, UserID, ChannelID uuid.UUID }, error) {
	rows, err := db.Query(`SELECT server_id, user_id, channel_id FROM timeouts WHERE expires_at <= ?`, time.Now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var expired []struct{ ServerID, UserID, ChannelID uuid.UUID }
	for rows.Next() {
		var serverIDStr, userIDStr, channelIDStr string
		if err := rows.Scan(&serverIDStr, &userIDStr, &channelIDStr); err != nil {
			return nil, err
		}
		serverID, _ := uuid.Parse(serverIDStr)
		userID, _ := uuid.Parse(userIDStr)
		channelID, _ := uuid.Parse(channelIDStr)
		expired = append(expired, struct{ ServerID, UserID, ChannelID uuid.UUID }{serverID, userID, channelID})
	}

	// Delete expired timeouts
	_, err = db.Exec(`DELETE FROM timeouts WHERE expires_at <= ?`, time.Now())
	return expired, err
}

// AddMute adds a timed mute for a user
func (db *DB) AddMute(serverID, userID, channelID, issuedBy uuid.UUID, duration int) error {
	id := uuid.New()
	now := time.Now()
	expiresAt := now.Add(time.Duration(duration) * time.Minute)

	debugf("AddMute: userID=%s, issuedBy=%s, duration=%d minutes, expiresAt=%s", userID, issuedBy, duration, expiresAt.Format("15:04:05"))

	_, err := db.Exec(`
		INSERT INTO mutes (id, server_id, user_id, channel_id, muted_by, muted_at, muted_until, reason, duration, issued_by, issued_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), serverID.String(), userID.String(), channelID.String(), issuedBy.String(), now, &expiresAt, "", duration, issuedBy.String(), now, expiresAt)
	return err
}

// GetActiveMute returns the expiry time for an active mute, or nil if not muted
func (db *DB) GetActiveMute(serverID, userID uuid.UUID) (*time.Time, error) {
	var expiresAt time.Time
	err := db.QueryRow(`
		SELECT expires_at FROM mutes
		WHERE server_id = ? AND user_id = ? AND expires_at > ?`,
		serverID.String(), userID.String(), time.Now()).Scan(&expiresAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &expiresAt, nil
}

// RemoveMute removes an active mute for a user
func (db *DB) RemoveMute(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM mutes WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String())
	return err
}

// CleanupExpiredMutes removes expired mutes and returns the users that were unmuted
func (db *DB) CleanupExpiredMutes() ([]struct{ ServerID, UserID, ChannelID uuid.UUID }, error) {
	now := time.Now()
	debugf("CleanupExpiredMutes: Checking for mutes expired before %s", now.Format("15:04:05"))

	rows, err := db.Query(`SELECT server_id, user_id, channel_id FROM mutes WHERE expires_at <= ?`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var unmuted []struct{ ServerID, UserID, ChannelID uuid.UUID }
	for rows.Next() {
		var serverIDStr, userIDStr, channelIDStr string
		if err := rows.Scan(&serverIDStr, &userIDStr, &channelIDStr); err != nil {
			return nil, err
		}
		serverID, _ := uuid.Parse(serverIDStr)
		userID, _ := uuid.Parse(userIDStr)
		channelID, _ := uuid.Parse(channelIDStr)
		unmuted = append(unmuted, struct{ ServerID, UserID, ChannelID uuid.UUID }{serverID, userID, channelID})
		debugf("CleanupExpiredMutes: Found expired mute for userID=%s", userID)
	}

	if len(unmuted) > 0 {
		debugf("CleanupExpiredMutes: Deleting %d expired mutes", len(unmuted))
	}

	// Delete expired mutes
	_, err = db.Exec(`DELETE FROM mutes WHERE expires_at <= ?`, now)
	return unmuted, err
}

// GetRoleByName returns the first role with the given name in a server (case-insensitive).
func (db *DB) GetRoleByName(serverID uuid.UUID, name string) (*models.Role, error) {
	roles, err := db.GetServerRoles(serverID)
	if err != nil {
		return nil, err
	}
	nameLower := strings.ToLower(name)
	for _, r := range roles {
		if strings.ToLower(r.Name) == nameLower {
			return r, nil
		}
	}
	return nil, fmt.Errorf("role %q not found", name)
}

// GetMemberRoles returns all roles assigned to a member.
func (db *DB) GetMemberRoles(serverID, userID uuid.UUID) ([]*models.Role, error) {
	rows, err := db.Query(`
		SELECT r.id, r.server_id, r.name, r.color, r.permissions, r.position,
		       r.is_hoisted, r.is_mentionable, r.is_default, r.created_at, r.updated_at
		FROM roles r
		JOIN member_roles mr ON mr.role_id = r.id
		WHERE mr.server_id = ? AND mr.user_id = ?`,
		serverID.String(), userID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var roles []*models.Role
	for rows.Next() {
		r := &models.Role{}
		var idStr, serverIDStr string
		var permInt int64
		if err := rows.Scan(&idStr, &serverIDStr, &r.Name, &r.Color, &permInt,
			&r.Position, &r.IsHoisted, &r.IsMentionable, &r.IsDefault, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.ID, _ = uuid.Parse(idStr)
		r.ServerID, _ = uuid.Parse(serverIDStr)
		r.Permissions = models.Permission(permInt)
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// EnsureAdminRole grants the Admin role to the user with the given email, and makes
// them the server owner. Safe to call repeatedly.
func (db *DB) EnsureAdminRole(email string) error {
	user, _, err := db.GetUserByEmail(email)
	if err != nil {
		return fmt.Errorf("user not found for email %q: %w", email, err)
	}

	server, _, err := db.EnsureDefaultServer("Concord Server") // Name only used if creating new server
	if err != nil {
		return err
	}

	adminRole, err := db.GetOrCreateAdminRole(server.ID)
	if err != nil {
		return err
	}

	// Assign the role (ignore duplicate errors)
	_ = db.AddMemberRole(user.ID, server.ID, adminRole.ID)

	// Make them the server owner
	return db.UpdateServerOwner(server.ID, user.ID)
}

// CountMessagesSince counts messages in a channel since a given time
func (db *DB) CountMessagesSince(channelID uuid.UUID, since time.Time) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM messages
		WHERE channel_id = ? AND created_at > ?
	`, channelID.String(), since).Scan(&count)
	return count, err
}

// GetMessageCountsByChannel returns message counts per channel for a server in a time window
func (db *DB) GetMessageCountsByChannel(serverID uuid.UUID, since time.Time) (map[uuid.UUID]int, error) {
	rows, err := db.Query(`
		SELECT c.id, COUNT(m.id) as count
		FROM channels c
		LEFT JOIN messages m ON c.id = m.channel_id AND m.created_at > ?
		WHERE c.server_id = ? AND c.type = 0
		GROUP BY c.id
	`, since, serverID.String())

	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := make(map[uuid.UUID]int)
	for rows.Next() {
		var chID string
		var count int
		if err := rows.Scan(&chID, &count); err != nil {
			return nil, err
		}
		id, _ := uuid.Parse(chID)
		counts[id] = count
	}

	return counts, rows.Err()
}

// FixAdminRole repairs the Admin role if it exists with wrong permissions/settings
func (db *DB) FixAdminRole(serverID uuid.UUID) error {
	roles, err := db.GetServerRoles(serverID)
	if err != nil {
		return err
	}
	
	var adminRole *models.Role
	for _, r := range roles {
		if r.Name == "Admin" {
			adminRole = r
			break
		}
	}
	
	if adminRole == nil {
		return fmt.Errorf("no Admin role found to fix")
	}
	
	// Fix the role settings
	// Note: PermissionsAdmin (1<<63) is stored as negative int64 in SQLite due to two's complement
	// but the bit pattern is preserved and converts back correctly when read
	adminPerms := models.PermissionsAdmin
	_, err = db.Exec(`
		UPDATE roles
		SET permissions = ?,
		    is_hoisted = 1,
		    is_mentionable = 1,
		    position = 100,
		    updated_at = ?
		WHERE id = ?`,
		int64(adminPerms),
		time.Now(),
		adminRole.ID.String())

	return err
}

// CleanupDuplicateRoles finds roles with duplicate names and merges them
// Keeps the newest role, transfers all members from old roles to it, then deletes old roles
func (db *DB) CleanupDuplicateRoles(serverID uuid.UUID) error {
	// Find all roles for this server
	rows, err := db.Query(`
		SELECT id, name, created_at
		FROM roles
		WHERE server_id = ?
		ORDER BY name, created_at DESC`, serverID.String())
	if err != nil {
		return err
	}
	defer rows.Close()

	// Group roles by name
	type roleInfo struct {
		ID        uuid.UUID
		CreatedAt time.Time
	}
	rolesByName := make(map[string][]roleInfo)

	for rows.Next() {
		var idStr, name string
		var createdAt time.Time
		if err := rows.Scan(&idStr, &name, &createdAt); err != nil {
			return err
		}
		id, _ := uuid.Parse(idStr)
		rolesByName[name] = append(rolesByName[name], roleInfo{ID: id, CreatedAt: createdAt})
	}

	// Process duplicates
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for name, roles := range rolesByName {
		if len(roles) <= 1 {
			continue // No duplicates
		}

		// Keep the newest role (first in list due to ORDER BY created_at DESC)
		keepRole := roles[0]
		oldRoles := roles[1:]

		fmt.Printf("Found %d duplicate(s) for role '%s', keeping newest (id=%s)\n",
			len(oldRoles), name, keepRole.ID.String()[:8])

		// Transfer all members from old roles to the kept role
		for _, oldRole := range oldRoles {
			// Get all users with this old role
			memberRows, err := tx.Query(`
				SELECT user_id, server_id FROM member_roles WHERE role_id = ?`,
				oldRole.ID.String())
			if err != nil {
				return err
			}

			type memberInfo struct {
				UserID   string
				ServerID string
			}
			var members []memberInfo
			for memberRows.Next() {
				var m memberInfo
				memberRows.Scan(&m.UserID, &m.ServerID)
				members = append(members, m)
			}
			memberRows.Close()

			// Assign them to the kept role (ignore duplicates)
			for _, m := range members {
				_, err := tx.Exec(`
					INSERT OR IGNORE INTO member_roles (user_id, server_id, role_id)
					VALUES (?, ?, ?)`, m.UserID, m.ServerID, keepRole.ID.String())
				if err != nil {
					return err
				}
			}

			// Delete the old role
			_, err = tx.Exec(`DELETE FROM roles WHERE id = ?`, oldRole.ID.String())
			if err != nil {
				return err
			}

			fmt.Printf("  - Merged and deleted old role (id=%s)\n", oldRole.ID.String()[:8])
		}
	}

	return tx.Commit()
}

// CountUsersWithAdminPermission returns the number of users with any admin role
func (db *DB) CountUsersWithAdminPermission(serverID uuid.UUID) (int, error) {
	adminPerm := models.PermissionAdministrator
	var count int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT ur.user_id)
		FROM member_roles ur
		JOIN roles r ON ur.role_id = r.id
		JOIN server_members sm ON ur.user_id = sm.user_id AND r.server_id = sm.server_id
		WHERE r.server_id = ?
		AND (r.permissions & ?) != 0`,
		serverID.String(), int64(adminPerm)).Scan(&count)
	return count, err
}

// CountUsersWithAdminPermissionExcludingUser counts admins excluding a specific user
func (db *DB) CountUsersWithAdminPermissionExcludingUser(serverID, excludeUserID uuid.UUID) (int, error) {
	adminPerm := models.PermissionAdministrator
	var count int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT ur.user_id)
		FROM member_roles ur
		JOIN roles r ON ur.role_id = r.id
		JOIN server_members sm ON ur.user_id = sm.user_id AND r.server_id = sm.server_id
		WHERE r.server_id = ?
		AND ur.user_id != ?
		AND (r.permissions & ?) != 0`,
		serverID.String(), excludeUserID.String(), int64(adminPerm)).Scan(&count)
	return count, err
}

// CountUsersWithAdminPermissionExcludingRole counts admins excluding a specific role
func (db *DB) CountUsersWithAdminPermissionExcludingRole(serverID, excludeRoleID uuid.UUID) (int, error) {
	adminPerm := models.PermissionAdministrator
	var count int
	err := db.QueryRow(`
		SELECT COUNT(DISTINCT ur.user_id)
		FROM member_roles ur
		JOIN roles r ON ur.role_id = r.id
		JOIN server_members sm ON ur.user_id = sm.user_id AND r.server_id = sm.server_id
		WHERE r.server_id = ?
		AND ur.role_id != ?
		AND (r.permissions & ?) != 0`,
		serverID.String(), excludeRoleID.String(), int64(adminPerm)).Scan(&count)
	return count, err
}

// UserHasOtherAdminRoles checks if a user has admin permissions via other roles
func (db *DB) UserHasOtherAdminRoles(userID, excludeRoleID, serverID uuid.UUID) (bool, error) {
	adminPerm := models.PermissionAdministrator
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*)
		FROM member_roles ur
		JOIN roles r ON ur.role_id = r.id
		WHERE ur.user_id = ?
		AND r.server_id = ?
		AND ur.role_id != ?
		AND (r.permissions & ?) != 0`,
		userID.String(), serverID.String(), excludeRoleID.String(),
		int64(adminPerm)).Scan(&count)
	return count > 0, err
}

// CanDeleteRole checks if a role can be safely deleted without leaving zero admins
func (db *DB) CanDeleteRole(roleID, serverID uuid.UUID) error {
	// Get the role
	role, err := db.GetRoleByID(roleID)
	if err != nil {
		return err
	}

	// If it's not an admin role, deletion is safe
	if !role.HasPermission(models.PermissionAdministrator) {
		return nil
	}

	// Count other admins (excluding this role)
	otherAdminCount, err := db.CountUsersWithAdminPermissionExcludingRole(serverID, roleID)
	if err != nil {
		return err
	}

	if otherAdminCount == 0 {
		return fmt.Errorf("cannot delete the last admin role - server must have at least one admin")
	}

	return nil
}

// CanModifyRolePermissions checks if role permissions can be safely modified
func (db *DB) CanModifyRolePermissions(roleID, serverID uuid.UUID, newPerms models.Permission) error {
	role, err := db.GetRoleByID(roleID)
	if err != nil {
		return err
	}

	// If removing Administrator permission (check using bitwise AND)
	hasAdmin := role.Permissions&models.PermissionAdministrator != 0
	willHaveAdmin := newPerms&models.PermissionAdministrator != 0

	if hasAdmin && !willHaveAdmin {
		// Check if other admin roles exist with assigned members
		otherAdminCount, err := db.CountUsersWithAdminPermissionExcludingRole(serverID, roleID)
		if err != nil {
			return err
		}

		if otherAdminCount == 0 {
			return fmt.Errorf("cannot remove admin permissions - server must have at least one admin")
		}
	}

	return nil
}

// CanRemoveRoleFromUser checks if a role can be safely removed from a user
func (db *DB) CanRemoveRoleFromUser(userID, roleID, serverID uuid.UUID) error {
	role, err := db.GetRoleByID(roleID)
	if err != nil {
		return err
	}

	// If it's not an admin role, removal is safe
	if !role.HasPermission(models.PermissionAdministrator) {
		return nil
	}

	// Check if user has other admin roles
	hasOtherAdminRoles, err := db.UserHasOtherAdminRoles(userID, roleID, serverID)
	if err != nil {
		return err
	}
	if hasOtherAdminRoles {
		return nil // User will still be admin via another role
	}

	// Check if other users have admin permissions
	otherAdminCount, err := db.CountUsersWithAdminPermissionExcludingUser(serverID, userID)
	if err != nil {
		return err
	}

	if otherAdminCount == 0 {
		return fmt.Errorf("cannot remove the last admin from the server - assign another admin first")
	}

	return nil
}

// --- Member Moderation Operations ---

// MuteServerMember adds a server-wide mute for a user (channel_id = NULL)
func (db *DB) MuteServerMember(serverID, userID, mutedBy uuid.UUID, durationMinutes int, reason string) error {
	id := uuid.New()
	now := time.Now()
	var expiresAt *time.Time

	if durationMinutes > 0 {
		exp := now.Add(time.Duration(durationMinutes) * time.Minute)
		expiresAt = &exp
	} // nil = permanent mute

	_, err := db.Exec(`
		INSERT OR REPLACE INTO mutes (id, server_id, user_id, channel_id, muted_by, muted_at, muted_until, reason, duration, issued_by, issued_at, expires_at)
		VALUES (?, ?, ?, NULL, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id.String(), serverID.String(), userID.String(), mutedBy.String(), now, expiresAt, reason, durationMinutes, mutedBy.String(), now, expiresAt)
	return err
}

// UnmuteServerMember removes a server-wide mute for a user
func (db *DB) UnmuteServerMember(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`
		DELETE FROM mutes
		WHERE server_id = ? AND user_id = ? AND channel_id IS NULL`,
		serverID.String(), userID.String())
	return err
}

// IsMutedServer checks if a user has an active server-wide mute
func (db *DB) IsMutedServer(serverID, userID uuid.UUID) (bool, error) {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM mutes
		WHERE server_id = ? AND user_id = ? AND channel_id IS NULL
		AND (muted_until IS NULL OR muted_until > ?)`,
		serverID.String(), userID.String(), time.Now()).Scan(&count)
	return count > 0, err
}

// GetServerMuteExpiry returns the expiry time for a server-wide mute, or nil if not muted
func (db *DB) GetServerMuteExpiry(serverID, userID uuid.UUID) (*time.Time, error) {
	var mutedUntil sql.NullTime
	err := db.QueryRow(`
		SELECT muted_until FROM mutes
		WHERE server_id = ? AND user_id = ? AND channel_id IS NULL
		AND (muted_until IS NULL OR muted_until > ?)`,
		serverID.String(), userID.String(), time.Now()).Scan(&mutedUntil)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !mutedUntil.Valid {
		return nil, nil // Permanent mute represented as nil
	}
	return &mutedUntil.Time, nil
}

// IncrementKickCount increments the kick counter for a member
func (db *DB) IncrementKickCount(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`
		UPDATE server_members
		SET kick_count = kick_count + 1
		WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String())
	return err
}

// GetMemberKickCount returns the kick count for a member
func (db *DB) GetMemberKickCount(serverID, userID uuid.UUID) (int, error) {
	var count int
	err := db.QueryRow(`
		SELECT COALESCE(kick_count, 0) FROM server_members
		WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String()).Scan(&count)

	if err == sql.ErrNoRows {
		return 0, nil
	}
	return count, err
}

// btoi converts a bool to 0/1 for SQLite INTEGER columns.
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// ── Voice State Migrations ─────────────────────────────────────────────────────

// MigrateVoiceStates creates the voice_states table if it doesn't exist.
func (db *DB) MigrateVoiceStates() error {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM sqlite_master
		WHERE type='table' AND name='voice_states'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check voice_states table: %w", err)
	}
	if count > 0 {
		return nil // Already exists
	}

	log.Println("[MIGRATION] Creating voice_states table...")
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS voice_states (
			user_id             TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			server_id           TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			channel_id          TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
			is_self_muted       INTEGER DEFAULT 0,
			is_self_deafened    INTEGER DEFAULT 0,
			is_server_muted     INTEGER DEFAULT 0,
			is_server_deafened  INTEGER DEFAULT 0,
			joined_at           DATETIME NOT NULL,
			PRIMARY KEY (user_id, server_id)
		);
		CREATE INDEX IF NOT EXISTS idx_voice_states_channel ON voice_states(channel_id);
		CREATE INDEX IF NOT EXISTS idx_voice_states_server  ON voice_states(server_id);
	`)
	if err != nil {
		return fmt.Errorf("failed to create voice_states table: %w", err)
	}
	log.Println("[MIGRATION] voice_states table created")
	return nil
}

// MigrateVoiceChannelSettings adds max_users column to channels table.
func (db *DB) MigrateVoiceChannelSettings() error {
	var count int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('channels') WHERE name='max_users'
	`).Scan(&count)
	if err != nil {
		return fmt.Errorf("failed to check max_users column: %w", err)
	}
	if count > 0 {
		return nil
	}
	log.Println("[MIGRATION] Adding max_users column to channels table...")
	_, err = db.Exec(`ALTER TABLE channels ADD COLUMN max_users INTEGER DEFAULT 0`)
	if err != nil {
		return fmt.Errorf("failed to add max_users column: %w", err)
	}
	log.Println("[MIGRATION] max_users column added")
	return nil
}

// ── Voice State DB Methods ─────────────────────────────────────────────────────

// ClearAllVoiceStates removes all voice state rows. Called on server startup to
// discard stale state from a previous run.
func (db *DB) ClearAllVoiceStates() error {
	_, err := db.Exec(`DELETE FROM voice_states`)
	return err
}

// SetVoiceState upserts a user's voice state (join or update self-mute/deafen).
func (db *DB) SetVoiceState(userID, serverID, channelID uuid.UUID, selfMuted, selfDeafened bool) error {
	_, err := db.Exec(`
		INSERT INTO voice_states (user_id, server_id, channel_id, is_self_muted, is_self_deafened, joined_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, server_id) DO UPDATE SET
			channel_id       = excluded.channel_id,
			is_self_muted    = excluded.is_self_muted,
			is_self_deafened = excluded.is_self_deafened
	`,
		userID.String(), serverID.String(), channelID.String(),
		btoi(selfMuted), btoi(selfDeafened),
		time.Now().UTC(),
	)
	return err
}

// ClearVoiceState removes a user's voice state (they left all voice channels).
func (db *DB) ClearVoiceState(userID, serverID uuid.UUID) error {
	_, err := db.Exec(
		`DELETE FROM voice_states WHERE user_id = ? AND server_id = ?`,
		userID.String(), serverID.String(),
	)
	return err
}

// SetServerVoiceMute updates the server-mute / server-deafen flags for a user.
func (db *DB) SetServerVoiceMute(userID, serverID uuid.UUID, muted, deafened bool) error {
	_, err := db.Exec(`
		UPDATE voice_states SET is_server_muted = ?, is_server_deafened = ?
		WHERE user_id = ? AND server_id = ?`,
		btoi(muted), btoi(deafened),
		userID.String(), serverID.String(),
	)
	return err
}

// GetVoiceStatesForChannel returns all active voice states for a channel.
func (db *DB) GetVoiceStatesForChannel(channelID uuid.UUID) ([]*models.VoiceState, error) {
	rows, err := db.Query(`
		SELECT user_id, server_id, channel_id,
		       is_self_muted, is_self_deafened, is_server_muted, is_server_deafened, joined_at
		FROM voice_states WHERE channel_id = ?`, channelID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVoiceStates(rows)
}

// GetVoiceStatesForServer returns all active voice states for a server.
func (db *DB) GetVoiceStatesForServer(serverID uuid.UUID) ([]*models.VoiceState, error) {
	rows, err := db.Query(`
		SELECT user_id, server_id, channel_id,
		       is_self_muted, is_self_deafened, is_server_muted, is_server_deafened, joined_at
		FROM voice_states WHERE server_id = ?`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVoiceStates(rows)
}

// scanVoiceStates reads voice state rows into a slice.
func scanVoiceStates(rows *sql.Rows) ([]*models.VoiceState, error) {
	var states []*models.VoiceState
	for rows.Next() {
		var (
			vs                                                         models.VoiceState
			userID, serverID, channelID                                string
			selfMuted, selfDeafened, serverMuted, serverDeafened       int
		)
		if err := rows.Scan(
			&userID, &serverID, &channelID,
			&selfMuted, &selfDeafened, &serverMuted, &serverDeafened,
			&vs.JoinedAt,
		); err != nil {
			return nil, err
		}
		vs.UserID, _   = uuid.Parse(userID)
		vs.ServerID, _ = uuid.Parse(serverID)
		vs.ChannelID, _ = uuid.Parse(channelID)
		vs.IsSelfMuted      = selfMuted != 0
		vs.IsSelfDeafened   = selfDeafened != 0
		vs.IsServerMuted    = serverMuted != 0
		vs.IsServerDeafened = serverDeafened != 0
		states = append(states, &vs)
	}
	return states, rows.Err()
}
