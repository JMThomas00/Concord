package server

// The database key (Privacy Pack 1, 2026-11-05). A new database is
// encrypted ([database] encrypt, default true) with a random key kept in a
// key file ([database] key_file), or with a passphrase typed at every start
// ([database] ask_passphrase). An existing unencrypted database keeps
// working as it is until `concord-server --encrypt-database` converts it.
//
// Where the key file goes by default: beside the settings file. The
// installer and the Docker setup point key_file somewhere outside the
// server's data folder (the user's config folder; a secrets folder that
// isn't the data volume), so a copied data folder or volume doesn't carry
// the key with it. A key on the same machine doesn't protect a seized
// server: only a typed passphrase does that.

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/database"
)

// DatabaseConfig is [database] in concord-server.toml.
type DatabaseConfig struct {
	// Encrypt: a new database is encrypted (default true). It changes
	// nothing about an existing one (see --encrypt-database).
	Encrypt *bool `toml:"encrypt,omitempty"`
	// KeyFile is where the key lives (default: concord-database.key beside
	// the settings file; the CONCORD_DATABASE_KEY_FILE environment variable
	// wins over both).
	KeyFile string `toml:"key_file,omitempty"`
	// AskPassphrase: no key file; a passphrase is typed at every start (and
	// so the server can't restart by itself).
	AskPassphrase bool `toml:"ask_passphrase,omitempty"`
}

func (d DatabaseConfig) encryptNew() bool { return d.Encrypt == nil || *d.Encrypt }

// DefaultDatabaseKeyName is the key file's name.
const DefaultDatabaseKeyName = "concord-database.key"

const dbKeyIntro = "# Concord database key: the server needs it to read its database.\n# Keep a copy somewhere safe and away from the server's data folder.\n# Without it the database can't be read. Never share it.\n"

// DatabaseKeyInfo says how the database is protected (Server Settings ›
// About shows it to administrators).
type DatabaseKeyInfo struct {
	Encrypted  bool
	KeyFile    string // "" with a passphrase, or when not encrypted
	Passphrase bool
}

// databaseKeyPath is where the key file is (configPath: the settings file).
func databaseKeyPath(config *Config, configPath string) string {
	// Docker: the key lives in a mounted folder that isn't the data volume
	// (docker-compose.yml).
	if env := strings.TrimSpace(os.Getenv("CONCORD_DATABASE_KEY_FILE")); env != "" {
		return env
	}
	if config.Database.KeyFile != "" {
		return config.Database.KeyFile
	}
	dir := filepath.Dir(config.DatabasePath)
	if configPath != "" {
		dir = filepath.Dir(configPath)
	}
	return filepath.Join(dir, DefaultDatabaseKeyName)
}

// sqliteHeader starts every unencrypted SQLite database.
var sqliteHeader = []byte("SQLite format 3\x00")

// databaseState: missing (or empty), plain, or encrypted (anything else).
func databaseState(path string) (exists, plain bool, err error) {
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	defer f.Close()
	head := make([]byte, len(sqliteHeader))
	n, _ := f.Read(head)
	if n == 0 {
		return false, false, nil // an empty file: SQLite would make a new database
	}
	return true, bytes.Equal(head[:n], sqliteHeader), nil
}

// ResolveDatabaseKey decides how to open the database: as it is if it's
// unencrypted; with the key file or a passphrase (ask, typed at the
// terminal) if it's encrypted; and for a new database, with a new key
// unless [database] encrypt = false. It never makes a new key for an
// existing encrypted database.
func ResolveDatabaseKey(config *Config, configPath string, ask func(prompt string, confirm bool) (string, error)) (database.Key, DatabaseKeyInfo, error) {
	exists, plain, err := databaseState(config.DatabasePath)
	if err != nil {
		return database.Key{}, DatabaseKeyInfo{}, err
	}
	if plain {
		return database.Key{}, DatabaseKeyInfo{}, nil
	}
	if !exists && !config.Database.encryptNew() {
		return database.Key{}, DatabaseKeyInfo{}, nil
	}
	if config.Database.AskPassphrase {
		if ask == nil {
			return database.Key{}, DatabaseKeyInfo{}, errors.New("[database] ask_passphrase is on, but there's no terminal to type it in")
		}
		prompt := "Database passphrase: "
		if !exists {
			prompt = "A passphrase for the new encrypted database (typed at every start; lose it and the database is lost): "
		}
		pass, err := ask(prompt, !exists)
		if err != nil {
			return database.Key{}, DatabaseKeyInfo{}, err
		}
		if len(pass) < 8 {
			return database.Key{}, DatabaseKeyInfo{}, errors.New("the passphrase must be at least 8 characters")
		}
		return database.Key{Passphrase: pass}, DatabaseKeyInfo{Encrypted: true, Passphrase: true}, nil
	}
	path := databaseKeyPath(config, configPath)
	raw, err := readDatabaseKey(path)
	switch {
	case err == nil:
	case errors.Is(err, fs.ErrNotExist) && !exists:
		if raw, err = writeNewDatabaseKey(path); err != nil {
			return database.Key{}, DatabaseKeyInfo{}, err
		}
		Logger.Warn("Made a database key: the database is encrypted with it. Keep a copy somewhere safe; without it the database can't be read.", "file", path)
	case errors.Is(err, fs.ErrNotExist):
		return database.Key{}, DatabaseKeyInfo{}, fmt.Errorf("the database is encrypted, but its key isn't at %s (set [database] key_file to where it is)", path)
	default:
		return database.Key{}, DatabaseKeyInfo{}, err
	}
	return database.Key{Raw: raw}, DatabaseKeyInfo{Encrypted: true, KeyFile: path}, nil
}

// readDatabaseKey reads a key file: the first line that isn't a comment,
// 64 hex digits.
func readDatabaseKey(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		raw, err := hex.DecodeString(line)
		if err != nil || len(raw) != database.KeySize {
			return nil, fmt.Errorf("%s isn't a database key (64 hex digits)", path)
		}
		return raw, nil
	}
	return nil, fmt.Errorf("%s has no key in it", path)
}

// writeNewDatabaseKey makes a key and writes it to path (0600; never over
// an existing file).
func writeNewDatabaseKey(path string) ([]byte, error) {
	raw := make([]byte, database.KeySize)
	if _, err := rand.Read(raw); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("the database key's folder: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, fmt.Errorf("writing the database key: %w", err)
	}
	_, werr := fmt.Fprintf(f, "%s%s\n", dbKeyIntro, hex.EncodeToString(raw))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	return raw, werr
}

// ConvertDatabase encrypts an unencrypted database (encrypt) or decrypts an
// encrypted one, with the server stopped: a converted copy is written
// beside it and checked, then swapped in; the old file is kept as
// concord.db.unencrypted-<time> (or .encrypted-<time>) until the admin
// deletes it. It returns what it did, to print.
func ConvertDatabase(config *Config, configPath string, encrypt bool, ask func(prompt string, confirm bool) (string, error)) (string, error) {
	path := config.DatabasePath
	exists, plain, err := databaseState(path)
	if err != nil {
		return "", err
	}
	if !exists {
		return "", fmt.Errorf("there's no database at %s", path)
	}
	if encrypt && !plain {
		return "", errors.New("the database is already encrypted")
	}
	if !encrypt && plain {
		return "", errors.New("the database isn't encrypted")
	}

	var from, to database.Key
	var info DatabaseKeyInfo
	if encrypt {
		c := *config
		c.Database.Encrypt = nil
		if !c.Database.AskPassphrase {
			if _, err := os.Stat(databaseKeyPath(&c, configPath)); err == nil {
				return "", fmt.Errorf("a key file is already at %s: move it away, or set [database] key_file, so a new key isn't mixed up with an old one", databaseKeyPath(&c, configPath))
			}
		}
		// Resolve as if the database were new: makes the key (or asks for
		// the passphrase twice).
		c.DatabasePath = path + ".not-there"
		to, info, err = ResolveDatabaseKey(&c, configPath, ask)
		if err != nil {
			return "", err
		}
	} else if from, _, err = ResolveDatabaseKey(config, configPath, ask); err != nil {
		return "", err
	}

	stamp := time.Now().UTC().Format("20060102-150405")
	tmp := path + ".converting-" + stamp
	db, err := database.Open(path, from)
	if err != nil {
		return "", err
	}
	_, before, _ := db.ContentCounts()
	err = db.ConvertedCopyTo(tmp, to)
	db.Close()
	if err != nil {
		os.Remove(tmp)
		return "", err
	}
	check, err := database.Open(tmp, to)
	if err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("the converted copy doesn't open: %w", err)
	}
	_, after, _ := check.ContentCounts()
	check.Close()
	if after != before {
		os.Remove(tmp)
		return "", fmt.Errorf("the converted copy holds %d messages, not %d; nothing was changed", after, before)
	}
	old := path + ".unencrypted-" + stamp
	if !encrypt {
		old = path + ".encrypted-" + stamp
	}
	if err := os.Rename(path, old); err != nil {
		os.Remove(tmp)
		return "", err
	}
	for _, ext := range []string{"-wal", "-shm"} {
		if _, err := os.Stat(path + ext); err == nil {
			_ = os.Rename(path+ext, old+ext)
		}
	}
	_ = os.Remove(tmp + "-wal")
	_ = os.Remove(tmp + "-shm")
	if err := os.Rename(tmp, path); err != nil {
		return "", fmt.Errorf("the converted database is at %s but couldn't be moved into place: %w", tmp, err)
	}
	if encrypt {
		where := "the passphrase you typed (needed at every start)"
		if info.KeyFile != "" {
			where = "the key in " + info.KeyFile + " (keep a copy somewhere safe)"
		}
		return fmt.Sprintf("Encrypted %s (%d messages) with %s.\nThe unencrypted original is kept as %s: delete it once the server runs fine.", path, after, where, old), nil
	}
	return fmt.Sprintf("Decrypted %s (%d messages).\nThe encrypted original is kept as %s.", path, after, old), nil
}
