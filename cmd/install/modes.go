package main

import (
	"fmt"
	"math/rand"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/concord-chat/concord/internal/installer"
)

// What the installer was started to do. Install asks everything; the
// others work on what's recorded as installed (~/.concord/install.json)
// and are what Concord's Updates page (Ctrl+U) runs.
const (
	modeInstall   = "install"
	modeUpdate    = "update"    // straight to the checklist: the newest release, settings kept
	modeConfigure = "configure" // the questions again, starting from the current settings
	modeUninstall = "uninstall" // remove everything, after typing "uninstall"
)

// farewellQuips play while Concord is being removed.
var farewellQuips = []string{
	"Corking the last bottles…", "Packing up the vineyard…", "Folding the tablecloths…", "Saying goodbye to the grapes…",
	"Turning off the cellar lights…", "Rolling up the trellis…", "Sweeping up the leaves…",
}

// begin puts the model on the right first screen for its mode.
func (m *model) begin() {
	switch m.mode {
	case modeUpdate:
		m.initCmd = m.startInstall()
	case modeConfigure:
		m.initCmd = m.toForm("Change anything you like. Everything else stays as it is.")
	case modeUninstall:
		if m.confirmed {
			m.initCmd = m.startInstall()
		} else {
			m.initCmd = m.toConfirm()
		}
	}
}

// --- confirming an uninstall ---------------------------------------------------------

func (m *model) toConfirm() tea.Cmd {
	var list []string
	for _, c := range m.plan.Components {
		what := map[string]string{
			installer.Client: "the client",
			installer.Server: "the server, with its database, plugins and members' accounts",
			installer.Hub:    "the hub, with its listings",
		}[c]
		list = append(list, fmt.Sprintf("  • %s (%s)", what, installer.Tilde(m.plan.Dir(c))))
	}
	list = append(list, "  • your profiles, saved sign-ins and settings ("+installer.Tilde(installer.ConfigDir(m.plan.Platform.Home))+")")
	b := &formBuilder{}
	b.group(
		huh.NewInput().
			Title("Type uninstall to remove all of this").
			Description(strings.Join(list, "\n") + "\n\nThis can't be undone.").
			Validate(func(s string) error {
				if m.back || strings.EqualFold(strings.TrimSpace(s), "uninstall") {
					return nil
				}
				return fmt.Errorf("type uninstall, or press Ctrl+C to keep Concord")
			}),
	)
	m.form = b.form(m.contentWidth()).WithShowHelp(true)
	m.go_(stConfirm)
	return m.form.Init()
}

func (m *model) updateConfirm(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateAborted:
		m.aborted = true
		return m, tea.Quit
	case huh.StateCompleted:
		return m, m.startInstall()
	}
	return m, cmd
}

func (m *model) viewConfirm() string {
	return sBad.Bold(true).Render("Remove Concord from this computer?") + "\n\n" + m.form.View()
}

// quip picks the next line to play under the checklist.
func (m *model) nextQuip() string {
	if m.mode == modeUninstall {
		return farewellQuips[rand.Intn(len(farewellQuips))]
	}
	return quips[rand.Intn(len(quips))]
}

// farewell is printed after an uninstall.
func (m *model) farewell() string {
	var b strings.Builder
	b.WriteString("\n🍇 " + sGood.Bold(true).Render("Concord has been removed.") + "\n")
	if m.plan.DryRun {
		b.WriteString(sYellow.Render("   (dry run: nothing was changed)") + "\n")
	}
	b.WriteString(sDim.Render("   Thanks for growing with us. The vineyard's always here if you want to come back:") + "\n")
	b.WriteString("   " + sCode.Render("https://github.com/"+installer.Repo) + "\n")
	return b.String()
}
