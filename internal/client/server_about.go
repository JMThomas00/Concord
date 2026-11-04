package client

// Server Settings › About: the server's version against the latest
// release, its health, and for administrators Back up now and Restart
// (OpServerAdmin, internal/server/server_admin.go).

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// serverAdminVersion is the first server version that answers OpServerAdmin.
const serverAdminVersion = "0.1.8"

// serverAboutState is the About page's state, kept while Server Settings is open.
type serverAboutState struct {
	conn     *ServerConnection // the server the page is about
	status   *protocol.ServerStatusPayload
	gotAt    time.Time // when status arrived (uptime keeps counting from it)
	asking   bool
	tooOld   bool // the server doesn't know OpServerAdmin
	canAsk   bool // owner or Manage Server
	release  updateCheckedMsg
	checking bool

	notice    string
	noticeErr bool

	confirmRestart bool
	confirmUpdate  bool
	restarting     bool // asked; waiting for the server to come back
}

// serverReleaseMsg is the latest release, for the About page (the Updates
// page has its own updateCheckedMsg).
type serverReleaseMsg updateCheckedMsg

// answersServerAdmin reports whether sc's server takes OpServerAdmin.
func answersServerAdmin(sc *ServerConnection) bool {
	if sc == nil {
		return false
	}
	sc.mu.RLock()
	v := strings.TrimPrefix(strings.TrimSpace(sc.ServerVersion), "v")
	sc.mu.RUnlock()
	return v == "dev" || versionAtLeast(v, serverAdminVersion)
}

// canManageServer: the owner, or a role with Manage Server (or Administrator).
func (a *App) canManageServer() bool {
	if a.activeConn == nil || a.activeConn.User == nil {
		return false
	}
	if a.currentServer != nil && a.currentServer.OwnerID == a.activeConn.User.ID {
		return true
	}
	return a.hasPermission(a.activeConn.User, models.PermissionManageServer)
}

// openServerAbout starts the page: asks the server for its status and
// GitHub for the latest release.
func (a *App) openServerAbout() tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}
	if s.About == nil || s.About.conn != a.activeConn {
		s.About = &serverAboutState{conn: a.activeConn}
	}
	ab := s.About
	ab.canAsk = a.canManageServer()
	ab.tooOld = !answersServerAdmin(a.activeConn)
	if ab.canAsk && !ab.tooOld {
		a.askServerStatus(protocol.ServerAdminStatus)
	}
	ab.checking = true
	return func() tea.Msg {
		m, _ := fetchLatestRelease().(updateCheckedMsg)
		return serverReleaseMsg(m)
	}
}

// askServerStatus sends one OpServerAdmin request for the active server.
func (a *App) askServerStatus(action string) { a.sendServerAdmin(action, "") }

// sendServerAdmin sends one OpServerAdmin request (tag: the release, for "update").
func (a *App) sendServerAdmin(action, tag string) {
	s := a.serverManagementState
	if s == nil || s.About == nil || a.activeConn == nil {
		return
	}
	if action == protocol.ServerAdminStatus {
		s.About.asking = true
	}
	msg, err := protocol.NewMessage(protocol.OpServerAdmin, protocol.ServerAdminRequest{ServerID: a.getActiveServerID(), Action: action, Tag: tag})
	if err != nil || a.activeConn.Connection == nil {
		return
	}
	_ = a.activeConn.Connection.Send(msg)
}

// aboutFor is the open About page, if it's about sc.
func (a *App) aboutFor(sc *ServerConnection) *serverAboutState {
	if a.view != ViewServerManagement || a.serverManagementState == nil {
		return nil
	}
	ab := a.serverManagementState.About
	if ab == nil || ab.conn != sc {
		return nil
	}
	return ab
}

// handleServerAdminEvent takes SERVER_STATUS and SERVER_ADMIN_RESULT.
func (a *App) handleServerAdminEvent(sc *ServerConnection, msg *protocol.Message) {
	ab := a.aboutFor(sc)
	if ab == nil {
		return
	}
	switch msg.Type {
	case protocol.EventServerStatus:
		var st protocol.ServerStatusPayload
		if json.Unmarshal(msg.Data, &st) != nil {
			return
		}
		ab.status, ab.gotAt, ab.asking = &st, time.Now(), false
		if ab.restarting && st.StartedAt.After(time.Now().Add(-5*time.Minute)) {
			ab.restarting = false
			ab.notice, ab.noticeErr = "The server is back", false
		}
	case protocol.EventServerAdminResult:
		var res protocol.ServerAdminResult
		if json.Unmarshal(msg.Data, &res) != nil {
			return
		}
		ab.notice, ab.noticeErr = res.Message, !res.OK
		if res.Restarting {
			ab.restarting = true
		}
	}
}

// serverAboutReconnected asks again once a restarted server is back.
func (a *App) serverAboutReconnected(sc *ServerConnection) {
	if ab := a.aboutFor(sc); ab != nil && ab.canAsk {
		ab.tooOld = !answersServerAdmin(sc)
		if !ab.tooOld {
			a.askServerStatus(protocol.ServerAdminStatus)
		}
	}
}

func (a *App) handleServerRelease(m serverReleaseMsg) {
	if s := a.serverManagementState; s != nil && s.About != nil {
		s.About.release, s.About.checking = updateCheckedMsg(m), false
	}
}

// handleServerAboutKey takes the About page's keys; false lets the
// rest of Server Settings have it.
func (a *App) handleServerAboutKey(key string) (bool, tea.Cmd) {
	s := a.serverManagementState
	if s == nil || s.About == nil {
		return false, nil
	}
	ab := s.About
	canAct := ab.status != nil && ab.status.CanAct
	if ab.confirmUpdate {
		ab.confirmUpdate = false
		if key == "u" || key == "U" {
			ab.notice, ab.noticeErr = "Updating to "+ab.release.latest+"…", false
			a.sendServerAdmin(protocol.ServerAdminUpdate, ab.release.latest)
			return true, nil
		}
		ab.notice = ""
		if key == "esc" {
			return true, nil
		}
	}
	if ab.confirmRestart {
		ab.confirmRestart = false
		switch key {
		case "r", "R":
			ab.notice, ab.noticeErr = "Restarting…", false
			a.askServerStatus(protocol.ServerAdminRestart)
			return true, nil
		case "esc":
			ab.notice = ""
			return true, nil
		}
		ab.notice = ""
	}
	switch key {
	case "b", "B":
		if !canAct {
			return true, nil
		}
		if ab.status.BackingUp {
			ab.notice, ab.noticeErr = "A backup is already running", true
			return true, nil
		}
		ab.notice, ab.noticeErr = "Backing up…", false
		a.askServerStatus(protocol.ServerAdminBackup)
		return true, nil
	case "r", "R":
		if !canAct {
			return true, nil
		}
		if !ab.status.CanRestart {
			ab.notice, ab.noticeErr = ab.status.RestartNote, true
			return true, nil
		}
		ab.confirmRestart = true
		ab.notice, ab.noticeErr = "Press R again to restart the server (everyone reconnects in a few seconds) · any other key cancels", true
		return true, nil
	case "u", "U":
		if !a.canUpdateServer() {
			return true, nil
		}
		if ab.status.Updating {
			ab.notice, ab.noticeErr = "An update is already running", true
			return true, nil
		}
		ab.confirmUpdate = true
		ab.notice, ab.noticeErr = "Press U again to update to "+ab.release.latest+" and restart (everyone reconnects in a few seconds) · any other key cancels", true
		return true, nil
	case "s", "S":
		ab.notice = ""
		return true, a.openServerAbout()
	}
	return false, nil
}

// canUpdateServer: an administrator, a newer release, and a server that
// can install it itself.
func (a *App) canUpdateServer() bool {
	s := a.serverManagementState
	if s == nil || s.About == nil || s.About.status == nil || a.activeConn == nil {
		return false
	}
	ab := s.About
	a.activeConn.mu.RLock()
	version := a.activeConn.ServerVersion
	a.activeConn.mu.RUnlock()
	return ab.status.CanAct && ab.status.UpdateHow == protocol.UpdateInPlace &&
		!ab.checking && newerVersion(version, ab.release.latest)
}

// ── The page ───────────────────────────────────────────────────────────────

func (a *App) renderServerAboutContent(width, height int) string {
	layout := calculateSettingsLayout(width, height, 2, 0)
	c := a.theme.Colors
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	good := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Orange))
	w := layout.interiorWidth

	var ab *serverAboutState
	if s := a.serverManagementState; s != nil {
		ab = s.About
	}

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, w)
	top.writeLine(label.Render("About"))
	top.writeLine(dim.Render(ansi.Truncate("This server's version, health and upkeep", w, "…")))
	top.writeBlank()
	top.writeLine(a.renderSeparator(w))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, w)
	const labelW = 12
	row := func(name string, value string) {
		pad := strings.Repeat(" ", max(0, labelW-len(name)))
		middle.writeLine(label.Render(name) + pad + ansi.Truncate(value, max(1, w-labelW), "…"))
	}
	more := func(value string) {
		middle.writeLine(strings.Repeat(" ", labelW) + ansi.Truncate(value, max(1, w-labelW), "…"))
	}

	if a.activeConn == nil {
		middle.writeLine(dim.Render("  No active server connection"))
	} else {
		sc := a.activeConn
		sc.mu.RLock()
		version, commit, built := sc.ServerVersion, sc.ServerGitCommit, sc.ServerBuildTime
		sc.mu.RUnlock()

		// Version, and the latest release.
		v := normal.Render(orDash(version))
		if commit != "" || built != "" {
			v += dim.Render(fmt.Sprintf("  %s %s", commit, strings.ReplaceAll(built, "_", " ")))
		}
		row("Version", v)
		if ab != nil {
			switch r := ab.release; {
			case ab.checking:
				more(dim.Render("Checking for a newer release…"))
			case r.err != "":
				more(warn.Render(r.err))
			case r.latest == "":
				more(dim.Render("No releases published yet"))
			case newerVersion(version, r.latest):
				more(warn.Render(r.latest+" is out") + dim.Render(" · "+r.date.Local().Format("Jan 2, 2006")+" · "+r.url))
				for _, line := range releaseNoteLines(r.notes, 3) {
					more(dim.Render("  " + line))
				}
				if st := ab.status; st != nil {
					switch {
					case st.Updating:
						more(normal.Render("Updating…"))
					case a.canUpdateServer():
						more(good.Render("U to update and restart"))
					case st.UpdateHow == protocol.UpdateDocker:
						more(dim.Render("To update: " + st.UpdateNote))
					case st.UpdateNote != "" && st.CanAct:
						more(dim.Render("Can't update from here: " + st.UpdateNote))
					}
				}
			case func() bool { _, _, ok := parseVersion(version); return !ok }():
				more(dim.Render("Built from source · the latest release is " + r.latest))
			default:
				more(good.Render("Up to date") + dim.Render(" · the latest release is "+r.latest))
			}
		}
		middle.writeBlank()

		switch {
		case ab == nil:
		case !ab.canAsk:
			middle.writeLine(dim.Render("  Health and upkeep are for people who can manage the server"))
		case ab.tooOld:
			middle.writeLine(dim.Render(fmt.Sprintf("  This server is too old to report its health: update it to v%s or later", serverAdminVersion)))
		case ab.status == nil && ab.restarting:
			middle.writeLine(dim.Render("  Restarting…"))
		case ab.status == nil:
			middle.writeLine(dim.Render("  Asking the server…"))
		default:
			st := ab.status
			up := st.Now.Sub(st.StartedAt) + time.Since(ab.gotAt)
			row("Uptime", normal.Render(humanDuration(up))+dim.Render(" · since "+st.StartedAt.Local().Format("Jan 2 15:04")))
			row("Runs as", normal.Render(runsAsText(st.RunsAs))+dim.Render(" · "+st.OS+"/"+st.Arch))
			row("People", normal.Render(count(st.Members, "member", "members")+" · "+commas(st.Online)+" online now"))
			row("Content", normal.Render(count(st.Channels, "channel", "channels")+" · "+count(st.Messages, "message", "messages")))
			row("Database", normal.Render(aboutBytes(st.DatabaseBytes)))
			row("Plugins", normal.Render(fmt.Sprintf("%d installed · %d running", st.PluginsInstalled, st.PluginsRunning)))
			g := st.Grapevine
			switch {
			case !g.Enabled:
				row("Grapevine", dim.Render("Not listed"))
			case g.LastError != "":
				row("Grapevine", warn.Render(g.LastError))
			case g.LastHeartbeat.IsZero():
				row("Grapevine", dim.Render("Listing on "+g.HubURL+"…"))
			default:
				row("Grapevine", normal.Render("Listed on "+g.HubURL)+dim.Render(" · last heartbeat "+humanDuration(st.Now.Sub(g.LastHeartbeat))+" ago"))
			}
			switch {
			case st.BackingUp:
				row("Backups", normal.Render("Backing up…"))
			case st.BackupCount == 0:
				row("Backups", dim.Render("None from here yet"))
			default:
				row("Backups", normal.Render(fmt.Sprintf("%d · newest %s (%s)", st.BackupCount, st.LastBackupAt.Local().Format("Jan 2 15:04"), aboutBytes(st.LastBackupBytes))))
			}
			if st.BackupsDir != "" {
				more(dim.Render("in " + st.BackupsDir))
			}
		}
		if ab != nil && ab.notice != "" {
			middle.writeBlank()
			style := good
			if ab.noticeErr {
				style = warn
			}
			middle.writeLine(style.Render(ansi.Truncate(ab.notice, w, "…")))
		}
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, w)
	bottom.writeLine(a.renderSeparator(w))
	hint := "S check again · Esc close"
	if ab != nil && ab.status != nil && ab.status.CanAct {
		hint = "B back up now · R restart · " + hint
		if a.canUpdateServer() {
			hint = "U update · " + hint
		}
	}
	bottom.writeLine(dim.Render(hint))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Selection)).
		Padding(0, 1).
		Render(content)
}

// count is "1 member" or "1,204 members".
func count(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return commas(n) + " " + many
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func runsAsText(kind string) string {
	switch kind {
	case protocol.RunsAsDocker:
		return "Docker container"
	case protocol.RunsAsSystemd:
		return "systemd service"
	case protocol.RunsAsLaunchd:
		return "launchd job"
	case protocol.RunsAsWindows:
		return "Windows startup task"
	}
	return "Started by hand"
}

// releaseNoteLines is the first few lines of a release's notes with
// markdown headings and blank lines left out.
func releaseNoteLines(notes string, n int) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(notes, "\r", ""), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
		if len(out) == n {
			break
		}
	}
	return out
}

func humanDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%d s", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d min", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%d h %d min", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d days %d h", int(d.Hours())/24, int(d.Hours())%24)
}

func aboutBytes(n int64) string {
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
