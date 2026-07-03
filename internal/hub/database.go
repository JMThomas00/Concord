package hub

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

const hubSchema = `
CREATE TABLE IF NOT EXISTS registered_servers (
    id                      TEXT PRIMARY KEY,
    name                    TEXT NOT NULL,
    description             TEXT DEFAULT '',
    category                TEXT DEFAULT '',
    tags                    TEXT DEFAULT '[]',
    host                    TEXT NOT NULL,
    port                    INTEGER NOT NULL,
    registration_secret     TEXT NOT NULL,
    member_count            INTEGER DEFAULT 0,
    online_count            INTEGER DEFAULT 0,
    max_members             INTEGER DEFAULT 1000,
    is_online               INTEGER DEFAULT 0,
    last_heartbeat          DATETIME,
    registered_at           DATETIME NOT NULL,
    updated_at              DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS peer_hubs (
    id            TEXT PRIMARY KEY,
    name          TEXT DEFAULT '',
    url           TEXT NOT NULL UNIQUE,
    is_active     INTEGER DEFAULT 1,
    last_synced   DATETIME,
    registered_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS federated_servers (
    id           TEXT NOT NULL,
    hub_id       TEXT NOT NULL REFERENCES peer_hubs(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    description  TEXT DEFAULT '',
    category     TEXT DEFAULT '',
    tags         TEXT DEFAULT '[]',
    member_count INTEGER DEFAULT 0,
    online_count INTEGER DEFAULT 0,
    max_members  INTEGER DEFAULT 1000,
    is_online    INTEGER DEFAULT 0,
    last_seen    DATETIME,
    cached_at    DATETIME NOT NULL,
    PRIMARY KEY (id, hub_id)
);

CREATE TABLE IF NOT EXISTS join_tokens (
    token      TEXT PRIMARY KEY,
    server_id  TEXT NOT NULL,
    created_at DATETIME NOT NULL,
    expires_at DATETIME NOT NULL,
    used       INTEGER DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_servers_category ON registered_servers(category);
CREATE INDEX IF NOT EXISTS idx_servers_online   ON registered_servers(is_online);
CREATE INDEX IF NOT EXISTS idx_join_tokens_exp  ON join_tokens(expires_at);
`

// HubDB wraps the hub's SQLite connection.
type HubDB struct {
	*sql.DB
}

// NewHubDB opens (or creates) the hub SQLite database and initialises the schema.
func NewHubDB(path string) (*HubDB, error) {
	db, err := sql.Open("sqlite", path+"?_foreign_keys=on&_journal_mode=WAL")
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	hdb := &HubDB{db}
	if _, err := db.Exec(hubSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("init schema: %w", err)
	}
	return hdb, nil
}

// ── Registered servers ────────────────────────────────────────────────────────

func (db *HubDB) CreateServer(s *RegisteredServer) error {
	tags, _ := json.Marshal(s.Tags)
	now := time.Now().UTC()
	_, err := db.Exec(`
		INSERT INTO registered_servers
		(id, name, description, category, tags, host, port, registration_secret,
		 member_count, online_count, max_members, is_online, last_heartbeat, registered_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, s.Name, s.Description, s.Category, string(tags),
		s.Host, s.Port, s.RegistrationSecret,
		s.MemberCount, s.OnlineCount, s.MaxMembers,
		boolInt(s.IsOnline), s.LastHeartbeat.UTC(), now, now,
	)
	return err
}

func (db *HubDB) GetServer(id string) (*RegisteredServer, error) {
	row := db.QueryRow(`
		SELECT id, name, description, category, tags, host, port, registration_secret,
		       member_count, online_count, max_members, is_online, last_heartbeat,
		       registered_at, updated_at
		FROM registered_servers WHERE id = ?`, id)
	return scanServer(row)
}

func (db *HubDB) DeleteServer(id string) error {
	_, err := db.Exec(`DELETE FROM registered_servers WHERE id = ?`, id)
	return err
}

// MarkServerOffline sets is_online=0 for a single server. Used on graceful
// deregister so the server keeps its ID and secret for when it comes back.
func (db *HubDB) MarkServerOffline(id string) error {
	_, err := db.Exec(`UPDATE registered_servers SET is_online=0, updated_at=? WHERE id=?`,
		time.Now().UTC(), id)
	return err
}

// PurgeStaleServers deletes servers that haven't heartbeated since cutoff.
func (db *HubDB) PurgeStaleServers(cutoff time.Time) (int64, error) {
	res, err := db.Exec(`
		DELETE FROM registered_servers
		WHERE last_heartbeat IS NULL OR last_heartbeat < ?`, cutoff.UTC())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func (db *HubDB) UpdateHeartbeat(id string, memberCount, onlineCount int) error {
	_, err := db.Exec(`
		UPDATE registered_servers
		SET member_count=?, online_count=?, last_heartbeat=?, is_online=1, updated_at=?
		WHERE id=?`,
		memberCount, onlineCount, time.Now().UTC(), time.Now().UTC(), id,
	)
	return err
}

func (db *HubDB) MarkOfflineIfStale(cutoff time.Time) error {
	// All stored timestamps are UTC; normalize the cutoff or SQLite's string
	// comparison silently breaks across timezones.
	_, err := db.Exec(`
		UPDATE registered_servers SET is_online=0, updated_at=?
		WHERE is_online=1 AND (last_heartbeat IS NULL OR last_heartbeat < ?)`,
		time.Now().UTC(), cutoff.UTC(),
	)
	return err
}

// ListServers returns local registered servers filtered by category and/or search query.
// Pass includeOffline=true to include servers with is_online=0.
func (db *HubDB) ListServers(category, query string, includeOffline bool) ([]*RegisteredServer, error) {
	q := `SELECT id, name, description, category, tags, host, port, registration_secret,
	             member_count, online_count, max_members, is_online, last_heartbeat,
	             registered_at, updated_at
	      FROM registered_servers WHERE 1=1`
	var args []any
	if !includeOffline {
		q += " AND is_online=1"
	}
	if category != "" {
		q += " AND category=?"
		args = append(args, category)
	}
	if query != "" {
		q += " AND (name LIKE ? OR description LIKE ? OR tags LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like, like)
	}
	q += " ORDER BY online_count DESC, member_count DESC"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*RegisteredServer
	for rows.Next() {
		s, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── Join tokens ───────────────────────────────────────────────────────────────

func (db *HubDB) CreateJoinToken(t *JoinToken) error {
	_, err := db.Exec(`
		INSERT INTO join_tokens (token, server_id, created_at, expires_at, used)
		VALUES (?,?,?,?,0)`,
		t.Token, t.ServerID, t.CreatedAt.UTC(), t.ExpiresAt.UTC(),
	)
	return err
}

func (db *HubDB) CleanupExpiredTokens() error {
	_, err := db.Exec(`DELETE FROM join_tokens WHERE expires_at < ?`, time.Now().UTC())
	return err
}

// ── Peer hubs ─────────────────────────────────────────────────────────────────

func (db *HubDB) UpsertPeerHub(h *PeerHub) error {
	if h.ID == "" {
		h.ID = uuid.New().String()
	}
	now := time.Now().UTC()
	_, err := db.Exec(`
		INSERT INTO peer_hubs (id, name, url, is_active, last_synced, registered_at)
		VALUES (?,?,?,1,NULL,?)
		ON CONFLICT(url) DO UPDATE SET name=excluded.name, is_active=1`,
		h.ID, h.Name, h.URL, now,
	)
	return err
}

func (db *HubDB) ListPeerHubs() ([]*PeerHub, error) {
	rows, err := db.Query(`
		SELECT id, name, url, is_active, last_synced, registered_at
		FROM peer_hubs ORDER BY registered_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*PeerHub
	for rows.Next() {
		ph := &PeerHub{}
		var lastSynced sql.NullTime
		err := rows.Scan(&ph.ID, &ph.Name, &ph.URL, &ph.IsActive, &lastSynced, &ph.RegisteredAt)
		if err != nil {
			return nil, err
		}
		if lastSynced.Valid {
			ph.LastSynced = &lastSynced.Time
		}
		out = append(out, ph)
	}
	return out, rows.Err()
}

func (db *HubDB) MarkHubSynced(hubID string, t time.Time) error {
	_, err := db.Exec(`UPDATE peer_hubs SET last_synced=? WHERE id=?`, t.UTC(), hubID)
	return err
}

// ── Federated servers ─────────────────────────────────────────────────────────

func (db *HubDB) UpsertFederatedServer(hubID string, s *ServerListing) error {
	tags, _ := json.Marshal(s.Tags)
	now := time.Now().UTC()
	_, err := db.Exec(`
		INSERT INTO federated_servers
		(id, hub_id, name, description, category, tags,
		 member_count, online_count, max_members, is_online, last_seen, cached_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id, hub_id) DO UPDATE SET
		  name=excluded.name, description=excluded.description,
		  category=excluded.category, tags=excluded.tags,
		  member_count=excluded.member_count, online_count=excluded.online_count,
		  max_members=excluded.max_members, is_online=excluded.is_online,
		  last_seen=excluded.last_seen, cached_at=excluded.cached_at`,
		s.ID, hubID, s.Name, s.Description, s.Category, string(tags),
		s.MemberCount, s.OnlineCount, s.MaxMembers,
		boolInt(s.IsOnline), s.LastSeen, now,
	)
	return err
}

// GetFederatedServerOrigin returns the URL of the peer hub a federated server
// listing came from, so join requests can be proxied to it.
func (db *HubDB) GetFederatedServerOrigin(serverID string) (string, error) {
	var url string
	err := db.QueryRow(`
		SELECT ph.url
		FROM federated_servers fs
		JOIN peer_hubs ph ON ph.id = fs.hub_id AND ph.is_active=1
		WHERE fs.id = ?
		ORDER BY fs.is_online DESC, fs.cached_at DESC
		LIMIT 1`, serverID).Scan(&url)
	return url, err
}

// ListFederatedServers returns all cached federated server listings joined with hub names.
func (db *HubDB) ListFederatedServers(category, query string) ([]*ServerListing, error) {
	q := `SELECT fs.id, fs.name, fs.description, fs.category, fs.tags,
	             fs.member_count, fs.online_count, fs.max_members, fs.is_online,
	             fs.last_seen, ph.name
	      FROM federated_servers fs
	      JOIN peer_hubs ph ON ph.id = fs.hub_id AND ph.is_active=1
	      WHERE 1=1`
	var args []any
	if category != "" {
		q += " AND fs.category=?"
		args = append(args, category)
	}
	if query != "" {
		q += " AND (fs.name LIKE ? OR fs.description LIKE ?)"
		like := "%" + query + "%"
		args = append(args, like, like)
	}
	q += " ORDER BY fs.online_count DESC"

	rows, err := db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*ServerListing
	for rows.Next() {
		s := &ServerListing{}
		var tagsJSON string
		var lastSeen sql.NullString
		err := rows.Scan(&s.ID, &s.Name, &s.Description, &s.Category, &tagsJSON,
			&s.MemberCount, &s.OnlineCount, &s.MaxMembers, &s.IsOnline,
			&lastSeen, &s.FromHub)
		if err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(tagsJSON), &s.Tags)
		if lastSeen.Valid {
			s.LastSeen = lastSeen.String
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ── Helpers ───────────────────────────────────────────────────────────────────

type serverScanner interface {
	Scan(dest ...any) error
}

func scanServer(row serverScanner) (*RegisteredServer, error) {
	s := &RegisteredServer{}
	var tagsJSON string
	var lastHB sql.NullTime
	err := row.Scan(
		&s.ID, &s.Name, &s.Description, &s.Category, &tagsJSON,
		&s.Host, &s.Port, &s.RegistrationSecret,
		&s.MemberCount, &s.OnlineCount, &s.MaxMembers,
		&s.IsOnline, &lastHB, &s.RegisteredAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(tagsJSON), &s.Tags)
	if lastHB.Valid {
		s.LastHeartbeat = lastHB.Time
	}
	return s, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// relativeTime formats t as a human-readable relative string ("2 minutes ago", "offline").
func relativeTime(t time.Time, isOnline bool) string {
	if !isOnline || t.IsZero() {
		return "offline"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", mins)
	case d < 24*time.Hour:
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", hrs)
	default:
		return strings.ToLower(t.Format("Jan 2"))
	}
}
