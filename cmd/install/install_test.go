package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/installer"
)

// testModel is the installer at a typical terminal size, installing every
// component into a temporary home from a folder of stand-in programs, as a
// dry run.
func testModel(t *testing.T) *model {
	home := t.TempDir()
	src := t.TempDir()
	p := installer.Detect()
	p.Home = home
	for _, c := range []string{"client", "server", "hub"} {
		if err := os.WriteFile(filepath.Join(src, p.Exe("concord-"+c)), []byte("stand-in"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	pl := installer.NewPlan(p)
	pl.Source, pl.DryRun = src, true
	m := newModel(pl, true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

func plain(s string) string { return ansi.Strip(s) }

func TestEveryStageDraws(t *testing.T) {
	m := testModel(t)
	if v := plain(m.View()); !strings.Contains(v, "press any key") && !strings.Contains(v, ";") {
		t.Fatalf("intro:\n%s", v)
	}
	m.stageAt = time.Now().Add(-3 * time.Second)
	if v := plain(m.View()); !strings.Contains(v, "██████╗") || !strings.Contains(v, "Chat that lives in your terminal") {
		t.Fatalf("intro after 3s:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.st != stForm {
		t.Fatal("a key didn't skip the intro")
	}
	v := plain(m.View())
	for _, want := range []string{"What do you want to install on this machine?", "Client", "Server", "Hub", "Choose"} {
		if !strings.Contains(v, want) {
			t.Fatalf("form missing %q:\n%s", want, v)
		}
	}
	m.plan.Components = []string{installer.Client, installer.Server, installer.Hub}
	m.toTerms()
	if v := plain(m.View()); !strings.Contains(v, "Server Terms") || !strings.Contains(v, "Accept") {
		t.Fatalf("terms:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !m.plan.TermsAccepted || m.st != stReview {
		t.Fatal("accepting the terms didn't go on to the review")
	}
	v = plain(m.View())
	for _, want := range []string{"Here's the plan", "Client", "Server", "Hub", "Install 🍇", "Change something"} {
		if !strings.Contains(v, want) {
			t.Fatalf("review missing %q:\n%s", want, v)
		}
	}
	t.Log("\n" + v)
}

// A dry run goes through every step to the celebration, the next-steps
// questions and the summary, without touching the computer.
func TestDryRunInstall(t *testing.T) {
	m := testModel(t)
	m.plan.Components = []string{installer.Client, installer.Server, installer.Hub}
	m.plan.TermsAccepted = true
	m.plan.ServerOnHub, m.plan.PublicHost = true, "chat.example.com"
	m.plan.Tidy()
	cmd := m.startInstall()
	deadline := time.Now().Add(20 * time.Second)
	for m.st == stInstall && time.Now().Before(deadline) {
		msg := cmd()
		_, cmd = m.Update(msg)
		if cmd == nil {
			cmd = m.listen()
		}
		_ = m.View()
	}
	if m.st != stParty {
		t.Fatalf("stopped at %v: %v\n%s", m.st, m.failed, plain(m.View()))
	}
	if v := plain(m.View()); !strings.Contains(v, "is ready.") {
		t.Fatalf("party:\n%s", v)
	}
	m.stageAt = time.Now().Add(-partyDur)
	m.Update(tickMsg(time.Now()))
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.st != stAfter {
		t.Fatalf("no next steps after the party (%v)", m.st)
	}
	if v := plain(m.View()); !strings.Contains(v, "Join the official Concord server?") {
		t.Fatalf("after:\n%s", v)
	}
	for _, c := range []string{installer.Client, installer.Server, installer.Hub} {
		if fileExistsT(m.plan.Binary(c)) {
			t.Fatalf("a dry run installed the %s", c)
		}
	}
	s := plain(m.summary())
	for _, want := range []string{"is ready", "dry run", "Client", "Server", "Hub", "Next steps", "concord"} {
		if !strings.Contains(s, want) {
			t.Fatalf("summary missing %q:\n%s", want, s)
		}
	}
	t.Log("\n" + strings.Join(m.logs, "\n"))
	t.Log("\n" + s)
}

func fileExistsT(p string) bool { _, err := os.Stat(p); return err == nil }
