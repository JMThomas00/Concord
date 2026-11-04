package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// adminAsk sends one OpServerAdmin request and returns the first answer of
// the wanted type, or an error (Type "").
func adminAsk(t *testing.T, wsURL, token string, req protocol.ServerAdminRequest, want protocol.EventType) *protocol.Message {
	t.Helper()
	c := newTestWSClient(t, wsURL)
	c.identify(token)
	c.send(protocol.OpServerAdmin, req)
	return c.readUntil(10*time.Second, func(m *protocol.Message) bool {
		return m.Op == protocol.OpDispatch && (m.Type == "" || m.Type == want)
	})
}

// adminServer: users[0] owns the server, users[1] has Manage Server, users[2]
// is a plain member.
func adminServer(t *testing.T) (srv *Server, wsURL string, s *models.Server, tokens []string) {
	t.Helper()
	srv, wsURL, users, tokens, _, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	all, err := srv.db.GetAllServers()
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range all {
		if x.OwnerID == users[0].ID {
			s = x
		}
	}
	managers := models.NewRole(s.ID, "Managers")
	managers.Permissions = models.PermissionManageServer
	if err := srv.db.CreateRole(managers); err != nil {
		t.Fatal(err)
	}
	srv.db.AddMemberRole(users[1].ID, s.ID, managers.ID)
	return srv, wsURL, s, tokens
}

func TestServerStatusAndBackup(t *testing.T) {
	srv, wsURL, s, tokens := adminServer(t)
	status := func(token string) (protocol.ServerStatusPayload, *protocol.Message) {
		m := adminAsk(t, wsURL, token, protocol.ServerAdminRequest{ServerID: s.ID, Action: protocol.ServerAdminStatus}, protocol.EventServerStatus)
		var st protocol.ServerStatusPayload
		if m != nil && m.Type == protocol.EventServerStatus {
			mustUnmarshal(t, m.Data, &st)
		}
		return st, m
	}

	// The owner sees the health and may act.
	st, m := status(tokens[0])
	if m == nil || m.Type != protocol.EventServerStatus {
		t.Fatalf("owner got %+v, want SERVER_STATUS", m)
	}
	if !st.CanAct || st.Channels < 1 || st.Messages < 1 || st.DatabaseBytes == 0 || st.StartedAt.IsZero() || st.OS == "" {
		t.Errorf("status = %+v", st)
	}
	if st.BackupCount != 0 || !st.CanRestart || st.RunsAs == "" {
		t.Errorf("before a backup: count %d, can restart %v, runs as %q", st.BackupCount, st.CanRestart, st.RunsAs)
	}

	// Manage Server sees it but can't act; a plain member gets nothing.
	if st, m := status(tokens[1]); m == nil || m.Type != protocol.EventServerStatus || st.CanAct {
		t.Errorf("manager: %+v, can act %v", m, st.CanAct)
	}
	if _, m := status(tokens[2]); m == nil || m.Type != "" {
		t.Errorf("plain member got %+v, want an error", m)
	}
	m = adminAsk(t, wsURL, tokens[1], protocol.ServerAdminRequest{ServerID: s.ID, Action: protocol.ServerAdminBackup}, protocol.EventServerAdminResult)
	var res protocol.ServerAdminResult
	mustUnmarshal(t, m.Data, &res)
	if res.OK {
		t.Errorf("a manager backed up: %+v", res)
	}

	// The owner backs up: a folder with the database, which opens and holds the message.
	m = adminAsk(t, wsURL, tokens[0], protocol.ServerAdminRequest{ServerID: s.ID, Action: protocol.ServerAdminBackup}, protocol.EventServerAdminResult)
	mustUnmarshal(t, m.Data, &res)
	if !res.OK {
		t.Fatalf("backup failed: %s", res.Message)
	}
	backups := listBackups(srv.backupsDir())
	if len(backups) != 1 {
		t.Fatalf("%d backups, want 1", len(backups))
	}
	dir := filepath.Join(srv.backupsDir(), backups[0].name)
	if _, err := os.Stat(filepath.Join(dir, "plugin-secrets.key")); err != nil {
		t.Errorf("the secrets key wasn't backed up: %v", err)
	}
	if fi, err := os.Stat(filepath.Join(dir, "concord.db")); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("backed-up database: %v, mode %v (want 0600: it holds every account)", err, fi.Mode())
	}
	copyDB, err := database.New(filepath.Join(dir, "concord.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	if _, msgs, err := copyDB.ContentCounts(); err != nil || msgs < 1 {
		t.Errorf("backup holds %d messages (%v)", msgs, err)
	}
	if st, _ := status(tokens[0]); st.BackupCount != 1 || st.LastBackupBytes == 0 || st.LastBackupAt.IsZero() {
		t.Errorf("after a backup: %+v", st)
	}
}

// Back up now keeps the newest ten, and ignores unfinished or foreign folders.
func TestBackupsKeepTheNewestTen(t *testing.T) {
	srv, _ := startPlainTestServer(t)
	dir := srv.backupsDir()
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 12; i++ {
		os.MkdirAll(filepath.Join(dir, backupPrefix+old.Add(time.Duration(i)*time.Hour).Format(backupLayout)), 0o700)
	}
	os.MkdirAll(filepath.Join(dir, "mine"), 0o700)
	os.MkdirAll(filepath.Join(dir, backupPrefix+"20260101-000000.partial"), 0o700)
	if _, err := srv.backupNow(); err != nil {
		t.Fatal(err)
	}
	got := listBackups(dir)
	if len(got) != keepBackups {
		t.Fatalf("%d backups kept, want %d", len(got), keepBackups)
	}
	if !got[0].at.Equal(old.Add(3 * time.Hour)) {
		t.Errorf("oldest kept is %v, want the 4th oldest", got[0].at)
	}
	if _, err := os.Stat(filepath.Join(dir, "mine")); err != nil {
		t.Error("a folder that isn't a backup was removed")
	}
}

// Restart stops the server cleanly and says so; only administrators may.
func TestRestartStopsTheServerForMain(t *testing.T) {
	srv, wsURL, s, tokens := adminServer(t)
	m := adminAsk(t, wsURL, tokens[1], protocol.ServerAdminRequest{ServerID: s.ID, Action: protocol.ServerAdminRestart}, protocol.EventServerAdminResult)
	var res protocol.ServerAdminResult
	mustUnmarshal(t, m.Data, &res)
	if res.OK || srv.RestartRequested() {
		t.Fatalf("a manager restarted the server: %+v", res)
	}
	m = adminAsk(t, wsURL, tokens[0], protocol.ServerAdminRequest{ServerID: s.ID, Action: protocol.ServerAdminRestart}, protocol.EventServerAdminResult)
	mustUnmarshal(t, m.Data, &res)
	if !res.OK || !srv.RestartRequested() {
		t.Fatalf("restart: %+v", res)
	}
	select {
	case <-srv.admin.stopped:
	case <-time.After(10 * time.Second):
		t.Fatal("the server didn't stop")
	}
}

func TestRunsAsByHand(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	t.Setenv("XPC_SERVICE_NAME", "")
	if fileExists("/.dockerenv") || fileExists("/run/.containerenv") {
		t.Skip("running in a container")
	}
	if got := runsAs(); got != protocol.RunsAsManual && got != protocol.RunsAsWindows {
		t.Errorf("runsAs = %q", got)
	}
}

func mustUnmarshal(t *testing.T, data []byte, v any) {
	t.Helper()
	if err := json.Unmarshal(data, v); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
}
