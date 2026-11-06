package database

// Opening the database (Privacy Pack 1, 2026-11-05). SQLite runs through
// github.com/ncruces/go-sqlite3: pure Go (SQLite compiled to WebAssembly,
// run by wazero), with an encrypting file layer, "adiantum" (Adiantum,
// XChaCha12 + AES, in 4 KiB blocks). An encrypted database, its write-ahead
// log and its temporary files are unreadable without the key. The cipher is
// for data at rest: it doesn't detect tampering, and someone holding several
// copies over time can see which blocks changed.
//
// Times are stored as SQLite's own text format in UTC to the second,
// "2006-01-02 15:04:05" (what CURRENT_TIMESTAMP writes), which sorts as text
// and works with SQLite's date functions; the driver reads every SQLite
// format (_timefmt=sqlite). Order comes from messages.seq, not times. The earlier driver (modernc) wrote Go's debug form in the
// server's zone ("2026-11-04 16:54:48.83 -0600 CST m=+2.03");
// convertTimes rewrites those once.

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
	_ "github.com/ncruces/go-sqlite3/embed"
	_ "github.com/ncruces/go-sqlite3/vfs/adiantum"
)

// TimeLayout is how times are stored (always UTC).
const TimeLayout = "2006-01-02 15:04:05"

// KeySize is a database key's length in bytes.
const KeySize = 32

// Key encrypts a database: 32 random bytes (a key file) or a passphrase
// (stretched with Argon2id by the cipher). Zero: not encrypted.
type Key struct {
	Raw        []byte
	Passphrase string
}

// Encrypted reports whether k encrypts.
func (k Key) Encrypted() bool { return len(k.Raw) > 0 || k.Passphrase != "" }

// ErrWrongKey: the database can't be read with the key given (or needs one).
var ErrWrongKey = errors.New("the database can't be read: wrong key, or it's encrypted and no key was given")

// dataSource is the driver's URI for path.
func dataSource(path string, key Key) string {
	q := url.Values{}
	q.Set("_timefmt", "sqlite") // read any SQLite format, write TimeLayout in UTC
	// The key must be the first PRAGMA.
	switch {
	case len(key.Raw) > 0:
		q.Set("vfs", "adiantum")
		q.Add("_pragma", "hexkey('"+hex.EncodeToString(key.Raw)+"')")
	case key.Passphrase != "":
		q.Set("vfs", "adiantum")
		q.Add("_pragma", "textkey('"+strings.ReplaceAll(key.Passphrase, "'", "''")+"')")
	}
	q.Add("_pragma", "busy_timeout(60000)")
	// Foreign keys stay off, as they always were in practice: the old driver
	// ignored the "_foreign_keys=on" it was given, so existing data was never
	// checked against them. (This SQLite build turns them on by default.)
	q.Add("_pragma", "foreign_keys(0)")
	q.Add("_pragma", "journal_mode(wal)")
	q.Add("_pragma", "temp_store(memory)") // temporary files stay off disk
	return uriPath(path) + "?" + q.Encode()
}

// uriPath is path as an SQLite URI ("file:…", escaped where URIs need it).
func uriPath(path string) string {
	p := filepath.ToSlash(path)
	if filepath.VolumeName(path) != "" { // C:/… on Windows
		p = "/" + p
	}
	p = strings.NewReplacer("%", "%25", "?", "%3F", "#", "%23").Replace(p)
	if strings.HasPrefix(p, "/") {
		return "file://" + p
	}
	return "file:" + p
}

// plainTarget is a URI for writing an unencrypted copy (VACUUM INTO, for
// backups, which encrypt it themselves).
func plainTarget(path string) string {
	return uriPath(path) + "?vfs=os"
}

// checkReadable reads the database's first page, so a wrong key fails here
// and not partway through a migration.
func checkReadable(db *sql.DB, key Key) error {
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_master`).Scan(&n); err != nil {
		if strings.Contains(err.Error(), "not a database") || strings.Contains(err.Error(), "encrypted") {
			return ErrWrongKey
		}
		return fmt.Errorf("failed to open database: %w", err)
	}
	return nil
}

// convertTimes rewrites times stored in another format (the old driver's,
// or SQLite's CURRENT_TIMESTAMP without milliseconds) as TimeLayout in UTC,
// in every DATE/DATETIME/TIMESTAMP column. It's run on every open and does
// nothing once everything is converted (one query per column).
func convertTimes(db *DB) error {
	type col struct{ table, name string }
	var cols []col
	rows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND sql NOT LIKE 'CREATE VIRTUAL%'`)
	if err != nil {
		return err
	}
	var tables []string
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return err
		}
		tables = append(tables, t)
	}
	rows.Close()
	for _, t := range tables {
		info, err := db.Query(`SELECT name, upper(type) FROM pragma_table_info(?)`, t)
		if err != nil {
			return err
		}
		for info.Next() {
			var name, typ string
			if err := info.Scan(&name, &typ); err != nil {
				info.Close()
				return err
			}
			if typ == "DATETIME" || typ == "TIMESTAMP" || typ == "DATE" {
				cols = append(cols, col{t, name})
			}
		}
		info.Close()
	}

	for _, c := range cols {
		// Only text that isn't already in the stored form (19 characters,
		// a space after the date); one connection, so read everything before
		// writing.
		q := fmt.Sprintf(`SELECT rowid, CAST(%s AS TEXT) FROM %s WHERE typeof(%s) = 'text' AND NOT (length(%s) = 19 AND substr(%s, 11, 1) = ' ')`,
			quoteIdent(c.name), quoteIdent(c.table), quoteIdent(c.name), quoteIdent(c.name), quoteIdent(c.name))
		rows, err := db.Query(q)
		if err != nil {
			return fmt.Errorf("%s.%s: %w", c.table, c.name, err)
		}
		type fix struct {
			rowid int64
			val   string
		}
		var fixes []fix
		for rows.Next() {
			var id int64
			var v string
			if err := rows.Scan(&id, &v); err != nil {
				rows.Close()
				return err
			}
			if t, ok := parseStoredTime(v); ok {
				fixes = append(fixes, fix{id, t.UTC().Format(TimeLayout)})
			}
		}
		rows.Close()
		if len(fixes) == 0 {
			continue
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		upd := fmt.Sprintf(`UPDATE %s SET %s = ? WHERE rowid = ?`, quoteIdent(c.table), quoteIdent(c.name))
		for _, f := range fixes {
			if _, err := tx.Exec(upd, f.val, f.rowid); err != nil {
				tx.Rollback()
				return fmt.Errorf("%s.%s: %w", c.table, c.name, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		log.Printf("[MIGRATION] %s.%s: %d times stored as UTC", c.table, c.name, len(fixes))
	}
	return nil
}

// Layouts times were stored in before (Go's time.String without the
// monotonic reading, RFC 3339, and SQLite's own).
var oldTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02",
}

// parseStoredTime reads a time in any format it was stored in. Times
// without a zone are UTC (SQLite's rule).
func parseStoredTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " m="); i > 0 { // Go's monotonic clock reading
		s = s[:i]
	}
	for _, layout := range oldTimeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ConvertedCopyTo writes a copy of the whole database to path (which must
// not exist), encrypted with key, or unencrypted with a zero key. Used to
// encrypt or decrypt a database (concord-server --encrypt-database).
func (db *DB) ConvertedCopyTo(path string, key Key) error {
	q := url.Values{}
	switch {
	case len(key.Raw) > 0:
		q.Set("vfs", "adiantum")
		q.Set("hexkey", hex.EncodeToString(key.Raw))
	case key.Passphrase != "":
		q.Set("vfs", "adiantum")
		q.Set("textkey", key.Passphrase)
	default:
		q.Set("vfs", "os")
	}
	if _, err := db.Exec(`VACUUM INTO ?`, uriPath(path)+"?"+q.Encode()); err != nil {
		return fmt.Errorf("copying the database: %w", err)
	}
	return nil
}

// quoteIdent quotes an SQL identifier the way SQLite expects: in double
// quotes, with any double quote doubled. Go's %q escapes differently, which
// was safe only because these names come from the schema itself (security
// audit 2026-11-05, finding 17).
func quoteIdent(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}
