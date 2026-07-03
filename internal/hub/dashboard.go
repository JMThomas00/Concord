package hub

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DashboardModel is a live TUI for a running hub: pinned identity + stats and
// the registered-server table on top, with the activity log tailing below.
type DashboardModel struct {
	hub    *Hub
	width  int
	height int

	servers  []*RegisteredServer
	peers    []*PeerHub
	fedCount int
	logLines []string
	dbErr    string

	// confirmQuit gates tea.Quit — quitting the dashboard shuts the hub down,
	// so a stray 'q' must not take the hub offline without confirmation.
	confirmQuit bool
}

// NewDashboard creates a dashboard bound to a (running) hub.
func NewDashboard(h *Hub) DashboardModel {
	return DashboardModel{hub: h, width: 120, height: 30}
}

type dashTickMsg time.Time

func dashTick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return dashTickMsg(t) })
}

func (m DashboardModel) Init() tea.Cmd {
	return tea.Batch(m.refresh, dashTick())
}

type dashDataMsg struct {
	servers  []*RegisteredServer
	peers    []*PeerHub
	fedCount int
	err      string
}

// refresh pulls current state from the hub DB.
func (m DashboardModel) refresh() tea.Msg {
	data := dashDataMsg{}
	servers, err := m.hub.db.ListServers("", "", true)
	if err != nil {
		data.err = err.Error()
	}
	data.servers = servers
	if peers, err := m.hub.db.ListPeerHubs(); err == nil {
		data.peers = peers
	}
	if fed, err := m.hub.db.ListFederatedServers("", ""); err == nil {
		data.fedCount = len(fed)
	}
	return data
}

func (m DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		if m.confirmQuit {
			switch msg.String() {
			// ctrl+c is here so a second reflexive ctrl+c confirms instead
			// of cancelling the dialog it just opened.
			case "y", "Y", "q", "enter", "ctrl+c":
				return m, tea.Quit
			// On Windows a Ctrl keydown arrives as a stray NUL key event
			// before the real ctrl+c — ignore it or it cancels the dialog.
			case "\x00":
				return m, nil
			default: // n, esc, or anything else keeps the hub running
				m.confirmQuit = false
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			m.confirmQuit = true
		}

	case dashTickMsg:
		return m, tea.Batch(m.refresh, dashTick())

	case dashDataMsg:
		m.servers = msg.servers
		m.peers = msg.peers
		m.fedCount = msg.fedCount
		m.dbErr = msg.err
	}
	return m, nil
}

// ── Rendering ─────────────────────────────────────────────────────────────────

var (
	dashAccent  = lipgloss.Color("#BD93F9")
	dashDim     = lipgloss.Color("#6272a4")
	dashText    = lipgloss.Color("#f8f8f2")
	dashGreen   = lipgloss.Color("#50fa7b")
	dashRed     = lipgloss.Color("#ff5555")
	dashYellow  = lipgloss.Color("#f1fa8c")
	dashBox     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(dashAccent).Padding(0, 1)
	dashTitleSt = lipgloss.NewStyle().Foreground(dashAccent).Bold(true)
	dashDimSt   = lipgloss.NewStyle().Foreground(dashDim)
	dashTextSt  = lipgloss.NewStyle().Foreground(dashText)
)

func (m DashboardModel) View() string {
	if m.width < 40 || m.height < 15 {
		return "Terminal too small for the hub dashboard."
	}

	if m.confirmQuit {
		return m.renderQuitConfirm()
	}

	header := m.renderHeader()
	serversBox := m.renderServers()

	used := lipgloss.Height(header) + lipgloss.Height(serversBox)
	logHeight := m.height - used - 3 // border + padding for log box
	if logHeight < 3 {
		logHeight = 3
	}
	logBox := m.renderLog(logHeight)

	return lipgloss.JoinVertical(lipgloss.Left, header, serversBox, logBox)
}

// renderQuitConfirm draws the shutdown-confirmation dialog centered on screen.
func (m DashboardModel) renderQuitConfirm() string {
	warnSt := lipgloss.NewStyle().Foreground(dashYellow).Bold(true)
	dialog := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(dashRed).
		Padding(1, 3).
		Render(warnSt.Render("⚠  Shut down the hub?") + "\n\n" +
			dashTextSt.Render("Quitting the dashboard stops the Grapevine hub.") + "\n" +
			dashTextSt.Render("Registered servers and clients will lose discovery") + "\n" +
			dashTextSt.Render("until it is started again.") + "\n\n" +
			dashDimSt.Render("[y] shut down    [n / esc] keep running"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialog)
}

func (m DashboardModel) renderHeader() string {
	s := m.hub.stats

	online, totalMembers, totalOnline := 0, 0, 0
	for _, sv := range m.servers {
		if sv.IsOnline {
			online++
			totalMembers += sv.MemberCount
			totalOnline += sv.OnlineCount
		}
	}

	activePeers := 0
	var lastSync string
	for _, p := range m.peers {
		if p.IsActive {
			activePeers++
			if p.LastSynced != nil {
				age := time.Since(*p.LastSynced).Round(time.Second)
				if lastSync == "" {
					lastSync = age.String() + " ago"
				}
			}
		}
	}
	if lastSync == "" {
		lastSync = "never"
	}

	title := dashTitleSt.Render("🍇 GRAPEVINE HUB") + dashDimSt.Render("  ·  ") +
		dashTextSt.Render(m.hub.config.HubName)
	addr := dashDimSt.Render(fmt.Sprintf("listening on %s:%d  ·  uptime %s",
		m.hub.config.Host, m.hub.config.Port, formatUptime(s.Uptime())))

	stat := func(label string, value string) string {
		return dashDimSt.Render(label+" ") + dashTextSt.Bold(true).Render(value)
	}
	sep := dashDimSt.Render("  │  ")

	row1 := strings.Join([]string{
		stat("Servers", fmt.Sprintf("%d online / %d listed", online, len(m.servers))),
		stat("Users", fmt.Sprintf("%d online / %d members", totalOnline, totalMembers)),
		stat("Peer Hubs", fmt.Sprintf("%d (synced %s)", activePeers, lastSync)),
		stat("Federated", fmt.Sprintf("%d cached", m.fedCount)),
	}, sep)

	row2 := strings.Join([]string{
		stat("Joins", fmt.Sprintf("%d served", s.JoinsServed.Load())),
		stat("Rate-limited", fmt.Sprintf("%d", s.JoinsRateLimited.Load())),
		stat("Heartbeats", fmt.Sprintf("%d", s.Heartbeats.Load())),
		stat("Registrations", fmt.Sprintf("%d (+%d dereg)", s.Registrations.Load(), s.Deregistrations.Load())),
		stat("Fed Syncs", fmt.Sprintf("%d", s.FederationSyncs.Load())),
	}, sep)

	content := title + "\n" + addr + "\n\n" + row1 + "\n" + row2
	if m.dbErr != "" {
		content += "\n" + lipgloss.NewStyle().Foreground(dashRed).Render("DB error: "+m.dbErr)
	}

	return dashBox.Width(m.width - 2).Render(content)
}

func (m DashboardModel) renderServers() string {
	headerSt := lipgloss.NewStyle().Foreground(dashAccent).Bold(true)

	nameW := m.width - 66
	if nameW < 16 {
		nameW = 16
	}

	var b strings.Builder
	b.WriteString(headerSt.Render(fmt.Sprintf("%-*s  %-14s  %-8s  %7s  %8s  %-12s",
		nameW, "SERVER", "CATEGORY", "STATUS", "ONLINE", "MEMBERS", "LAST BEAT")))

	if len(m.servers) == 0 {
		b.WriteString("\n" + dashDimSt.Render("No servers registered yet — waiting for the first POST /v1/servers…"))
	}

	// Cap the table so the log pane always gets room.
	maxRows := m.height / 3
	if maxRows < 4 {
		maxRows = 4
	}
	shown := m.servers
	if len(shown) > maxRows {
		shown = shown[:maxRows]
	}

	for _, sv := range shown {
		status := lipgloss.NewStyle().Foreground(dashRed).Render("○ offline")
		beat := "—"
		if sv.IsOnline {
			status = lipgloss.NewStyle().Foreground(dashGreen).Render("● online ")
		}
		if !sv.LastHeartbeat.IsZero() {
			beat = time.Since(sv.LastHeartbeat).Round(time.Second).String() + " ago"
		}
		category := sv.Category
		if category == "" {
			category = "—"
		}
		b.WriteString("\n" + dashTextSt.Render(fmt.Sprintf("%-*s  %-14s  ",
			nameW, truncate(sv.Name, nameW), truncate(category, 14))) +
			status + dashTextSt.Render(fmt.Sprintf("  %7d  %8d  %-12s",
			sv.OnlineCount, sv.MemberCount, beat)))
	}
	if len(m.servers) > len(shown) {
		b.WriteString("\n" + dashDimSt.Render(fmt.Sprintf("… and %d more", len(m.servers)-len(shown))))
	}

	return dashBox.Width(m.width - 2).Render(b.String())
}

func (m DashboardModel) renderLog(height int) string {
	lines := m.hub.stats.LogTail(height)
	logSt := lipgloss.NewStyle().Foreground(dashDim)

	var b strings.Builder
	b.WriteString(dashTitleSt.Render("ACTIVITY") +
		dashDimSt.Render("   [q] quit"))
	for i := 0; i < height; i++ {
		b.WriteString("\n")
		if i < len(lines) {
			b.WriteString(logSt.Render(truncate(lines[i], m.width-6)))
		}
	}
	return dashBox.Width(m.width - 2).Render(b.String())
}

func truncate(s string, max int) string {
	if max <= 1 || len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

func formatUptime(d time.Duration) string {
	d = d.Round(time.Second)
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	default:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
}
