package server

// Encrypted backups (Privacy Pack 1, 2026-11-05). A backup is one file,
// concord-backup-<UTC>.tar.gz.age: a gzipped tar of the database snapshot,
// the settings file, the plugin secrets key and the plugins' data, encrypted
// with age (https://age-encryption.org) to the server's backup key. The key
// (an age identity, backup.key beside the database) is never put inside a
// backup and never logged: administrators see it in Server Settings › About,
// and `concord-server --backup-key` prints it. Anyone holding it can open a
// backup with `concord-server --open-backup`, or with the age tool itself:
//
//	age -d -i backup.key concord-backup-….tar.gz.age | tar xz

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
)

const (
	backupKeyFile  = "backup.key"
	backupFileExt  = ".tar.gz.age"
	backupDBName   = "concord.db"
	backupDataDir  = "PluginData"
	backupKeyIntro = "# Concord backup key: opens this server's backups. Keep a copy somewhere safe;\n# without it the backups can't be read. Never share it.\n"
)

// backupKeyPath is the backup key's file, beside the database.
func backupKeyPath(dbPath string) string {
	return filepath.Join(filepath.Dir(dbPath), backupKeyFile)
}

// loadBackupKey reads the key at path; made reports a new one was written.
func loadBackupKey(path string) (id *age.X25519Identity, made bool, err error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		id, err = age.GenerateX25519Identity()
		if err != nil {
			return nil, false, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, false, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return nil, false, err
		}
		_, werr := fmt.Fprintf(f, "%s%s\n", backupKeyIntro, id.String())
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return id, true, werr
	}
	if err != nil {
		return nil, false, err
	}
	id, err = parseBackupKey(string(data))
	return id, false, err
}

// parseBackupKey reads an identity from a key file's text, or the key itself.
func parseBackupKey(text string) (*age.X25519Identity, error) {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "AGE-SECRET-KEY-") {
			return age.ParseX25519Identity(line)
		}
	}
	return nil, errors.New("no backup key (AGE-SECRET-KEY-…) found")
}

// backupKey is the server's backup key, made on first use.
func (s *Server) backupKey() (*age.X25519Identity, error) {
	path := backupKeyPath(s.config.DatabasePath)
	id, made, err := loadBackupKey(path)
	if err != nil {
		return nil, fmt.Errorf("the backup key (%s): %w", path, err)
	}
	if made {
		// Never the key itself: logs end up in places keys mustn't.
		Logger.Warn("Made a backup key: backups are encrypted with it. Keep a copy somewhere safe (administrators see it in Server Settings › About, or run concord-server --backup-key); without it backups can't be read.", "file", path)
	}
	return id, nil
}

// writeEncryptedBackup writes the backup to dst (created, 0600): the
// database snapshot at dbSnap, then each extra file and folder under its
// base name, as a gzipped tar encrypted to the key's recipient.
func writeEncryptedBackup(dst string, recipient age.Recipient, dbSnap string, extras []string) error {
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	enc, err := age.Encrypt(out, recipient)
	if err != nil {
		out.Close()
		return err
	}
	zw := gzip.NewWriter(enc)
	tw := tar.NewWriter(zw)
	err = addFileToTar(tw, dbSnap, backupDBName)
	for _, src := range extras {
		if err != nil {
			break
		}
		err = addPathToTar(tw, src, filepath.Base(src))
	}
	for _, c := range []io.Closer{tw, zw, enc, out} {
		if cerr := c.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

// addPathToTar adds a file, or a folder and everything in it; a missing
// path is skipped. SQLite files inside plugins' data are copied as they are
// (a game's table JSON or a board database mid-write may be a moment old).
func addPathToTar(tw *tar.Writer, src, name string) error {
	fi, err := os.Stat(src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.IsDir() {
		return addFileToTar(tw, src, name)
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		inTar := filepath.ToSlash(filepath.Join(name, rel))
		if d.IsDir() {
			return tw.WriteHeader(&tar.Header{Typeflag: tar.TypeDir, Name: inTar + "/", Mode: 0o700, ModTime: time.Now()})
		}
		if !d.Type().IsRegular() {
			return nil // sockets, links: not data
		}
		return addFileToTar(tw, p, inTar)
	})
}

func addFileToTar(tw *tar.Writer, src, name string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	if err := tw.WriteHeader(&tar.Header{Typeflag: tar.TypeReg, Name: filepath.ToSlash(name), Mode: 0o600, Size: fi.Size(), ModTime: fi.ModTime()}); err != nil {
		return err
	}
	_, err = io.CopyN(tw, f, fi.Size())
	return err
}

// OpenBackup decrypts a backup with the key in keyText (a key file's text,
// or the key) and unpacks it into dir, which must not exist yet. It returns
// what it wrote.
func OpenBackup(file, keyText, dir string) ([]string, error) {
	id, err := parseBackupKey(keyText)
	if err != nil {
		return nil, err
	}
	in, err := os.Open(file)
	if err != nil {
		return nil, err
	}
	defer in.Close()
	dec, err := age.Decrypt(in, id)
	if err != nil {
		return nil, fmt.Errorf("can't open it with this key (%w)", err)
	}
	zr, err := gzip.NewReader(dec)
	if err != nil {
		return nil, fmt.Errorf("not a Concord backup: %w", err)
	}
	if _, err := os.Stat(dir); err == nil {
		return nil, fmt.Errorf("%s already exists; pick a new folder", dir)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	var wrote []string
	tr := tar.NewReader(zr)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return wrote, fmt.Errorf("reading the backup: %w", err)
		}
		dst := filepath.Join(root, filepath.FromSlash(h.Name))
		if dst != root && !strings.HasPrefix(dst, root+string(filepath.Separator)) {
			return wrote, fmt.Errorf("the backup has a path outside its folder: %q", h.Name)
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o700); err != nil {
				return wrote, err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return wrote, err
			}
			f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
			if err != nil {
				return wrote, err
			}
			_, err = io.Copy(f, tr)
			if cerr := f.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return wrote, err
			}
			wrote = append(wrote, h.Name)
		}
	}
	return wrote, nil
}

// BackupsConfig is [backups] in concord-server.toml.
type BackupsConfig struct {
	// EveryHours takes an encrypted backup that often (0: only when an
	// administrator asks). The newest ten are kept. Default 24.
	EveryHours int `toml:"every_hours"`
}

// backupEvery is how often scheduled backups run (0: never).
func (s *Server) backupEvery() time.Duration {
	if s.config.Backups.EveryHours <= 0 {
		return 0
	}
	return time.Duration(s.config.Backups.EveryHours) * time.Hour
}

// firstBackupDelay: with no backup yet, the first one waits this long after
// starting (not straight away, so a server in a restart loop doesn't fill
// the folder).
const firstBackupDelay = 10 * time.Minute

// nextBackupAt is when the next scheduled backup is due (zero: none).
func (s *Server) nextBackupAt() time.Time {
	every := s.backupEvery()
	if every == 0 {
		return time.Time{}
	}
	backups := listBackups(s.backupsDir())
	if len(backups) == 0 {
		next := s.admin.startedAt.Add(firstBackupDelay)
		if retry := time.Unix(s.admin.backupRetryAt.Load(), 0); next.Before(retry) {
			next = retry
		}
		return next
	}
	next := backups[len(backups)-1].at.Add(every)
	if early := s.admin.startedAt.Add(firstBackupDelay); next.Before(early) {
		next = early
	}
	if retry := time.Unix(s.admin.backupRetryAt.Load(), 0); next.Before(retry) {
		next = retry // the last try failed
	}
	return next
}

// runScheduledBackups takes a backup whenever one is due.
func (s *Server) runScheduledBackups() {
	if s.backupEvery() == 0 {
		return
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for range t.C {
		if s.admin.stopping.Load() {
			return
		}
		next := s.nextBackupAt()
		if next.IsZero() || time.Now().Before(next) {
			continue
		}
		if text, err := s.backupNow(); err != nil {
			Logger.Warn("Scheduled backup failed", "error", err)
			s.admin.backupRetryAt.Store(time.Now().Add(time.Hour).Unix()) // not every minute
		} else {
			Logger.Info("Scheduled backup", "result", text)
		}
	}
}

// backupKeyText is the backup key, made if there's none yet.
func (s *Server) backupKeyText() (string, error) {
	id, err := s.backupKey()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// BackupKeyPath is where a server whose database is at dbPath keeps its
// backup key.
func BackupKeyPath(dbPath string) string { return backupKeyPath(dbPath) }

// BackupKeyText is the backup key of the server whose database is at
// dbPath (concord-server --backup-key). It isn't made here: a server makes
// it with its first backup.
func BackupKeyText(dbPath string) (string, error) {
	path := backupKeyPath(dbPath)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no backup key yet (%s): the server makes one with its first backup", path)
	}
	if err != nil {
		return "", err
	}
	id, err := parseBackupKey(string(data))
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	return id.String(), nil
}
