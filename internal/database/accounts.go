package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

// Account verification and recovery (2026-09-30): a verified-email flag on
// every user, an email change waiting for its code, and the one-time codes
// themselves (hashed; the server package makes and checks them).

// MigrateAccountVerification adds the columns and table. Existing users
// default to verified: only accounts created while a server requires
// verification start unverified.
func (db *DB) MigrateAccountVerification() error {
	if err := addColumnIfMissing(db, "users", "email_verified", "INTEGER NOT NULL DEFAULT 1"); err != nil {
		return err
	}
	if err := addColumnIfMissing(db, "users", "pending_email", "TEXT"); err != nil {
		return err
	}
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS account_codes (
			user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			purpose    TEXT NOT NULL,
			code_hash  TEXT NOT NULL,
			expires_at DATETIME NOT NULL,
			sent_at    DATETIME NOT NULL,
			attempts   INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (user_id, purpose)
		)`); err != nil {
		return fmt.Errorf("failed to create account_codes table: %w", err)
	}
	return nil
}

// AccountCode is a one-time code waiting to be entered.
type AccountCode struct {
	UserID    uuid.UUID
	Purpose   string
	CodeHash  string
	ExpiresAt time.Time
	SentAt    time.Time
	Attempts  int
}

// FindUserByEmail looks a user up by email, ignoring case when there's no
// exact match (addresses were stored as typed before 2026-09-30).
func (db *DB) FindUserByEmail(email string) (*models.User, string, error) {
	user, hash, err := db.GetUserByEmail(email)
	if err == nil || !errors.Is(err, sql.ErrNoRows) {
		return user, hash, err
	}
	var exact string
	if err := db.QueryRow(`SELECT email FROM users WHERE email = ? COLLATE NOCASE LIMIT 1`, strings.TrimSpace(email)).Scan(&exact); err != nil {
		return nil, "", err
	}
	return db.GetUserByEmail(exact)
}

// EmailInUse reports whether another account has this email (any case).
func (db *DB) EmailInUse(email string, except uuid.UUID) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE email = ? COLLATE NOCASE AND id != ?`,
		strings.TrimSpace(email), except.String()).Scan(&n)
	return n > 0, err
}

// IsEmailVerified reports the user's verified flag.
func (db *DB) IsEmailVerified(userID uuid.UUID) (bool, error) {
	var v int
	err := db.QueryRow(`SELECT email_verified FROM users WHERE id = ?`, userID.String()).Scan(&v)
	return v != 0, err
}

// SetEmailVerified sets the user's verified flag.
func (db *DB) SetEmailVerified(userID uuid.UUID, verified bool) error {
	v := 0
	if verified {
		v = 1
	}
	_, err := db.Exec(`UPDATE users SET email_verified = ?, updated_at = ? WHERE id = ?`, v, time.Now(), userID.String())
	return err
}

// GetPasswordHash returns the user's bcrypt hash.
func (db *DB) GetPasswordHash(userID uuid.UUID) (string, error) {
	var h string
	err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, userID.String()).Scan(&h)
	return h, err
}

// UpdatePasswordHash replaces the user's bcrypt hash.
func (db *DB) UpdatePasswordHash(userID uuid.UUID, hash string) error {
	_, err := db.Exec(`UPDATE users SET password_hash = ?, updated_at = ? WHERE id = ?`, hash, time.Now(), userID.String())
	return err
}

// UpdateUserEmail changes the user's email and clears any pending change.
func (db *DB) UpdateUserEmail(userID uuid.UUID, email string) error {
	_, err := db.Exec(`UPDATE users SET email = ?, pending_email = NULL, updated_at = ? WHERE id = ?`, email, time.Now(), userID.String())
	return err
}

// SetPendingEmail records an email change that's waiting for its code
// ("" clears it).
func (db *DB) SetPendingEmail(userID uuid.UUID, email string) error {
	var v interface{}
	if email != "" {
		v = email
	}
	_, err := db.Exec(`UPDATE users SET pending_email = ?, updated_at = ? WHERE id = ?`, v, time.Now(), userID.String())
	return err
}

// GetPendingEmail returns the email change waiting for its code, if any.
func (db *DB) GetPendingEmail(userID uuid.UUID) (string, error) {
	var v sql.NullString
	err := db.QueryRow(`SELECT pending_email FROM users WHERE id = ?`, userID.String()).Scan(&v)
	return v.String, err
}

// RenameUser changes a user's username, keeping their discriminator when
// it's still free under the new name and otherwise picking a free one.
// It returns the discriminator the user ends up with.
func (db *DB) RenameUser(userID uuid.UUID, username string) (string, error) {
	var disc string
	if err := db.QueryRow(`SELECT discriminator FROM users WHERE id = ?`, userID.String()).Scan(&disc); err != nil {
		return "", err
	}
	for i := 0; i < 50; i++ {
		var n int
		if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE username = ? AND discriminator = ? AND id != ?`,
			username, disc, userID.String()).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			_, err := db.Exec(`UPDATE users SET username = ?, discriminator = ?, updated_at = ? WHERE id = ?`,
				username, disc, time.Now(), userID.String())
			return disc, err
		}
		disc = models.NewUser(username, "").Discriminator
	}
	return "", fmt.Errorf("no free discriminator for %q", username)
}

// SaveAccountCode stores (replacing) the user's code for purpose.
func (db *DB) SaveAccountCode(c AccountCode) error {
	_, err := db.Exec(`
		INSERT INTO account_codes (user_id, purpose, code_hash, expires_at, sent_at, attempts)
		VALUES (?, ?, ?, ?, ?, 0)
		ON CONFLICT(user_id, purpose) DO UPDATE SET
			code_hash = excluded.code_hash, expires_at = excluded.expires_at,
			sent_at = excluded.sent_at, attempts = 0`,
		c.UserID.String(), c.Purpose, c.CodeHash, c.ExpiresAt.UTC(), c.SentAt.UTC())
	return err
}

// GetAccountCode returns the user's code for purpose (sql.ErrNoRows if none).
func (db *DB) GetAccountCode(userID uuid.UUID, purpose string) (*AccountCode, error) {
	c := &AccountCode{UserID: userID, Purpose: purpose}
	err := db.QueryRow(`SELECT code_hash, expires_at, sent_at, attempts FROM account_codes WHERE user_id = ? AND purpose = ?`,
		userID.String(), purpose).Scan(&c.CodeHash, &c.ExpiresAt, &c.SentAt, &c.Attempts)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CountCodeAttempt records a wrong guess at the user's code.
func (db *DB) CountCodeAttempt(userID uuid.UUID, purpose string) error {
	_, err := db.Exec(`UPDATE account_codes SET attempts = attempts + 1 WHERE user_id = ? AND purpose = ?`, userID.String(), purpose)
	return err
}

// DeleteAccountCode removes the user's code for purpose.
func (db *DB) DeleteAccountCode(userID uuid.UUID, purpose string) error {
	_, err := db.Exec(`DELETE FROM account_codes WHERE user_id = ? AND purpose = ?`, userID.String(), purpose)
	return err
}

// DeleteUserSessionsExcept removes every session of the user except the
// one with keepTokenHash.
func (db *DB) DeleteUserSessionsExcept(userID uuid.UUID, keepTokenHash string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID.String(), keepTokenHash)
	return err
}
