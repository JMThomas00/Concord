package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// aboutTestApp is Server Settings open on About, signed in as the owner of
// a server running version.
func aboutTestApp(t *testing.T, version string) *App {
	t.Helper()
	a := newLayoutTestApp(t, 160, 45)
	me := &models.User{ID: uuid.New(), Username: "owner"}
	a.activeConn = &ServerConnection{ServerVersion: version, ServerGitCommit: "abc1234", User: me}
	a.currentServer = &models.Server{ID: uuid.New(), OwnerID: me.ID}
	a.view = ViewServerManagement
	a.serverManagementState = &ServerManagementState{Categories: []string{"Channels", "Roles", "Members", "Messages", "Plugins", "About"}, SelectedCategory: 5}
	a.openServerAbout()
	return a
}

func sampleStatus(canAct bool) protocol.ServerStatusPayload {
	now := time.Now()
	return protocol.ServerStatusPayload{
		StartedAt: now.Add(-50 * time.Hour), Now: now, OS: "linux", Arch: "amd64",
		Members: 1204, Online: 7, Channels: 12, Messages: 34567, DatabaseBytes: 3 << 20,
		PluginsInstalled: 5, PluginsRunning: 4,
		Grapevine:  protocol.ServerGrapevineStatus{Enabled: true, HubURL: "https://grapevine.example", Listed: true, LastHeartbeat: now.Add(-20 * time.Second)},
		BackupsDir: "/srv/concord/backups", BackupCount: 2, LastBackupAt: now.Add(-time.Hour), LastBackupBytes: 2 << 20,
		RunsAs: protocol.RunsAsSystemd, CanRestart: true, CanAct: canAct,
	}
}

func giveStatus(t *testing.T, a *App, st protocol.ServerStatusPayload) {
	t.Helper()
	msg, err := protocol.NewDispatch(protocol.EventServerStatus, 1, st)
	if err != nil {
		t.Fatal(err)
	}
	a.handleServerAdminEvent(a.activeConn, msg)
}

func TestServerAboutShowsHealthAndAdminKeys(t *testing.T) {
	a := aboutTestApp(t, "v0.1.8")
	if !strings.Contains(ansi.Strip(a.renderServerAboutContent(120, 40)), "Asking the server") {
		t.Error("no waiting line before the status arrives")
	}
	giveStatus(t, a, sampleStatus(true))
	a.handleServerRelease(serverReleaseMsg{latest: "v0.1.8"})
	out := ansi.Strip(a.renderServerAboutContent(120, 40))
	for _, want := range []string{"Up to date", "2 days 2 h", "systemd service", "1,204 members", "7 online", "12 channels", "34,567 messages", "3.0 MB",
		"5 installed · 4 running", "Listed on https://grapevine.example", "/srv/concord/backups", "B back up now · K backup key · R restart"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	for i, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 122 { // every settings page's border sits outside its width
			t.Errorf("line %d is %d wide: %q", i, w, line)
		}
	}

	// Encrypted backups: scheduled, and K shows the key, C copies, K hides.
	st := sampleStatus(true)
	st.LastBackupEncrypted, st.BackupKeyMade, st.BackupEvery, st.NextBackupAt = true, true, "24h0m0s", time.Now().Add(5*time.Hour)
	giveStatus(t, a, st)
	out = ansi.Strip(a.renderServerAboutContent(120, 40))
	for _, want := range []string{"· encrypted", "every 24h · next", "K shows the key"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	res, _ := protocol.NewDispatch(protocol.EventServerAdminResult, 2, protocol.ServerAdminResult{Action: protocol.ServerAdminBackupKey, OK: true, BackupKey: "AGE-SECRET-KEY-1TEST"})
	a.handleServerAdminEvent(a.activeConn, res)
	if out := ansi.Strip(a.renderServerAboutContent(120, 40)); !strings.Contains(out, "AGE-SECRET-KEY-1TEST") {
		t.Errorf("the key isn't shown\n%s", out)
	}
	a.handleServerAboutKey("k")
	if out := ansi.Strip(a.renderServerAboutContent(120, 40)); strings.Contains(out, "AGE-SECRET-KEY-1TEST") {
		t.Error("K didn't hide the key")
	}

	// Without Administrator: the health, but no actions.
	giveStatus(t, a, sampleStatus(false))
	if out := ansi.Strip(a.renderServerAboutContent(120, 40)); strings.Contains(out, "back up now") {
		t.Error("actions offered to someone who can't use them")
	}
	if ok, _ := a.handleServerAboutKey("b"); !ok || a.serverManagementState.About.notice != "" {
		t.Error("B did something without Administrator")
	}
}

func TestServerAboutVersionLine(t *testing.T) {
	a := aboutTestApp(t, "v0.1.8")
	a.handleServerRelease(serverReleaseMsg{latest: "v0.1.9", url: "https://github.com/x/releases/v0.1.9", date: time.Now(), notes: "## What's new\n\n- Faster backups\n- Fewer bugs"})
	out := ansi.Strip(a.renderServerAboutContent(120, 40))
	for _, want := range []string{"v0.1.9 is out", "Faster backups", "Fewer bugs"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "What's new") {
		t.Error("a markdown heading was shown as a note")
	}

	dev := aboutTestApp(t, "dev")
	dev.handleServerRelease(serverReleaseMsg{latest: "v0.1.9"})
	if out := ansi.Strip(dev.renderServerAboutContent(120, 40)); !strings.Contains(out, "Built from source") {
		t.Errorf("a dev build:\n%s", out)
	}
}

// A server before v0.1.8 isn't asked (it would answer "Unknown operation").
func TestServerAboutOnAnOldServer(t *testing.T) {
	a := aboutTestApp(t, "v0.1.7")
	ab := a.serverManagementState.About
	if !ab.tooOld || ab.asking {
		t.Fatalf("tooOld %v, asking %v", ab.tooOld, ab.asking)
	}
	if out := ansi.Strip(a.renderServerAboutContent(120, 40)); !strings.Contains(out, "too old to report its health") {
		t.Errorf("no explanation:\n%s", out)
	}
	if !aboutTestApp(t, "dev").serverManagementState.About.asking {
		t.Error("a dev server wasn't asked")
	}
}

// R asks first; R again restarts, anything else cancels.
func TestServerAboutRestartNeedsTwoPresses(t *testing.T) {
	a := aboutTestApp(t, "v0.1.8")
	giveStatus(t, a, sampleStatus(true))
	ab := a.serverManagementState.About
	a.handleServerAboutKey("r")
	if !ab.confirmRestart {
		t.Fatal("R didn't ask first")
	}
	a.handleServerAboutKey("x")
	if ab.confirmRestart || ab.notice != "" {
		t.Fatal("another key didn't cancel")
	}
	a.handleServerAboutKey("r")
	a.handleServerAboutKey("R")
	if ab.notice != "Restarting…" {
		t.Fatalf("notice %q", ab.notice)
	}

	// The server's answer, then its return after reconnecting.
	res, _ := protocol.NewDispatch(protocol.EventServerAdminResult, 2, protocol.ServerAdminResult{Action: protocol.ServerAdminRestart, OK: true, Restarting: true, Message: "Restarting"})
	a.handleServerAdminEvent(a.activeConn, res)
	if !ab.restarting {
		t.Fatal("not waiting for the restart")
	}
	back := sampleStatus(true)
	back.StartedAt = time.Now()
	giveStatus(t, a, back)
	if ab.restarting || ab.notice != "The server is back" {
		t.Errorf("after coming back: restarting %v, notice %q", ab.restarting, ab.notice)
	}
}

func TestServerAboutUpdate(t *testing.T) {
	a := aboutTestApp(t, "v0.1.8")
	st := sampleStatus(true)
	st.UpdateHow = protocol.UpdateInPlace
	giveStatus(t, a, st)
	a.handleServerRelease(serverReleaseMsg{latest: "v0.1.9"})
	ab := a.serverManagementState.About
	out := ansi.Strip(a.renderServerAboutContent(120, 40))
	if !strings.Contains(out, "U to update and restart") || !strings.Contains(out, "U update ·") {
		t.Fatalf("update not offered:\n%s", out)
	}
	a.handleServerAboutKey("u")
	if !ab.confirmUpdate {
		t.Fatal("U didn't ask first")
	}
	a.handleServerAboutKey("U")
	if ab.notice != "Updating to v0.1.9…" {
		t.Fatalf("notice %q", ab.notice)
	}
	for _, r := range []protocol.ServerAdminResult{
		{Action: protocol.ServerAdminUpdate, OK: true, Busy: true, Message: "Downloading v0.1.9 (20 MB)…"},
		{Action: protocol.ServerAdminUpdate, OK: true, Restarting: true, Message: "Updated to v0.1.9: restarting"},
	} {
		msg, _ := protocol.NewDispatch(protocol.EventServerAdminResult, 3, r)
		a.handleServerAdminEvent(a.activeConn, msg)
		if ab.notice != r.Message || ab.noticeErr {
			t.Errorf("notice %q (error %v), want %q", ab.notice, ab.noticeErr, r.Message)
		}
	}
	if !ab.restarting {
		t.Error("not waiting for the restart")
	}

	// Up to date, not an administrator, or Docker: no U.
	a.handleServerRelease(serverReleaseMsg{latest: "v0.1.8"})
	if a.canUpdateServer() {
		t.Error("offered an update to the same version")
	}
	a.handleServerRelease(serverReleaseMsg{latest: "v0.1.9"})
	st.CanAct = false
	giveStatus(t, a, st)
	if a.canUpdateServer() {
		t.Error("offered to a non-administrator")
	}
	st.CanAct, st.UpdateHow, st.UpdateNote = true, protocol.UpdateDocker, "git pull && docker compose build && docker compose up -d"
	giveStatus(t, a, st)
	out = ansi.Strip(a.renderServerAboutContent(120, 40))
	if a.canUpdateServer() || !strings.Contains(out, "To update: git pull && docker compose build") {
		t.Errorf("docker:\n%s", out)
	}
}
