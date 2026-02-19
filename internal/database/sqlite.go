package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
	"github.com/concord-chat/concord/internal/models"
)

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

	-- Indexes for common queries
	CREATE INDEX IF NOT EXISTS idx_messages_channel_created ON messages(channel_id, created_at DESC);
	CREATE INDEX IF NOT EXISTS idx_messages_author ON messages(author_id);
	CREATE INDEX IF NOT EXISTS idx_channels_server ON channels(server_id);
	CREATE INDEX IF NOT EXISTS idx_server_members_server ON server_members(server_id);
	CREATE INDEX IF NOT EXISTS idx_roles_server ON roles(server_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_user ON sessions(user_id);
	CREATE INDEX IF NOT EXISTS idx_sessions_token ON sessions(token_hash);
	CREATE INDEX IF NOT EXISTS idx_invites_server ON invites(server_id);
	`

	_, err := db.Exec(schema)
	return err
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

	_, err := db.Exec(`
		INSERT INTO channels (id, server_id, name, topic, type, position, category_id,
			is_nsfw, rate_limit_per_user, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		channel.ID.String(), serverID, channel.Name, channel.Topic, channel.Type,
		channel.Position, categoryID, channel.IsNSFW, channel.RateLimitPerUser,
		channel.CreatedAt, channel.UpdatedAt)
	return err
}

// GetServerChannels retrieves all channels for a server
func (db *DB) GetServerChannels(serverID uuid.UUID) ([]*models.Channel, error) {
	rows, err := db.Query(`
		SELECT id, server_id, name, topic, type, position, category_id,
			is_nsfw, rate_limit_per_user, created_at, updated_at
		FROM channels WHERE server_id = ?
		ORDER BY position`, serverID.String())
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
			&ch.Position, &categoryID, &ch.IsNSFW, &ch.RateLimitPerUser,
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
		SELECT id, server_id, name, topic, type, position, category_id,
			is_nsfw, rate_limit_per_user, created_at, updated_at
		FROM channels WHERE id = ?`, channelID.String()).
		Scan(&idStr, &serverIDStr, &ch.Name, &topic, &ch.Type, &ch.Position,
			&categoryID, &ch.IsNSFW, &ch.RateLimitPerUser, &ch.CreatedAt, &ch.UpdatedAt)

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
		SET name = ?, topic = ?, category_id = ?, position = ?, is_nsfw = ?,
			rate_limit_per_user = ?, updated_at = ?
		WHERE id = ?`,
		channel.Name, channel.Topic, categoryID, channel.Position, channel.IsNSFW,
		channel.RateLimitPerUser, time.Now(), channel.ID.String())

	return err
}

// DeleteChannel deletes a channel and all its messages
func (db *DB) DeleteChannel(channelID uuid.UUID) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete messages first (foreign key constraint)
	_, err = tx.Exec(`DELETE FROM messages WHERE channel_id = ?`, channelID.String())
	if err != nil {
		return err
	}

	// Delete channel
	_, err = tx.Exec(`DELETE FROM channels WHERE id = ?`, channelID.String())
	if err != nil {
		return err
	}

	return tx.Commit()
}

// GetServerMember retrieves a server member relationship
func (db *DB) GetServerMember(serverID, userID uuid.UUID) (*models.ServerMember, error) {
	var member models.ServerMember
	var serverIDStr, userIDStr string
	var nickname sql.NullString

	err := db.QueryRow(`
		SELECT server_id, user_id, nickname, joined_at, is_muted, is_deafened
		FROM server_members WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String()).
		Scan(&serverIDStr, &userIDStr, &nickname, &member.JoinedAt, &member.IsMuted, &member.IsDeafened)

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

	_, err := db.Exec(`
		INSERT INTO messages (id, channel_id, author_id, content, type, created_at, is_pinned, reply_to_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		msg.ID.String(), msg.ChannelID.String(), msg.AuthorID.String(),
		msg.Content, msg.Type, msg.CreatedAt, msg.IsPinned, replyToID)
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
func (db *DB) GetChannelMessages(channelID uuid.UUID, limit int, before *uuid.UUID) ([]*models.Message, error) {
	var query string
	var args []interface{}

	if before != nil {
		query = `
			SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, reply_to_id
			FROM messages
			WHERE channel_id = ? AND created_at < (SELECT created_at FROM messages WHERE id = ?)
			ORDER BY created_at DESC
			LIMIT ?`
		args = []interface{}{channelID.String(), before.String(), limit}
	} else {
		query = `
			SELECT id, channel_id, author_id, content, type, created_at, edited_at, is_pinned, reply_to_id
			FROM messages
			WHERE channel_id = ?
			ORDER BY created_at DESC
			LIMIT ?`
		args = []interface{}{channelID.String(), limit}
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
		var replyToID sql.NullString

		err := rows.Scan(&idStr, &channelIDStr, &authorIDStr, &msg.Content,
			&msg.Type, &msg.CreatedAt, &editedAt, &msg.IsPinned, &replyToID)
		if err != nil {
			return nil, err
		}

		msg.ID, _ = uuid.Parse(idStr)
		msg.ChannelID, _ = uuid.Parse(channelIDStr)
		msg.AuthorID, _ = uuid.Parse(authorIDStr)
		if editedAt.Valid {
			msg.EditedAt = &editedAt.Time
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

// GetServerMembers retrieves all members of a server, including their role IDs
func (db *DB) GetServerMembers(serverID uuid.UUID) ([]*models.ServerMember, error) {
	rows, err := db.Query(`
		SELECT user_id, server_id, nickname, joined_at, is_muted, is_deafened
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

		err := rows.Scan(&userIDStr, &serverIDStr, &m.Nickname, &m.JoinedAt, &m.IsMuted, &m.IsDeafened)
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
		INSERT INTO roles (id, server_id, name, color, permissions, position,
			is_hoisted, is_mentionable, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role.ID.String(), role.ServerID.String(), role.Name, role.Color,
		int64(role.Permissions), role.Position, role.IsHoisted, role.IsMentionable,
		role.IsDefault, role.CreatedAt, role.UpdatedAt)
	return err
}

// GetServerRoles retrieves all roles for a server
func (db *DB) GetServerRoles(serverID uuid.UUID) ([]*models.Role, error) {
	rows, err := db.Query(`
		SELECT id, server_id, name, color, permissions, position,
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
			&r.Position, &r.IsHoisted, &r.IsMentionable, &r.IsDefault,
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
		SELECT id, server_id, name, color, permissions, position,
			is_hoisted, is_mentionable, is_default, created_at, updated_at
		FROM roles WHERE id = ?`, roleID.String()).
		Scan(&idStr, &serverIDStr, &r.Name, &r.Color, &permInt,
			&r.Position, &r.IsHoisted, &r.IsMentionable, &r.IsDefault,
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
func (db *DB) EnsureDefaultServer() (*models.Server, *models.Role, error) {
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
		server := models.NewServer("Concord Server", systemUserID)
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

	server, _, err := db.EnsureDefaultServer()
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
