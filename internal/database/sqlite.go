package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/concord-chat/concord/internal/models"
)

// DB wraps the SQLite database connection
type DB struct {
	*sql.DB
}

// New creates a new database connection and initializes schema
func New(path string) (*DB, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_journal_mode=WAL")
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
	err := db.QueryRow(`
		SELECT id, username, discriminator, display_name, email, avatar_hash,
			status, status_text, created_at, updated_at, last_seen_at, is_bot
		FROM users WHERE id = ?`, id.String()).Scan(
		&idStr, &user.Username, &user.Discriminator, &user.DisplayName,
		&user.Email, &user.AvatarHash, &user.Status, &user.StatusText,
		&user.CreatedAt, &user.UpdatedAt, &user.LastSeenAt, &user.IsBot)
	if err != nil {
		return nil, err
	}
	user.ID, _ = uuid.Parse(idStr)
	return user, nil
}

// GetUserByEmail retrieves a user by their email
func (db *DB) GetUserByEmail(email string) (*models.User, string, error) {
	user := &models.User{}
	var idStr, passwordHash string
	err := db.QueryRow(`
		SELECT id, username, discriminator, display_name, email, password_hash, avatar_hash,
			status, status_text, created_at, updated_at, last_seen_at, is_bot
		FROM users WHERE email = ?`, email).Scan(
		&idStr, &user.Username, &user.Discriminator, &user.DisplayName,
		&user.Email, &passwordHash, &user.AvatarHash, &user.Status, &user.StatusText,
		&user.CreatedAt, &user.UpdatedAt, &user.LastSeenAt, &user.IsBot)
	if err != nil {
		return nil, "", err
	}
	user.ID, _ = uuid.Parse(idStr)
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
	var defaultChanID, systemChanID, rulesChanID sql.NullString

	err := db.QueryRow(`
		SELECT id, name, description, icon_hash, owner_id, default_channel_id,
			system_channel_id, rules_channel_id, max_members, created_at, updated_at,
			verification_level, explicit_content_filter, invites_enabled,
			default_invite_max_age, default_invite_max_uses
		FROM servers WHERE id = ?`, id.String()).Scan(
		&idStr, &server.Name, &server.Description, &server.IconHash,
		&ownerIDStr, &defaultChanID, &systemChanID, &rulesChanID,
		&server.MaxMembers, &server.CreatedAt, &server.UpdatedAt,
		&server.VerificationLevel, &server.ExplicitContentFilter,
		&server.InvitesEnabled, &server.DefaultInviteMaxAge, &server.DefaultInviteMaxUses)
	if err != nil {
		return nil, err
	}

	server.ID, _ = uuid.Parse(idStr)
	server.OwnerID, _ = uuid.Parse(ownerIDStr)
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
		var defaultChanID, systemChanID sql.NullString

		err := rows.Scan(&idStr, &server.Name, &server.Description, &server.IconHash,
			&ownerIDStr, &defaultChanID, &systemChanID, &server.MaxMembers,
			&server.CreatedAt, &server.UpdatedAt)
		if err != nil {
			return nil, err
		}

		server.ID, _ = uuid.Parse(idStr)
		server.OwnerID, _ = uuid.Parse(ownerIDStr)
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

	return messages, rows.Err()
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

// GetServerMembers retrieves all members of a server
func (db *DB) GetServerMembers(serverID uuid.UUID) ([]*models.ServerMember, error) {
	rows, err := db.Query(`
		SELECT user_id, server_id, nickname, joined_at, is_muted, is_deafened
		FROM server_members WHERE server_id = ?`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var members []*models.ServerMember
	for rows.Next() {
		m := &models.ServerMember{}
		var userIDStr, serverIDStr string

		err := rows.Scan(&userIDStr, &serverIDStr, &m.Nickname, &m.JoinedAt, &m.IsMuted, &m.IsDeafened)
		if err != nil {
			return nil, err
		}

		m.UserID, _ = uuid.Parse(userIDStr)
		m.ServerID, _ = uuid.Parse(serverIDStr)

		members = append(members, m)
	}

	return members, rows.Err()
}

// --- Role Operations ---

// CreateRole inserts a new role
func (db *DB) CreateRole(role *models.Role) error {
	_, err := db.Exec(`
		INSERT INTO roles (id, server_id, name, color, permissions, position,
			is_hoisted, is_mentionable, is_default, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		role.ID.String(), role.ServerID.String(), role.Name, role.Color,
		role.Permissions, role.Position, role.IsHoisted, role.IsMentionable,
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

		err := rows.Scan(&idStr, &serverIDStr, &r.Name, &r.Color, &r.Permissions,
			&r.Position, &r.IsHoisted, &r.IsMentionable, &r.IsDefault,
			&r.CreatedAt, &r.UpdatedAt)
		if err != nil {
			return nil, err
		}

		r.ID, _ = uuid.Parse(idStr)
		r.ServerID, _ = uuid.Parse(serverIDStr)

		roles = append(roles, r)
	}

	return roles, rows.Err()
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
