package database

// Invites and join modes (2026-11-05). A server is open (anyone who can
// reach it may register), invite only (a code is needed to become a
// member), or approval (anyone may ask; a moderator lets them in, and a
// valid code skips the wait). Invite times are kept as Unix seconds, since
// stored DATETIMEs are text in the server's zone and can't be compared.

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Join modes.
const (
	JoinOpen     = "open"
	JoinInvite   = "invite"
	JoinApproval = "approval"
)

// ValidJoinMode reports whether m is a join mode.
func ValidJoinMode(m string) bool { return m == JoinOpen || m == JoinInvite || m == JoinApproval }

// Invite is one invite code.
type Invite struct {
	Code      string
	ServerID  uuid.UUID
	InviterID uuid.UUID
	Email     string // set: only this address may use it (an emailed invite)
	Note      string
	MaxUses   int // 0: unlimited
	Uses      int
	CreatedAt time.Time
	ExpiresAt time.Time // zero: never
	Revoked   bool
}

// Usable reports whether the invite can still let someone in at now, and
// if not, why.
func (in *Invite) Usable(now time.Time) error {
	switch {
	case in.Revoked:
		return ErrInviteRevoked
	case !in.ExpiresAt.IsZero() && !now.Before(in.ExpiresAt):
		return ErrInviteExpired
	case in.MaxUses > 0 && in.Uses >= in.MaxUses:
		return ErrInviteUsedUp
	}
	return nil
}

// Why an invite can't be used.
var (
	ErrInviteNotFound = errors.New("that invite code doesn't exist")
	ErrInviteRevoked  = errors.New("that invite was cancelled")
	ErrInviteExpired  = errors.New("that invite has expired")
	ErrInviteUsedUp   = errors.New("that invite has been used up")
)

// JoinRequest is someone waiting for a moderator (approval mode).
type JoinRequest struct {
	UserID    uuid.UUID
	ServerID  uuid.UUID
	Note      string
	CreatedAt time.Time
	Denied    bool // turned down: they stay out until someone invites them
}

func migrateInvites(db *DB) error {
	for _, c := range []struct{ table, col, typ string }{
		{"invites", "email", "TEXT"},
		{"invites", "note", "TEXT"},
		{"invites", "created_unix", "INTEGER"},
		{"invites", "expires_unix", "INTEGER"},
		{"servers", "join_mode", "TEXT DEFAULT 'open'"},
		{"server_members", "invite_code", "TEXT"},
		{"server_members", "invited_by", "TEXT"},
	} {
		if err := addColumnIfMissing(db, c.table, c.col, c.typ); err != nil {
			return err
		}
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS join_requests (
			user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			server_id    TEXT NOT NULL REFERENCES servers(id) ON DELETE CASCADE,
			note         TEXT,
			created_unix INTEGER NOT NULL,
			denied       INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, server_id)
		)`); err != nil {
		return fmt.Errorf("creating join_requests: %w", err)
	}
	// Create Invite was never checked, so nobody had it but administrators
	// in effect. From now on the Moderator preset carries it: give it once
	// to roles that are exactly that preset as it was.
	return runOnce(db, "2026-11-05-moderator-invites", func() error {
		old := int64(models.PermissionsModerator &^ models.PermissionCreateInvite)
		_, err := db.Exec(`UPDATE roles SET permissions = ? WHERE is_default = 0 AND permissions = ?`,
			int64(models.PermissionsModerator), old)
		return err
	})
}

// JoinMode is how people become members of a server.
func (db *DB) JoinMode(serverID uuid.UUID) (string, error) {
	var m sql.NullString
	err := db.QueryRow(`SELECT join_mode FROM servers WHERE id = ?`, serverID.String()).Scan(&m)
	if err != nil {
		return JoinOpen, err
	}
	if !ValidJoinMode(m.String) {
		return JoinOpen, nil
	}
	return m.String, nil
}

// SetJoinMode changes how people become members.
func (db *DB) SetJoinMode(serverID uuid.UUID, mode string) error {
	if !ValidJoinMode(mode) {
		return fmt.Errorf("unknown join mode %q", mode)
	}
	_, err := db.Exec(`UPDATE servers SET join_mode = ? WHERE id = ?`, mode, serverID.String())
	return err
}

func unixOrZero(n sql.NullInt64) time.Time {
	if !n.Valid || n.Int64 == 0 {
		return time.Time{}
	}
	return time.Unix(n.Int64, 0)
}

func unixOf(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Unix()
}

// CreateInvite stores a new invite.
func (db *DB) CreateInvite(in *Invite) error {
	if in.CreatedAt.IsZero() {
		in.CreatedAt = time.Now()
	}
	maxAge := 0
	if !in.ExpiresAt.IsZero() {
		maxAge = int(in.ExpiresAt.Sub(in.CreatedAt).Seconds())
	}
	_, err := db.Exec(`
		INSERT INTO invites (code, server_id, inviter_id, max_age, max_uses, uses, created_at, is_revoked,
		                     email, note, created_unix, expires_unix)
		VALUES (?, ?, ?, ?, ?, 0, ?, 0, ?, ?, ?, ?)`,
		in.Code, in.ServerID.String(), in.InviterID.String(), maxAge, in.MaxUses, in.CreatedAt.UTC(),
		strings.ToLower(in.Email), in.Note, in.CreatedAt.Unix(), unixOf(in.ExpiresAt))
	return err
}

const inviteCols = `code, server_id, inviter_id, COALESCE(email, ''), COALESCE(note, ''), max_uses, uses,
	COALESCE(created_unix, 0), expires_unix, COALESCE(is_revoked, 0)`

func scanInvite(row interface{ Scan(...any) error }) (*Invite, error) {
	in := &Invite{}
	var server, inviter string
	var created int64
	var expires sql.NullInt64
	if err := row.Scan(&in.Code, &server, &inviter, &in.Email, &in.Note, &in.MaxUses, &in.Uses, &created, &expires, &in.Revoked); err != nil {
		return nil, err
	}
	in.ServerID, _ = uuid.Parse(server)
	in.InviterID, _ = uuid.Parse(inviter)
	in.CreatedAt = time.Unix(created, 0)
	in.ExpiresAt = unixOrZero(expires)
	return in, nil
}

// GetInvite finds an invite by its code (as typed: case and dashes don't
// matter).
func (db *DB) GetInvite(code string) (*Invite, error) {
	in, err := scanInvite(db.QueryRow(`SELECT `+inviteCols+` FROM invites WHERE code = ?`, code))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrInviteNotFound
	}
	return in, err
}

// ListInvites is a server's invites, newest first: everyone's, or one
// inviter's (inviter != uuid.Nil). Revoked ones are left out.
func (db *DB) ListInvites(serverID, inviter uuid.UUID) ([]*Invite, error) {
	q := `SELECT ` + inviteCols + ` FROM invites WHERE server_id = ? AND COALESCE(is_revoked, 0) = 0`
	args := []any{serverID.String()}
	if inviter != uuid.Nil {
		q += ` AND inviter_id = ?`
		args = append(args, inviter.String())
	}
	rows, err := db.Query(q+` ORDER BY COALESCE(created_unix, 0) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Invite
	for rows.Next() {
		in, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// RevokeInvite cancels an invite.
func (db *DB) RevokeInvite(code string) error {
	_, err := db.Exec(`UPDATE invites SET is_revoked = 1 WHERE code = ?`, code)
	return err
}

// UseInvite counts one use of an invite, if it's still usable at now.
func (db *DB) UseInvite(code string, now time.Time) error {
	in, err := db.GetInvite(code)
	if err != nil {
		return err
	}
	if err := in.Usable(now); err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE invites SET uses = uses + 1
		WHERE code = ? AND COALESCE(is_revoked, 0) = 0 AND (max_uses = 0 OR uses < max_uses)`, code)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInviteUsedUp
	}
	return nil
}

// EmailInvitesSince is how many emailed invites inviter has sent since t.
func (db *DB) EmailInvitesSince(inviter uuid.UUID, t time.Time) (int, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM invites WHERE inviter_id = ? AND COALESCE(email, '') != '' AND COALESCE(created_unix, 0) >= ?`,
		inviter.String(), t.Unix()).Scan(&n)
	return n, err
}

// PendingEmailInvite is a usable invite already sent to email in a server,
// or nil.
func (db *DB) PendingEmailInvite(serverID uuid.UUID, email string, now time.Time) (*Invite, error) {
	rows, err := db.Query(`SELECT `+inviteCols+` FROM invites WHERE server_id = ? AND email = ?`,
		serverID.String(), strings.ToLower(email))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		in, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		if in.Usable(now) == nil {
			return in, nil
		}
	}
	return nil, rows.Err()
}

// SetMemberInvite records which invite (and whose) let a member in.
func (db *DB) SetMemberInvite(serverID, userID uuid.UUID, code string, inviter uuid.UUID) error {
	_, err := db.Exec(`UPDATE server_members SET invite_code = ?, invited_by = ? WHERE server_id = ? AND user_id = ?`,
		code, inviter.String(), serverID.String(), userID.String())
	return err
}

// MemberInviters maps each member of a server who joined with an invite to
// whoever made it.
func (db *DB) MemberInviters(serverID uuid.UUID) (map[uuid.UUID]uuid.UUID, error) {
	rows, err := db.Query(`SELECT user_id, invited_by FROM server_members WHERE server_id = ? AND COALESCE(invited_by, '') != ''`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		var u, by string
		if err := rows.Scan(&u, &by); err != nil {
			return nil, err
		}
		uid, err1 := uuid.Parse(u)
		bid, err2 := uuid.Parse(by)
		if err1 == nil && err2 == nil {
			out[uid] = bid
		}
	}
	return out, rows.Err()
}

// AddJoinRequest records (or refreshes the note of) someone asking to join.
func (db *DB) AddJoinRequest(r JoinRequest) error {
	if r.CreatedAt.IsZero() {
		r.CreatedAt = time.Now()
	}
	_, err := db.Exec(`INSERT INTO join_requests (user_id, server_id, note, created_unix) VALUES (?, ?, ?, ?)
		ON CONFLICT(user_id, server_id) DO UPDATE SET note = CASE WHEN excluded.note != '' THEN excluded.note ELSE join_requests.note END`,
		r.UserID.String(), r.ServerID.String(), r.Note, r.CreatedAt.Unix())
	return err
}

// GetJoinRequest is a user's request to join, or nil.
func (db *DB) GetJoinRequest(serverID, userID uuid.UUID) (*JoinRequest, error) {
	r := &JoinRequest{UserID: userID, ServerID: serverID}
	var created int64
	var note sql.NullString
	err := db.QueryRow(`SELECT note, created_unix, denied FROM join_requests WHERE server_id = ? AND user_id = ?`,
		serverID.String(), userID.String()).Scan(&note, &created, &r.Denied)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Note, r.CreatedAt = note.String, time.Unix(created, 0)
	return r, nil
}

// ListJoinRequests is everyone waiting to join a server (not those turned
// down), oldest first.
func (db *DB) ListJoinRequests(serverID uuid.UUID) ([]JoinRequest, error) {
	rows, err := db.Query(`SELECT user_id, COALESCE(note, ''), created_unix FROM join_requests WHERE server_id = ? AND denied = 0 ORDER BY created_unix`, serverID.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []JoinRequest
	for rows.Next() {
		var u string
		var created int64
		r := JoinRequest{ServerID: serverID}
		if err := rows.Scan(&u, &r.Note, &created); err != nil {
			return nil, err
		}
		r.UserID, _ = uuid.Parse(u)
		r.CreatedAt = time.Unix(created, 0)
		out = append(out, r)
	}
	return out, rows.Err()
}

// DenyJoinRequest turns a request down.
func (db *DB) DenyJoinRequest(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`UPDATE join_requests SET denied = 1 WHERE server_id = ? AND user_id = ?`, serverID.String(), userID.String())
	return err
}

// DeleteJoinRequest removes a request (approved, denied or withdrawn).
func (db *DB) DeleteJoinRequest(serverID, userID uuid.UUID) error {
	_, err := db.Exec(`DELETE FROM join_requests WHERE server_id = ? AND user_id = ?`, serverID.String(), userID.String())
	return err
}

// GetEveryoneRole is a server's @everyone role.
func (db *DB) GetEveryoneRole(serverID uuid.UUID) (*models.Role, error) {
	var id string
	if err := db.QueryRow(`SELECT id FROM roles WHERE server_id = ? AND is_default = 1 LIMIT 1`, serverID.String()).Scan(&id); err != nil {
		return nil, err
	}
	rid, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	return db.GetRoleByID(rid)
}
