package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/installer"
)

// modeModel is the installer started in a mode, as main does, against a
// temporary home where a server and hub are recorded as installed.
func modeModel(t *testing.T, mode string, confirmed bool) *model {
	home, src := t.TempDir(), t.TempDir()
	p := installer.Detect()
	p.Home = home
	setup := installer.NewPlan(p)
	setup.ServerDir, setup.HubDir = filepath.Join(home, "srv"), filepath.Join(home, "hub")
	setup.ServerName, setup.ServerPort, setup.TermsAccepted = "Grape Hall", "9090", true
	b, _ := setup.ServerConfigTOML("https://hub.example")
	os.MkdirAll(setup.ServerDir, 0o755)
	os.WriteFile(filepath.Join(setup.ServerDir, installer.ServerConfigFile), b, 0o600)
	rec := &installer.Record{Components: map[string]installer.RecordEntry{
		installer.Server: {Dir: setup.ServerDir, Start: installer.StartNever},
		installer.Hub:    {Dir: setup.HubDir, Start: installer.StartNever},
	}}
	if err := rec.Save(home); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"server", "hub"} {
		os.WriteFile(filepath.Join(src, p.Exe("concord-"+c)), []byte("stand-in"), 0o755)
	}

	plan := installer.NewPlan(p)
	plan.Source, plan.DryRun = src, true
	if !prepare(plan, mode) {
		t.Fatal("prepare found nothing installed")
	}
	m := newModel(plan, false)
	m.mode, m.confirmed = mode, confirmed
	m.begin()
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// runToEnd pumps the install's events until it stops.
func runToEnd(t *testing.T, m *model) tea.Cmd {
	cmd := m.listen()
	deadline := time.Now().Add(20 * time.Second)
	for m.st == stInstall && !m.done && time.Now().Before(deadline) {
		_, next := m.Update(cmd())
		if m.st != stInstall || m.done {
			return next
		}
		cmd = m.listen()
	}
	return nil
}

func TestUpdateModeGoesStraightToTheChecklist(t *testing.T) {
	m := modeModel(t, modeUpdate, false)
	if m.st != stInstall || !strings.Contains(plain(m.View()), "Updating Concord") {
		t.Fatalf("update didn't start the checklist:\n%s", plain(m.View()))
	}
	if !m.plan.Has(installer.Server) || !m.plan.Has(installer.Hub) || m.plan.Has(installer.Client) {
		t.Fatalf("update isn't working on what's installed: %v", m.plan.Components)
	}
	runToEnd(t, m)
	if m.st != stParty {
		t.Fatalf("ended at %v: %v", m.st, m.failed)
	}
	if cmd := m.toAfter(); cmd == nil {
		t.Fatal("an update shouldn't ask the next-steps questions")
	}
	if !strings.Contains(strings.Join(m.logs, "\n"), "kept your existing settings") {
		t.Fatalf("settings weren't kept:\n%s", strings.Join(m.logs, "\n"))
	}
}

func TestConfigureModeStartsFromTheCurrentSettings(t *testing.T) {
	m := modeModel(t, modeConfigure, false)
	if m.st != stForm || !m.plan.Reconfigure || m.plan.ServerName != "Grape Hall" || m.plan.ServerPort != "9090" {
		t.Fatalf("configure didn't load the settings: %v %+v", m.st, m.plan)
	}
	if v := plain(m.View()); !strings.Contains(v, "Everything else stays as it is") {
		t.Fatalf("configure's form:\n%s", v)
	}
}

func TestUninstallAsksThenRemoves(t *testing.T) {
	m := modeModel(t, modeUninstall, false)
	v := plain(m.View())
	if m.st != stConfirm || !strings.Contains(v, "Type uninstall") || !strings.Contains(v, "database") {
		t.Fatalf("uninstall didn't ask first:\n%s", v)
	}
	typeText(m, "nope")
	press(m, enter)
	if m.st != stConfirm {
		t.Fatal("removed without the word")
	}
	m = modeModel(t, modeUninstall, true) // confirmed in Concord
	if m.st != stInstall || !strings.Contains(plain(m.View()), "Removing Concord") {
		t.Fatalf("a confirmed uninstall didn't start:\n%s", plain(m.View()))
	}
	if cmd := runToEnd(t, m); cmd == nil || m.failed != nil {
		t.Fatalf("uninstall didn't finish: %v", m.failed)
	}
	logs := strings.Join(m.logs, "\n")
	for _, want := range []string{"would remove " + m.plan.ServerDir, "would remove " + m.plan.HubDir, ".concord"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("uninstall missed %q:\n%s", want, logs)
		}
	}
	if _, err := os.Stat(m.plan.ServerDir); err != nil {
		t.Fatal("a dry run removed something")
	}
}
