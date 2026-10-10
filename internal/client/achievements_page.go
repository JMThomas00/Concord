package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/protocol"
	zone "github.com/lrstanley/bubblezone"
)

// Settings > About > Achievements: a tab for Concord's own achievements,
// then one per plugin per server (plugin_records.go), each with your
// stats, its leaderboard and its achievements. ←/→ (or Tab) change tab,
// ↑/↓ and the wheel scroll, V shows or hides you on that server's
// leaderboards, Esc goes back to About.

// openAchievements opens the page on its first tab.
func (a *App) openAchievements() tea.Cmd {
	s := a.settingsState
	s.AboutAch, s.AboutTab, s.AboutAchScroll = true, 0, 0
	return nil
}

// selectAchTab moves to a tab and fetches its leaderboard.
func (a *App) selectAchTab(i int) tea.Cmd {
	s := a.settingsState
	tabs := a.achievementTabs()
	if len(tabs) == 0 {
		return nil
	}
	s.AboutTab = (i%len(tabs) + len(tabs)) % len(tabs)
	s.AboutAchScroll = 0
	if t := tabs[s.AboutTab]; t.sc != nil && t.board.LeaderboardStat != "" {
		return a.fetchLeaderboard(t.sc, t.board.PluginID, false)
	}
	return nil
}

// handleAchKey handles every key while the page is open.
func (a *App) handleAchKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	switch msg.String() {
	case "esc", "a", "A":
		s.AboutAch = false
	case "right", "l", "tab":
		return a.selectAchTab(s.AboutTab + 1)
	case "left", "h", "shift+tab":
		return a.selectAchTab(s.AboutTab - 1)
	case "up", "k":
		s.AboutAchScroll = max(0, s.AboutAchScroll-1)
	case "down", "j":
		s.AboutAchScroll++
	case "pgup":
		s.AboutAchScroll = max(0, s.AboutAchScroll-10)
	case "pgdown":
		s.AboutAchScroll += 10
	case "r", "R": // refresh the leaderboard
		tabs := a.achievementTabs()
		if t := tabs[min(s.AboutTab, len(tabs)-1)]; t.sc != nil && t.board.LeaderboardStat != "" {
			return a.fetchLeaderboard(t.sc, t.board.PluginID, true)
		}
	case "v", "V": // show or hide me on this server's leaderboards
		tabs := a.achievementTabs()
		t := tabs[min(s.AboutTab, len(tabs)-1)]
		if t.sc == nil {
			return nil
		}
		st := a.leaderboards[leaderboardKey(t.sc.ServerID, t.board.PluginID)]
		hidden := st != nil && st.resp != nil && st.resp.Hidden
		return a.setLeaderboardVisible(t.sc, t.board.PluginID, hidden)
	case "ctrl+q":
		return tea.Quit
	}
	return nil
}

// clickAchTab handles a click on one of the page's tabs.
func (a *App) clickAchTab(msg tea.MouseMsg) (tea.Cmd, bool) {
	s := a.settingsState
	if s == nil || !s.AboutAch || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	for i := range a.achievementTabs() {
		if z := zone.Get(fmt.Sprintf("ach-tab:%d", i)); z != nil && z.InBounds(msg) {
			return a.selectAchTab(i), true
		}
	}
	return nil, false
}

// renderAchievementsPage draws the page inside the About panel.
func (a *App) renderAchievementsPage(width, height int) string {
	layout := calculateSettingsLayout(width, height, 2, 0)
	c := a.theme.Colors
	s := a.settingsState
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	tabs := a.achievementTabs()
	s.AboutTab = min(s.AboutTab, len(tabs)-1)
	tab := tabs[s.AboutTab]

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(label.Render("Achievements"))
	top.writeLine(a.renderAchTabBar(tabs, s.AboutTab, layout.interiorWidth))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	var lines []string
	if tab.sc == nil {
		lines = a.concordAchievementLines(layout.interiorWidth)
	} else {
		lines = a.pluginAchievementLines(tab, layout.interiorWidth)
	}
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	maxScroll := max(0, len(lines)-layout.middleLines)
	s.AboutAchScroll = min(s.AboutAchScroll, maxScroll)
	for i, l := range lines[s.AboutAchScroll:] {
		if i >= layout.middleLines {
			break
		}
		middle.writeLine(l)
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	help := "←/→ tabs · Esc back to About"
	if tab.sc != nil && tab.board.LeaderboardStat != "" {
		help = "←/→ tabs · V show or hide me on leaderboards · R refresh · Esc back"
	}
	if maxScroll > 0 {
		help = "↑↓ scroll · " + help
	}
	bottom.writeLine(dim.Render(help))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().Width(width).Height(height-2).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Selection)).
		Padding(0, 1).Render(content)
}

// renderAchTabBar is the row of tabs, scrolled to keep the chosen one in
// view, each clickable.
func (a *App) renderAchTabBar(tabs []achTab, sel, width int) string {
	c := a.theme.Colors
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Background(lipgloss.Color(c.Purple)).Bold(true).Padding(0, 1)
	off := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground)).Background(lipgloss.Color(c.Selection)).Padding(0, 1)
	rendered := make([]string, len(tabs))
	for i, t := range tabs {
		st := off
		if i == sel {
			st = on
		}
		rendered[i] = zone.Mark(fmt.Sprintf("ach-tab:%d", i), st.Render(t.title))
	}
	// Drop tabs from the front until the chosen one fits.
	first := 0
	for first < sel && lipgloss.Width(strings.Join(rendered[first:sel+1], " ")) > width-4 {
		first++
	}
	bar := strings.Join(rendered[first:], " ")
	if first > 0 {
		bar = "‹ " + bar
	}
	return ansi.Truncate(bar, width, "›")
}

// achLine is one achievement row: medal or icon, name, description, date
// (or ☆ ??? and the description while locked).
func (a *App) achLine(icon, tier, name, desc, date string, width int) string {
	c := a.theme.Colors
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	if date == "" {
		return dim.Render(ansi.Truncate("  ☆ ???  "+desc, width, "…"))
	}
	mark := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Yellow)).Render("★ ")
	if m := tierMedal(tier); m != "" {
		mark = m + " "
	} else if icon != "" {
		mark = icon + " "
	}
	room := width - lipgloss.Width(mark) - len([]rune(name)) - len(date) - 8
	if len([]rune(desc)) > room {
		desc = ansi.Truncate(desc, max(0, room), "…")
	}
	return "  " + mark + normal.Render(name) + "  " + dim.Render(desc) + "  " + dim.Italic(true).Render(date)
}

// concordAchievementLines is the Concord tab: the collection achievements.
func (a *App) concordAchievementLines(width int) []string {
	coll := a.coll()
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	earned := 0
	for _, ach := range achievements {
		if coll.Achievements[ach.id] != "" {
			earned++
		}
	}
	lines := []string{purple.Render(fmt.Sprintf("Concord  %d/%d", earned, len(achievements))), ""}
	hidden := 0
	for _, ach := range achievements {
		date := coll.Achievements[ach.id]
		if date == "" && ach.secret {
			hidden++
			continue
		}
		lines = append(lines, a.achLine("", map[string]string{"🥉": "bronze", "🥈": "silver", "🥇": "gold"}[achievementTiers[ach.id]],
			ach.name, ach.description(), date, width))
	}
	if hidden > 0 {
		lines = append(lines, dim.Render(fmt.Sprintf("  … and %d secret ones", hidden)))
	}
	return lines
}

// pluginAchievementLines is a plugin's tab: your stats, its leaderboard,
// and its achievements.
func (a *App) pluginAchievementLines(t achTab, width int) []string {
	c := a.theme.Colors
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	t.sc.mu.RLock()
	rec := t.sc.PluginRecords[t.board.PluginID]
	t.sc.mu.RUnlock()

	server := ""
	if t.sc.ServerInfo != nil {
		server = "  on " + t.sc.ServerInfo.Name
	}
	lines := []string{purple.Render(t.board.Name) + dim.Render(server), ""}

	// Your stats.
	if len(rec.Stats) == 0 {
		lines = append(lines, dim.Render("  Nothing yet: play to start your record."))
	} else {
		var parts []string
		for _, s := range rec.Stats {
			parts = append(parts, dim.Render(s.Label+" ")+normal.Bold(true).Render(s.Value))
		}
		lines = append(lines, "  "+strings.Join(parts, dim.Render("  ·  ")))
	}

	// The leaderboard.
	if t.board.LeaderboardStat != "" {
		title := "Leaderboard"
		if t.board.LeaderboardLabel != "" {
			title += " · " + t.board.LeaderboardLabel
		}
		lines = append(lines, "", purple.Render(title), "")
		lines = append(lines, a.leaderboardLines(t, rec)...)
	}

	// Achievements.
	unlocked := map[string]string{}
	for _, u := range rec.Unlocked {
		unlocked[u.ID] = u.At.Local().Format("2006-01-02")
	}
	if len(t.board.Achievements) > 0 {
		lines = append(lines, "", purple.Render(fmt.Sprintf("Achievements  %d/%d", len(unlocked), len(t.board.Achievements))), "")
		hidden := 0
		for _, d := range t.board.Achievements {
			date := unlocked[d.ID]
			if date == "" && d.Secret {
				hidden++
				continue
			}
			lines = append(lines, a.achLine(d.Icon, d.Tier, d.Name, d.Description, date, width))
		}
		if hidden > 0 {
			lines = append(lines, dim.Render(fmt.Sprintf("  … and %d secret ones", hidden)))
		}
	}
	return lines
}

func (a *App) leaderboardLines(t achTab, rec protocol.PluginRecord) []string {
	c := a.theme.Colors
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	you := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)).Bold(true)
	st := a.leaderboards[leaderboardKey(t.sc.ServerID, t.board.PluginID)]
	switch {
	case st == nil || (st.resp == nil && st.loading):
		return []string{dim.Render("  Loading…")}
	case st.err != "":
		return []string{dim.Render("  Couldn't load it: " + st.err)}
	case st.resp == nil || len(st.resp.Entries) == 0:
		return []string{dim.Render("  Nobody's on it yet.")}
	}
	var lines []string
	medals := []string{"🥇", "🥈", "🥉"}
	me := t.sc.User
	for _, e := range st.resp.Entries {
		place := fmt.Sprintf("%3d.", e.Rank)
		if e.Rank <= 3 {
			place = " " + medals[e.Rank-1] + " "
		}
		name := fmt.Sprintf("%-20s", e.Username)
		style := normal
		if me != nil && e.UserID == me.ID {
			style = you
		}
		lines = append(lines, "  "+place+" "+style.Render(name)+" "+style.Render(e.Value))
	}
	switch {
	case st.resp.Hidden:
		lines = append(lines, "", dim.Render("  You're hidden from this server's leaderboards (V to show you)."))
	case st.resp.You != nil && st.resp.You.Rank > len(st.resp.Entries):
		lines = append(lines, "", you.Render(fmt.Sprintf("  You're %s, with %s.", ordinal(st.resp.You.Rank), st.resp.You.Value)))
	}
	return lines
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
