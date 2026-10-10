package client

import (
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/installer"
)

// updatesApp is the Updates page for someone whose home has the installer's
// record of a client and a server.
func updatesApp(t *testing.T) *App {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	rec := &installer.Record{Components: map[string]installer.RecordEntry{
		installer.Client: {Dir: filepath.Join(home, "client"), Version: "v0.1.0"},
		installer.Server: {Dir: filepath.Join(home, "concord", "server"), Version: "v0.1.0", Start: installer.StartAtBoot},
	}}
	if err := rec.Save(home); err != nil {
		t.Fatal(err)
	}
	a := eggApp(t)
	a.clientVersion = "v0.1.0"
	a.openUpdates()
	a.handleUpdateChecked(updateCheckedMsg{latest: "v0.2.0"})
	return a
}

func TestUpdatesPageShowsWhatsInstalled(t *testing.T) {
	a := updatesApp(t)
	out := ansi.Strip(a.View())
	for _, want := range []string{"v0.2.0", "new!", "Client", "Server", "server", "Update", "Configure", "Uninstall"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestUpdatesActionsRunTheInstaller(t *testing.T) {
	a := updatesApp(t)
	t.Setenv("CONCORD_INSTALLER", "/path/to/concord-install")
	for key, action := range map[string]string{"u": "update", "c": "configure"} {
		cmd := a.handleUpdatesKey(keyOf(key))
		if cmd == nil || a.updates.busy == "" {
			t.Fatalf("%s did nothing", key)
		}
		if m, ok := cmd().(installerReadyMsg); !ok || m.action != action || m.path != "/path/to/concord-install" {
			t.Fatalf("%s: %+v", key, m)
		}
		a.updates.busy = ""
	}
}

// Uninstalling needs the word typed out; anything else keeps Concord.
func TestUninstallNeedsTheWord(t *testing.T) {
	a := updatesApp(t)
	t.Setenv("CONCORD_INSTALLER", "/path/to/concord-install")
	a.handleUpdatesKey(keyOf("x"))
	out := ansi.Strip(a.View())
	if !a.updates.confirming || !strings.Contains(out, "database") || !strings.Contains(out, "Type uninstall") {
		t.Fatalf("no confirmation:\n%s", out)
	}
	for _, r := range "nope" {
		a.handleUpdatesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if cmd := a.handleUpdatesKey(keyOf("enter")); cmd != nil || !a.updates.confirming {
		t.Fatal("uninstalled without the word")
	}
	a.handleUpdatesKey(keyOf("esc"))
	if a.updates.confirming {
		t.Fatal("Esc didn't keep Concord")
	}
	a.handleUpdatesKey(keyOf("x"))
	for _, r := range "uninstall" {
		a.handleUpdatesKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	cmd := a.handleUpdatesKey(keyOf("enter"))
	if m, ok := cmd().(installerReadyMsg); !ok || m.action != "uninstall" {
		t.Fatalf("the word didn't start the uninstall: %+v", m)
	}
	// Once it's done, Concord closes (it's gone).
	if a.installerDone(installerDoneMsg{action: "uninstall"}) == nil {
		t.Fatal("Concord didn't close after uninstalling")
	}
}

// After the client is updated, Enter restarts into the new version.
func TestUpdatedClientRestarts(t *testing.T) {
	a := updatesApp(t)
	rec := installer.LoadRecord(homeDir())
	e := rec.Components[installer.Client]
	e.Version = "v0.2.0"
	rec.Components[installer.Client] = e
	rec.Save(homeDir())
	a.installerDone(installerDoneMsg{action: "update"})
	if !a.updates.restart || !strings.Contains(ansi.Strip(a.View()), "Restart Concord") {
		t.Fatalf("no restart offered:\n%s", ansi.Strip(a.View()))
	}
	a.handleUpdatesKey(keyOf("enter"))
	if want := filepath.Join(e.Dir, installer.Detect().Exe("concord-client")); a.RestartPath() != want {
		t.Fatalf("restarts %q, want %q", a.RestartPath(), want)
	}
}

// Release candidates count: rc3 < rc4 < rc10 < the final release; local and
// "dev" builds never ask.
func TestNewerVersionKnowsReleaseCandidates(t *testing.T) {
	for _, c := range []struct {
		have, latest string
		newer        bool
	}{
		{"0.1.0-rc3", "v0.1.0-rc4", true},
		{"0.1.0-rc4", "v0.1.0-rc4", false},
		{"0.1.0-rc9", "v0.1.0-rc10", true},
		{"0.1.0-rc4", "v0.1.0", true},
		{"0.1.0", "v0.1.0-rc4", false},
		{"0.1.0", "v0.1.1", true},
		{"0.2.0", "v0.1.9", false},
		{"dev", "v0.1.0", false},
		{"", "v0.1.0", false},
	} {
		if got := newerVersion(c.have, c.latest); got != c.newer {
			t.Errorf("newerVersion(%q, %q) = %v, want %v", c.have, c.latest, got, c.newer)
		}
	}
}

// A newer release found at startup takes the login tip's place.
func TestTheLoginTipOffersAnUpdate(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	if tip := ansi.Strip(a.grapevineTip(120)); !strings.Contains(tip, "Ctrl+G") {
		t.Fatalf("tip %q", tip)
	}
	a.clientVersion = "0.1.0-rc3"
	a.handleStartupUpdate(startupUpdateMsg{latest: "v0.1.0-rc4"})
	tip := ansi.Strip(a.grapevineTip(120))
	if !strings.Contains(tip, "Client update available (v0.1.0-rc4): press") || !strings.Contains(tip, "Ctrl+U") || strings.Contains(tip, "Ctrl+G") {
		t.Fatalf("tip %q", tip)
	}
	a.updateAvailable = ""
	a.handleStartupUpdate(startupUpdateMsg{latest: "v0.1.0-rc3"})
	if a.updateAvailable != "" {
		t.Error("offered the version already running")
	}
	a.clientVersion = "dev"
	if a.startupUpdateCheck() != nil {
		t.Error("a dev build checks for updates")
	}
}
