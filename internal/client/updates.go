package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/installer"
)

// Updates (Ctrl+U on the login screen): checks GitHub for the newest
// Concord release, shows what the installer put on this computer
// (~/.concord/install.json), and runs the installer for it:
//
//	U  update the client, server and hub to the newest release
//	C  configure: the installer's questions again, from the current settings
//	X  uninstall everything (after typing "uninstall")
//
// It fetches concord-install for this computer from the release (checked
// against its published checksum) and hands it the terminal. After the
// client itself is updated, Enter restarts into the new version.
//
// For trying it without a release: CONCORD_INSTALLER=<a concord-install>
// uses that program, and CONCORD_INSTALL_FROM=<folder of binaries> installs
// from that folder.

const releasesURL = "https://api.github.com/repos/JMThomas00/Concord/releases/latest"

// startedFrom is this program's own file, read before anything can update
// it (afterwards, Linux reports the replaced file as deleted).
var startedFrom, _ = os.Executable()

type updateState struct {
	checking bool
	latest   string // the newest release's tag, "" if none
	url      string
	date     time.Time
	err      string

	record  *installer.Record
	busy    string // what's happening ("Fetching the installer…"), "" when idle
	notice  string // how the last action went
	noticeE bool   // ...badly
	restart bool   // the client was updated: Enter restarts it

	confirming bool // typing "uninstall"
	confirm    textinput.Model
}

type updateCheckedMsg struct {
	latest, url string
	date        time.Time
	err         string
}

// installerReadyMsg: concord-install is downloaded and ready to run.
type installerReadyMsg struct {
	path, action string
	err          error
}

// installerDoneMsg: the installer has handed the terminal back.
type installerDoneMsg struct {
	action string
	err    error
}

// openUpdates shows the Updates page and starts a check.
func (a *App) openUpdates() tea.Cmd {
	a.updates = &updateState{record: installer.LoadRecord(homeDir())}
	a.view = ViewUpdates
	return a.checkForUpdates()
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func (a *App) checkForUpdates() tea.Cmd {
	a.updates.checking, a.updates.err = true, ""
	return func() tea.Msg {
		client := &http.Client{Timeout: 8 * time.Second}
		req, _ := http.NewRequest("GET", releasesURL, nil)
		req.Header.Set("Accept", "application/vnd.github+json")
		resp, err := client.Do(req)
		if err != nil {
			return updateCheckedMsg{err: "Couldn't reach GitHub: " + err.Error()}
		}
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusNotFound {
			return updateCheckedMsg{} // no releases published yet
		}
		if resp.StatusCode != http.StatusOK {
			return updateCheckedMsg{err: "GitHub answered " + resp.Status}
		}
		var rel struct {
			TagName     string    `json:"tag_name"`
			HTMLURL     string    `json:"html_url"`
			PublishedAt time.Time `json:"published_at"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
			return updateCheckedMsg{err: "Couldn't read GitHub's answer"}
		}
		return updateCheckedMsg{latest: rel.TagName, url: rel.HTMLURL, date: rel.PublishedAt}
	}
}

func (a *App) handleUpdateChecked(m updateCheckedMsg) {
	if a.updates == nil {
		return
	}
	a.updates.checking = false
	a.updates.latest, a.updates.url, a.updates.date, a.updates.err = m.latest, m.url, m.date, m.err
}

// --- running the installer ------------------------------------------------------------

// fetchInstaller gets concord-install for this computer: the release's,
// checked against its checksum (or CONCORD_INSTALLER, for trying it out).
func (a *App) fetchInstaller(action string) tea.Cmd {
	a.updates.busy, a.updates.notice = "Fetching the installer…", ""
	return func() tea.Msg {
		if path := os.Getenv("CONCORD_INSTALLER"); path != "" {
			return installerReadyMsg{path: path, action: action}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		rel, err := installer.FetchRelease(ctx, "")
		if err != nil {
			return installerReadyMsg{action: action, err: err}
		}
		name := installer.Detect().InstallerName()
		asset, ok := rel.Assets[name]
		if !ok {
			return installerReadyMsg{action: action, err: fmt.Errorf("release %s has no installer for this computer (%s)", rel.Tag, name)}
		}
		dir, err := os.MkdirTemp("", "concord-install-*")
		if err != nil {
			return installerReadyMsg{action: action, err: err}
		}
		path, err := installer.Download(ctx, asset, dir, nil)
		if err == nil {
			err = os.Chmod(path, 0o755)
		}
		return installerReadyMsg{path: path, action: action, err: err}
	}
}

// runInstaller hands the terminal to the installer until it's done.
func (a *App) runInstaller(m installerReadyMsg) tea.Cmd {
	u := a.updates
	if u == nil {
		return nil
	}
	u.busy = ""
	if m.err != nil {
		u.notice, u.noticeE = "Couldn't get the installer: "+m.err.Error(), true
		return nil
	}
	args := []string{"--" + m.action, "--return"}
	if m.action == "uninstall" {
		args = append(args, "--yes") // asked here already
	}
	if from := os.Getenv("CONCORD_INSTALL_FROM"); from != "" {
		args = append(args, "--from", from)
	}
	action := m.action
	return tea.ExecProcess(exec.Command(m.path, args...), func(err error) tea.Msg {
		return installerDoneMsg{action: action, err: err}
	})
}

func (a *App) installerDone(m installerDoneMsg) tea.Cmd {
	u := a.updates
	if u == nil {
		return nil
	}
	if m.action == "uninstall" && m.err == nil {
		return tea.Quit // Concord is gone; the installer has said goodbye
	}
	before := u.record.Components[installer.Client].Version
	u.record = installer.LoadRecord(homeDir())
	switch {
	case m.err != nil:
		u.notice, u.noticeE = "The installer stopped before finishing. Nothing more was changed.", true
	case m.action == "update" && u.record.Components[installer.Client].Version != before:
		u.notice, u.noticeE, u.restart = "Updated to "+u.record.Components[installer.Client].Version+". Press Enter to restart Concord.", false, true
	case m.action == "update":
		u.notice, u.noticeE = "Everything's up to date.", false
	default:
		u.notice, u.noticeE = "Done: your new settings are in place.", false
	}
	return nil
}

// RestartPath is set when the client was updated and should start again:
// main runs it once the program has closed.
func (a *App) RestartPath() string { return a.restartPath }

func (a *App) restartClient() tea.Cmd {
	path := startedFrom
	if e, ok := a.updates.record.Components[installer.Client]; ok {
		path = filepath.Join(e.Dir, installer.Detect().Exe("concord-client"))
	}
	a.restartPath = path
	return tea.Quit
}

// --- keys -------------------------------------------------------------------------------------

func (a *App) handleUpdatesKey(msg tea.KeyMsg) tea.Cmd {
	u := a.updates
	if u.record == nil {
		u.record = &installer.Record{Components: map[string]installer.RecordEntry{}}
	}
	if u.confirming {
		switch msg.String() {
		case "esc":
			u.confirming = false
		case "enter":
			if strings.EqualFold(strings.TrimSpace(u.confirm.Value()), "uninstall") {
				u.confirming = false
				return a.fetchInstaller("uninstall")
			}
			u.notice, u.noticeE = "Type uninstall to remove everything, or Esc to keep Concord.", true
		default:
			var cmd tea.Cmd
			u.confirm, cmd = u.confirm.Update(msg)
			return cmd
		}
		return nil
	}
	if msg.String() == "ctrl+q" {
		return tea.Quit
	}
	if u.busy != "" {
		return nil
	}
	installed := len(u.record.Components) > 0
	switch msg.String() {
	case "esc":
		a.updates = nil
		a.view = ViewLogin
		a.initLoginView()
	case "enter":
		if u.restart {
			return a.restartClient()
		}
		if !u.checking {
			return a.checkForUpdates()
		}
	case "r":
		if !u.checking {
			return a.checkForUpdates()
		}
	case "u", "U":
		if installed {
			return a.fetchInstaller("update")
		}
	case "c", "C":
		if installed {
			return a.fetchInstaller("configure")
		}
	case "x", "X":
		if installed {
			u.confirming, u.notice = true, ""
			u.confirm = textinput.New()
			u.confirm.Placeholder = "uninstall"
			u.confirm.CharLimit = 20
			u.confirm.Focus()
			return textinput.Blink
		}
	}
	return nil
}

// newerVersion reports whether release tag b is newer than version a
// ("v0.1.2" style; anything unparsable compares as not newer).
func newerVersion(a, b string) bool {
	parse := func(v string) ([3]int, bool) {
		var out [3]int
		parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3)
		if len(parts) != 3 {
			return out, false
		}
		for i, p := range parts {
			p = strings.SplitN(p, "-", 2)[0]
			n, err := strconv.Atoi(p)
			if err != nil {
				return out, false
			}
			out[i] = n
		}
		return out, true
	}
	va, okA := parse(a)
	vb, okB := parse(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if vb[i] != va[i] {
			return vb[i] > va[i]
		}
	}
	return false
}

// --- the page -----------------------------------------------------------------------------------

func (a *App) renderUpdatesView() string {
	u := a.updates
	if u.record == nil {
		u.record = &installer.Record{Components: map[string]installer.RecordEntry{}}
	}
	if u.confirming {
		return a.renderUninstallConfirm()
	}
	c := a.theme.Colors
	bold := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground)).Bold(true)
	version := a.clientVersion
	if version == "" {
		version = "dev"
	}
	var b strings.Builder
	latest := ""
	switch {
	case u.checking:
		latest = a.dim("checking GitHub…")
	case u.err != "":
		latest = a.dim("couldn't check")
	case u.latest == "":
		latest = a.dim("no releases published yet")
	default:
		latest = bold.Render(u.latest)
		if !u.date.IsZero() {
			latest += a.dim("  (" + u.date.Local().Format("Jan 2, 2006") + ")")
		}
		if newerVersion(version, u.latest) {
			latest += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)).Render("new!")
		}
	}
	b.WriteString(a.dim("Latest    ") + latest + "\n\n")

	installed := u.record.Installed()
	if len(installed) == 0 {
		b.WriteString(a.dim("This client  ") + bold.Render(version) + "\n\n")
		b.WriteString(a.stageNotice("Updating from here needs Concord installed with its one-line installer (see the README).", false))
	} else {
		for _, comp := range installed {
			e := u.record.Components[comp]
			v := e.Version
			if comp == installer.Client && version != "dev" {
				v = version
			}
			if v == "" {
				v = "?"
			}
			b.WriteString(fmt.Sprintf("%s%s  %s\n", a.dim(fmt.Sprintf("%-8s", strings.ToUpper(comp[:1])+comp[1:])), bold.Render(fmt.Sprintf("%-10s", v)), a.dim(installer.Tilde(e.Dir))))
		}
		b.WriteString("\n")
	}
	switch {
	case u.busy != "":
		b.WriteString(a.dim(u.busy) + "\n")
	case u.notice != "":
		b.WriteString(a.stageNotice(u.notice, u.noticeE))
	case u.err != "":
		b.WriteString(a.stageNotice(u.err, true))
	}

	hints := []keyHint{{"Enter", "Check again"}, {"Esc", "Back"}}
	if u.restart {
		hints = []keyHint{{"Enter", "Restart Concord"}, {"Esc", "Back"}}
	} else if len(installed) > 0 {
		hints = []keyHint{{"U", "Update"}, {"C", "Configure"}, {"X", "Uninstall"}, {"Enter", "Check again"}, {"Esc", "Back"}}
	}
	return a.stagePage("Updates", "Keep Concord fresh", "fresh", b.String(), hints)
}

func (a *App) renderUninstallConfirm() string {
	u := a.updates
	var b strings.Builder
	b.WriteString(a.dim("This removes, for good:") + "\n")
	what := map[string]string{
		installer.Client: "the client",
		installer.Server: "the server, with its database, plugins and accounts",
		installer.Hub:    "the hub, with its listings",
	}
	for _, comp := range u.record.Installed() {
		b.WriteString(fmt.Sprintf("  • %s %s\n", what[comp], a.dim("("+installer.Tilde(u.record.Components[comp].Dir)+")")))
	}
	b.WriteString("  • your profiles, saved sign-ins and settings " + a.dim("("+installer.Tilde(installer.ConfigDir(homeDir()))+")") + "\n\n")
	b.WriteString("Type " + lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true).Render("uninstall") + " to remove Concord: " + u.confirm.View() + "\n")
	if u.notice != "" {
		b.WriteString(a.stageNotice(u.notice, u.noticeE))
	}
	return a.stagePage("Uninstall", "Say goodbye to Concord?", "goodbye", b.String(),
		[]keyHint{{"Enter", "Uninstall"}, {"Esc", "Keep Concord"}})
}

// String for logs.
func (u *updateState) String() string { return fmt.Sprintf("%+v", *u) }
