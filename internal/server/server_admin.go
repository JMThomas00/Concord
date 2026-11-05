package server

// Server Settings › About: health, Back up now and Restart (OpServerAdmin).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// serverAdmin is what the About page needs from the Server itself (the
// handlers don't own the config, Grapevine or the process).
type serverAdmin interface {
	adminStatus() protocol.ServerStatusPayload
	backupNow() (string, error)
	backupKeyText() (string, error)
	requestRestart() error
	updateServer(tag string, progress func(string)) (string, error)
}

const (
	backupPrefix = "concord-backup-"
	backupLayout = "20060102-150405" // UTC
	keepBackups  = 10                // Back up now keeps the newest ten
)

// adminState is the Server's part of the About page.
type adminState struct {
	startedAt time.Time

	backupMu sync.Mutex
	// backupRetryAt (Unix seconds): a scheduled backup failed; not again before this.
	backupRetryAt atomic.Int64
	backingUp     atomic.Bool

	updateMu sync.Mutex
	updating atomic.Bool

	restarting atomic.Bool   // a restart was asked for: main starts the server again
	stopping   atomic.Bool   // Shutdown has started
	stopOnce   sync.Once     // Shutdown runs once, however many ask
	stopped    chan struct{} // closed when Shutdown has finished
}

func newAdminState() adminState {
	return adminState{startedAt: time.Now(), stopped: make(chan struct{})}
}

// HandleServerAdmin answers OpServerAdmin: the status for anyone who can
// manage the server, backups and restarts for administrators only (a
// restart drops everyone for a moment, and a backup holds every secret).
func (h *Handlers) HandleServerAdmin(c *Client, msg *protocol.Message) {
	var req protocol.ServerAdminRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if err := h.checkPermission(c.UserID, req.ServerID, models.PermissionManageServer); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, err.Error())
		return
	}
	if h.admin == nil {
		c.sendError(protocol.ErrorCodeServerError, "This server can't report its status")
		return
	}
	canAct := h.checkPermission(c.UserID, req.ServerID, models.PermissionAdministrator) == nil
	sendStatus := func() {
		st := h.admin.adminStatus()
		st.CanAct = canAct
		_ = h.dispatchTo(c, protocol.EventServerStatus, st)
	}
	send := func(r protocol.ServerAdminResult) {
		r.Action = req.Action
		_ = h.dispatchTo(c, protocol.EventServerAdminResult, r)
	}
	result := func(ok bool, text string) { send(protocol.ServerAdminResult{OK: ok, Message: text}) }

	switch req.Action {
	case protocol.ServerAdminStatus, "":
		go sendStatus()

	case protocol.ServerAdminBackup:
		if !canAct {
			result(false, "Only administrators can back up the server")
			return
		}
		admin := c.UserID
		go func() {
			text, err := h.admin.backupNow()
			if err != nil {
				Logger.Warn("Backup failed", "admin", admin, "error", err)
				result(false, err.Error())
			} else {
				Logger.Info("Backup taken", "admin", admin)
				result(true, text)
			}
			sendStatus()
		}()

	case protocol.ServerAdminBackupKey:
		if !canAct {
			result(false, "Only administrators can see the backup key")
			return
		}
		key, err := h.admin.backupKeyText()
		if err != nil {
			result(false, err.Error())
			return
		}
		Logger.Info("Backup key shown", "admin", c.UserID) // who, never the key
		send(protocol.ServerAdminResult{OK: true, Message: "The backup key: keep a copy somewhere safe", BackupKey: key})

	case protocol.ServerAdminRestart:
		if !canAct {
			result(false, "Only administrators can restart the server")
			return
		}
		Logger.Warn("Restart asked for from the client", "admin", c.UserID)
		if err := h.admin.requestRestart(); err != nil {
			result(false, err.Error())
			return
		}
		send(protocol.ServerAdminResult{OK: true, Restarting: true, Message: "Restarting: everyone reconnects by themselves in a few seconds"})

	case protocol.ServerAdminUpdate:
		if !canAct {
			result(false, "Only administrators can update the server")
			return
		}
		admin := c.UserID
		Logger.Warn("Update asked for from the client", "admin", admin, "tag", req.Tag)
		go func() {
			text, err := h.admin.updateServer(req.Tag, func(step string) {
				send(protocol.ServerAdminResult{OK: true, Busy: true, Message: step})
			})
			if err != nil {
				Logger.Warn("Update failed", "admin", admin, "error", err)
				result(false, "Not updated: "+err.Error())
				sendStatus()
				return
			}
			send(protocol.ServerAdminResult{OK: true, Restarting: true, Message: text})
		}()

	default:
		result(false, "Unknown action "+req.Action)
	}
}

// adminStatus gathers the About page's health.
func (s *Server) adminStatus() protocol.ServerStatusPayload {
	st := protocol.ServerStatusPayload{
		StartedAt: s.admin.startedAt,
		Now:       time.Now(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Members:   s.db.GetTotalMemberCount(),
		Online:    s.hub.PeopleOnlineCount(),
		RunsAs:    runsAs(),
		BackingUp: s.admin.backingUp.Load(),
	}
	if ch, msgs, err := s.db.ContentCounts(); err == nil {
		st.Channels, st.Messages = ch, msgs
	}
	for _, suffix := range []string{"", "-wal"} {
		if fi, err := os.Stat(s.config.DatabasePath + suffix); err == nil {
			st.DatabaseBytes += fi.Size()
		}
	}
	if list, err := s.db.ListInstalledPlugins(); err == nil {
		st.PluginsInstalled = len(list)
		for _, p := range list {
			if p.Status == models.PluginStatusRunning {
				st.PluginsRunning++
			}
		}
	}
	if s.grapevine != nil {
		st.Grapevine = s.grapevine.status()
	} else if s.config.Grapevine.Enabled {
		st.Grapevine = protocol.ServerGrapevineStatus{Enabled: true, HubURL: s.config.Grapevine.HubURL}
	}

	dir := s.backupsDir()
	if abs, err := filepath.Abs(dir); err == nil {
		st.BackupsDir = abs
	} else {
		st.BackupsDir = dir
	}
	backups := listBackups(dir)
	st.BackupCount = len(backups)
	if n := len(backups); n > 0 {
		last := backups[n-1]
		st.LastBackupAt = last.at
		st.LastBackupBytes = backupSize(dir, last)
		st.LastBackupEncrypted = !last.dir
	}
	if fi, err := os.Stat(backupKeyPath(s.config.DatabasePath)); err == nil && !fi.IsDir() {
		st.BackupKeyMade = true
	}
	st.BackupEvery = s.backupEvery().String()
	if next := s.nextBackupAt(); !next.IsZero() {
		st.NextBackupAt = next
	}

	st.CanRestart, st.RestartNote = s.canRestart()
	st.UpdateHow, st.UpdateNote = s.updateHow()
	st.Updating = s.admin.updating.Load()
	return st
}

// backupsDir is beside the database, so in Docker it's on the data volume.
func (s *Server) backupsDir() string {
	return filepath.Join(filepath.Dir(s.config.DatabasePath), "backups")
}

type backupEntry struct {
	name string
	at   time.Time
	dir  bool // a folder from before v0.2.0 (not encrypted)
}

// listBackups finds finished backups (oldest first): encrypted files, and
// the folders older versions wrote.
func listBackups(dir string) []backupEntry {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []backupEntry
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, backupPrefix) {
			continue
		}
		stamp := strings.TrimPrefix(name, backupPrefix)
		if !e.IsDir() {
			if !strings.HasSuffix(name, backupFileExt) {
				continue // a .partial one
			}
			stamp = strings.TrimSuffix(stamp, backupFileExt)
		}
		at, err := time.ParseInLocation(backupLayout, stamp, time.UTC)
		if err != nil {
			continue // a .partial one, or someone else's file
		}
		out = append(out, backupEntry{name, at, e.IsDir()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// backupSize is a backup's size on disk (a folder's database, for old ones).
func backupSize(dir string, b backupEntry) int64 {
	p := filepath.Join(dir, b.name)
	if b.dir {
		p = filepath.Join(p, backupDBName)
	}
	if fi, err := os.Stat(p); err == nil {
		return fi.Size()
	}
	return 0
}

// backupNow writes backups/concord-backup-<UTC time>.tar.gz.age: the
// database (a consistent snapshot), the settings file, the plugin secrets
// key and the plugins' data, encrypted with the backup key (backup_crypt.go).
// Everything needed to bring the server back but the plugins themselves,
// which install again from their source. It keeps the newest ten.
func (s *Server) backupNow() (string, error) {
	if !s.admin.backupMu.TryLock() {
		return "", fmt.Errorf("a backup is already running")
	}
	defer s.admin.backupMu.Unlock()
	s.admin.backingUp.Store(true)
	defer s.admin.backingUp.Store(false)

	key, err := s.backupKey()
	if err != nil {
		return "", err
	}
	dir := s.backupsDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("making %s: %w", dir, err)
	}
	now := time.Now().UTC()
	name := backupPrefix + now.Format(backupLayout) + backupFileExt
	final := filepath.Join(dir, name)
	if _, err := os.Stat(final); err == nil {
		return "", fmt.Errorf("a backup was taken this second; try again")
	}
	partial := final + ".partial"
	snap := filepath.Join(dir, ".snapshot-"+now.Format(backupLayout)+".db")
	_ = os.Remove(partial)
	_ = os.Remove(snap)
	defer os.Remove(snap)
	fail := func(err error) (string, error) {
		_ = os.Remove(partial)
		return "", err
	}

	if err := s.db.BackupTo(snap); err != nil {
		return fail(err)
	}
	_ = os.Chmod(snap, 0o600) // VACUUM INTO makes it readable by everyone
	dataDir := filepath.Dir(s.config.DatabasePath)
	extras := []string{filepath.Join(dataDir, "plugin-secrets.key")}
	if s.configPath != "" {
		extras = append(extras, s.configPath)
	}
	if s.config.PluginsDir != "" {
		extras = append(extras, filepath.Join(filepath.Dir(s.config.PluginsDir), backupDataDir))
	}
	if err := writeEncryptedBackup(partial, key.Recipient(), snap, extras); err != nil {
		return fail(err)
	}
	if err := os.Rename(partial, final); err != nil {
		return fail(err)
	}

	backups := listBackups(dir)
	for len(backups) > keepBackups {
		_ = os.RemoveAll(filepath.Join(dir, backups[0].name))
		backups = backups[1:]
	}

	if abs, err := filepath.Abs(final); err == nil {
		Logger.Info("Backed up (encrypted)", "to", abs)
	}
	// The page shows the backups folder already, so the file's name is enough.
	size := ""
	if fi, err := os.Stat(final); err == nil {
		size = " (" + humanBytes(fi.Size()) + ")"
	}
	return "Backed up as " + name + size + ", encrypted", nil
}

// copyIfExists copies a file readable only by its owner; a missing source
// is skipped.
func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d bytes", n)
}

// canRestart: the full-screen dashboard owns the terminal and can't hand
// it to a new copy; everything else can (see relaunch).
func (s *Server) canRestart() (bool, string) {
	if s.dashboardDisplay == dashboardDisplayFull {
		return false, "Started with --dashboard: restart it there"
	}
	return true, ""
}

// requestRestart stops the server cleanly (like Ctrl+C), after which
// cmd/server starts it again (Relaunch). The reply to the admin goes out
// first: the stop starts a moment later.
func (s *Server) requestRestart() error {
	if ok, why := s.canRestart(); !ok {
		return fmt.Errorf("%s", why)
	}
	if !s.admin.restarting.CompareAndSwap(false, true) {
		return fmt.Errorf("already restarting")
	}
	go func() {
		time.Sleep(500 * time.Millisecond)
		Logger.Warn("Restarting server...")
		s.Shutdown()
	}()
	return nil
}

// RestartRequested reports whether Run returned because an admin asked
// for a restart; cmd/server then calls Relaunch.
func (s *Server) RestartRequested() bool { return s.admin.restarting.Load() }

// runsAs works out how this server was started.
func runsAs() string {
	if fileExists("/.dockerenv") || fileExists("/run/.containerenv") {
		return protocol.RunsAsDocker
	}
	switch runtime.GOOS {
	case "linux":
		if os.Getenv("INVOCATION_ID") != "" { // systemd sets it for every unit it runs
			return protocol.RunsAsSystemd
		}
	case "darwin":
		if strings.HasPrefix(os.Getenv("XPC_SERVICE_NAME"), "chat.concord.") { // the installer's launchd label
			return protocol.RunsAsLaunchd
		}
	case "windows":
		if strings.HasSuffix(os.Getenv("USERNAME"), "$") { // the SYSTEM account: the installer's startup task
			return protocol.RunsAsWindows
		}
	}
	return protocol.RunsAsManual
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
