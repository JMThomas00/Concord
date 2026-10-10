package client

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/protocol"
	zone "github.com/lrstanley/bubblezone"
)

// Settings > About > Achievements. Three rows choose what's listed:
//
//	SEARCH  fuzzy, across whatever the other two rows have narrowed it to
//	SERVER  All · Concord (this computer's own) · each server with plugin boards
//	PLUGIN  All · each plugin on the chosen server (hidden for Concord)
//
// One plugin on one server shows its stats, leaderboard and achievements;
// anything wider lists a section per plugin. Tab/Shift+Tab move between
// the rows, ←/→ change the server or plugin, / jumps to search, ↑/↓,
// PgUp/PgDn, Home/End and the wheel scroll, V shows or hides you on that
// server's leaderboards, Esc clears the search, then goes back to About.

// Which row of the page has the keys.
const (
	achFocusSearch = iota
	achFocusServer
	achFocusPlugin
)

// achServer is one entry on the server row.
type achServer struct {
	key  string // "all", "concord", or the server's ID
	name string
	sc   *ServerConnection
}

// achPlugin is one entry on the plugin row.
type achPlugin struct {
	key  string // "all", or the plugin's ID
	name string
}

// openAchievements opens the page on Concord's own achievements.
func (a *App) openAchievements() tea.Cmd {
	s := a.settingsState
	s.AboutAch, s.AboutAchScroll = true, 0
	s.AchServer, s.AchPlugin, s.AchQuery, s.AchFocus = "concord", "all", "", achFocusServer
	return nil
}

// achServers is the server row: All, Concord, then every server that has
// plugin boards, by name.
func (a *App) achServers() []achServer {
	var servers []achServer
	for _, sc := range a.allConnections() {
		sc.mu.RLock()
		has := len(sc.PluginBoards) > 0
		sc.mu.RUnlock()
		if !has {
			continue
		}
		name := "Server"
		if sc.ServerInfo != nil && sc.ServerInfo.Name != "" {
			name = sc.ServerInfo.Name
		}
		servers = append(servers, achServer{key: sc.ServerID.String(), name: name, sc: sc})
	}
	sort.SliceStable(servers, func(i, j int) bool { return strings.ToLower(servers[i].name) < strings.ToLower(servers[j].name) })
	return append([]achServer{{key: "all", name: "All"}, {key: "concord", name: "Concord"}}, servers...)
}

// achPlugins is the plugin row for a server: All, then its plugins by name
// (for All, every plugin on any server, once). Concord has none.
func (a *App) achPlugins(srv achServer) []achPlugin {
	if srv.key == "concord" {
		return nil
	}
	seen := map[string]string{}
	for _, s := range a.achServers() {
		if s.sc == nil || (srv.key != "all" && s.key != srv.key) {
			continue
		}
		s.sc.mu.RLock()
		for _, b := range s.sc.PluginBoards {
			seen[b.PluginID] = b.Name
		}
		s.sc.mu.RUnlock()
	}
	var plugins []achPlugin
	for id, name := range seen {
		plugins = append(plugins, achPlugin{key: id, name: name})
	}
	sort.Slice(plugins, func(i, j int) bool { return strings.ToLower(plugins[i].name) < strings.ToLower(plugins[j].name) })
	return append([]achPlugin{{key: "all", name: "All"}}, plugins...)
}

// achSelection is the chosen server and plugin, as indexes into their rows
// (an entry that has gone, say a server that disconnected, falls back).
func (a *App) achSelection() ([]achServer, int, []achPlugin, int) {
	s := a.settingsState
	servers := a.achServers()
	si := 1 // Concord
	for i, srv := range servers {
		if srv.key == s.AchServer {
			si = i
		}
	}
	plugins := a.achPlugins(servers[si])
	pi := 0
	for i, p := range plugins {
		if p.key == s.AchPlugin {
			pi = i
		}
	}
	return servers, si, plugins, pi
}

// achSections are the sections the page lists: Concord's own (sc nil),
// then each chosen plugin on each chosen server.
func (a *App) achSections() []achTab {
	servers, si, plugins, pi := a.achSelection()
	srv := servers[si]
	if srv.key == "concord" {
		return []achTab{{title: "Concord"}}
	}
	plugin := "all"
	if pi < len(plugins) {
		plugin = plugins[pi].key
	}
	var out []achTab
	if srv.key == "all" && plugin == "all" {
		out = append(out, achTab{title: "Concord"})
	}
	var secs []achTab
	for _, s := range servers {
		if s.sc == nil || (srv.key != "all" && s.key != srv.key) {
			continue
		}
		s.sc.mu.RLock()
		for _, b := range s.sc.PluginBoards {
			if plugin == "all" || b.PluginID == plugin {
				secs = append(secs, achTab{sc: s.sc, board: b, title: b.Name + " · " + s.name})
			}
		}
		s.sc.mu.RUnlock()
	}
	sort.SliceStable(secs, func(i, j int) bool { return strings.ToLower(secs[i].title) < strings.ToLower(secs[j].title) })
	return append(out, secs...)
}

// achSingle is the one plugin on one server being shown, if that's what's
// chosen: it gets the leaderboard and the V and R keys.
func (a *App) achSingle() (achTab, bool) {
	secs := a.achSections()
	if len(secs) == 1 && secs[0].sc != nil {
		return secs[0], true
	}
	return achTab{}, false
}

// achChanged runs after the server or plugin changes: back to the top,
// and fetch the leaderboard when one plugin on one server is chosen.
func (a *App) achChanged() tea.Cmd {
	a.settingsState.AboutAchScroll = 0
	if t, ok := a.achSingle(); ok && t.board.LeaderboardStat != "" {
		return a.fetchLeaderboard(t.sc, t.board.PluginID, false)
	}
	return nil
}

// moveAchServer and moveAchPlugin step the server or plugin row. The
// chosen plugin is remembered across servers: one that doesn't have it
// shows All, and the next one that does picks it up again.
func (a *App) moveAchServer(d int) tea.Cmd {
	s := a.settingsState
	servers, si, _, _ := a.achSelection()
	s.AchServer = servers[(si+d+len(servers))%len(servers)].key
	return a.achChanged()
}

func (a *App) moveAchPlugin(d int) tea.Cmd {
	s := a.settingsState
	_, _, plugins, pi := a.achSelection()
	if len(plugins) == 0 {
		return nil
	}
	s.AchPlugin = plugins[(pi+d+len(plugins))%len(plugins)].key
	return a.achChanged()
}

// handleAchKey handles every key while the page is open.
func (a *App) handleAchKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	key := msg.String()
	// keys that scroll the list, wherever the focus is
	switch key {
	case "up":
		s.AboutAchScroll = max(0, s.AboutAchScroll-1)
		return nil
	case "down":
		s.AboutAchScroll++
		return nil
	case "pgup":
		s.AboutAchScroll = max(0, s.AboutAchScroll-10)
		return nil
	case "pgdown":
		s.AboutAchScroll += 10
		return nil
	case "home":
		s.AboutAchScroll = 0
		return nil
	case "end":
		s.AboutAchScroll = 1 << 30 // clamped when drawn
		return nil
	case "tab":
		s.AchFocus = (s.AchFocus + 1) % a.achRows()
		return nil
	case "shift+tab":
		s.AchFocus = (s.AchFocus + a.achRows() - 1) % a.achRows()
		return nil
	case "ctrl+q":
		return tea.Quit
	}
	if s.AchFocus == achFocusSearch {
		switch {
		case key == "esc" && s.AchQuery != "":
			s.AchQuery, s.AboutAchScroll = "", 0
		case key == "esc":
			s.AboutAch = false
		case key == "backspace":
			if r := []rune(s.AchQuery); len(r) > 0 {
				s.AchQuery, s.AboutAchScroll = string(r[:len(r)-1]), 0
			}
		case key == "ctrl+u":
			s.AchQuery, s.AboutAchScroll = "", 0
		case key == "enter":
			s.AchFocus = achFocusServer
		case msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace:
			s.AchQuery += string(msg.Runes)
			if msg.Type == tea.KeySpace {
				s.AchQuery += " "
			}
			s.AboutAchScroll = 0
		}
		return nil
	}
	switch key {
	case "esc":
		if s.AchQuery != "" {
			s.AchQuery, s.AboutAchScroll = "", 0
			return nil
		}
		s.AboutAch = false
	case "a", "A":
		s.AboutAch = false
	case "/":
		s.AchFocus = achFocusSearch
	case "j":
		s.AboutAchScroll++
	case "k":
		s.AboutAchScroll = max(0, s.AboutAchScroll-1)
	case "right", "l", "left", "h":
		d := 1
		if key == "left" || key == "h" {
			d = -1
		}
		if s.AchFocus == achFocusPlugin {
			return a.moveAchPlugin(d)
		}
		return a.moveAchServer(d)
	case "r", "R": // refresh the leaderboard
		if t, ok := a.achSingle(); ok && t.board.LeaderboardStat != "" {
			return a.fetchLeaderboard(t.sc, t.board.PluginID, true)
		}
	case "v", "V": // show or hide me on this server's leaderboards
		if t, ok := a.achSingle(); ok {
			st := a.leaderboards[leaderboardKey(t.sc.ServerID, t.board.PluginID)]
			hidden := st != nil && st.resp != nil && st.resp.Hidden
			return a.setLeaderboardVisible(t.sc, t.board.PluginID, hidden)
		}
	}
	return nil
}

// achRows is how many rows take the focus: no plugin row for Concord.
func (a *App) achRows() int {
	if a.settingsState.AchServer == "concord" {
		if a.settingsState.AchFocus == achFocusPlugin {
			a.settingsState.AchFocus = achFocusServer
		}
		return 2
	}
	return 3
}

// clickAchTab handles a click on the search box or a server or plugin.
func (a *App) clickAchTab(msg tea.MouseMsg) (tea.Cmd, bool) {
	s := a.settingsState
	if s == nil || !s.AboutAch || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil, false
	}
	if z := zone.Get("ach-search"); z != nil && z.InBounds(msg) {
		s.AchFocus = achFocusSearch
		return nil, true
	}
	servers, _, plugins, _ := a.achSelection()
	for _, srv := range servers {
		if z := zone.Get("ach-srv:" + srv.key); z != nil && z.InBounds(msg) {
			s.AchFocus, s.AchServer = achFocusServer, srv.key
			return a.moveAchServer(0), true
		}
	}
	for _, p := range plugins {
		if z := zone.Get("ach-plg:" + p.key); z != nil && z.InBounds(msg) {
			s.AchFocus, s.AchPlugin = achFocusPlugin, p.key
			return a.achChanged(), true
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
	servers, si, plugins, pi := a.achSelection()
	rows := a.achRows()

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(label.Render("Achievements"))
	top.writeLine(a.achSearchRow(layout.interiorWidth))
	var names, keys []string
	for _, srv := range servers {
		names, keys = append(names, srv.name), append(keys, "ach-srv:"+srv.key)
	}
	top.writeLine(a.achChoiceRow("SERVER", s.AchFocus == achFocusServer, names, keys, si, layout.interiorWidth))
	if rows == 3 {
		names, keys = nil, nil
		for _, p := range plugins {
			names, keys = append(names, p.name), append(keys, "ach-plg:"+p.key)
		}
		top.writeLine(a.achChoiceRow("PLUGIN", s.AchFocus == achFocusPlugin, names, keys, pi, layout.interiorWidth))
	} else {
		top.writeBlank()
	}
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	lines := a.achListLines(layout.interiorWidth)
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
	var help []string
	if s.AchFocus == achFocusSearch {
		help = append(help, "type to search", "Enter done", "Esc clear")
	} else {
		help = append(help, "←/→ choose", "/ search")
	}
	help = append(help, "Tab next row")
	if t, ok := a.achSingle(); ok && t.board.LeaderboardStat != "" && s.AchFocus != achFocusSearch {
		help = append(help, "V show or hide me", "R refresh")
	}
	help = append(help, "Esc back")
	line := dim.Render(strings.Join(help, " · "))
	if maxScroll > 0 {
		var where []string
		if s.AboutAchScroll > 0 {
			where = append(where, fmt.Sprintf("▲ %d", s.AboutAchScroll))
		}
		if below := len(lines) - layout.middleLines - s.AboutAchScroll; below > 0 {
			where = append(where, fmt.Sprintf("▼ %d", below))
		}
		scroll := label.Render("↑↓ scroll  " + strings.Join(where, " · "))
		if gap := layout.interiorWidth - lipgloss.Width(line) - lipgloss.Width(scroll); gap >= 2 {
			line += strings.Repeat(" ", gap) + scroll
		} else {
			line = scroll + "  " + line
		}
	}
	bottom.writeLine(ansi.Truncate(line, layout.interiorWidth, "…"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().Width(width).Height(height-2).
		Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color(c.Selection)).
		Padding(0, 1).Render(content)
}

// achRowLabel is a row's label, bright when the row has the keys.
func (a *App) achRowLabel(text string, focused bool) string {
	c := a.theme.Colors
	if focused {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true).Render("▶ " + fmt.Sprintf("%-7s", text))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Render("  " + fmt.Sprintf("%-7s", text))
}

// achSearchRow is the search box.
func (a *App) achSearchRow(width int) string {
	c := a.theme.Colors
	s := a.settingsState
	focused := s.AchFocus == achFocusSearch
	var box string
	switch {
	case s.AchQuery == "" && !focused:
		box = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Italic(true).Render("press / to search")
	default:
		box = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground)).Render(s.AchQuery)
		if focused {
			box += lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Render("█")
		}
	}
	return ansi.Truncate(a.achRowLabel("SEARCH", focused)+zone.Mark("ach-search", box), width, "…")
}

// achChoiceRow is a row of choices (servers or plugins), scrolled to keep
// the chosen one in view, each clickable.
func (a *App) achChoiceRow(title string, focused bool, names, keys []string, sel, width int) string {
	c := a.theme.Colors
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Background(lipgloss.Color(c.Purple)).Bold(true).Padding(0, 1)
	chosen := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Background(lipgloss.Color(c.Selection)).Bold(true).Padding(0, 1)
	off := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground)).Background(lipgloss.Color(c.Selection)).Padding(0, 1)
	rendered := make([]string, len(names))
	for i, n := range names {
		st := off
		if i == sel {
			st = chosen
			if focused {
				st = on
			}
		}
		rendered[i] = zone.Mark(keys[i], st.Render(n))
	}
	head := a.achRowLabel(title, focused)
	room := width - lipgloss.Width(head)
	// Drop entries from the front until the chosen one fits.
	first := 0
	for first < sel && lipgloss.Width(strings.Join(rendered[first:sel+1], " ")) > room-4 {
		first++
	}
	bar := strings.Join(rendered[first:], " ")
	if first > 0 {
		bar = "‹ " + bar
	}
	return head + ansi.Truncate(bar, room, "›")
}

// achEntry is one achievement row, with the text a search looks in.
type achEntry struct {
	line, text string
}

// achListLines is the list under the rows: the chosen sections, filtered by
// the search.
func (a *App) achListLines(width int) []string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	q := strings.TrimSpace(a.settingsState.AchQuery)
	secs := a.achSections()
	var lines []string
	if q == "" {
		for i, t := range secs {
			if i > 0 {
				lines = append(lines, "", dim.Render(strings.Repeat("─", min(width, 40))), "")
			}
			if t.sc == nil {
				lines = append(lines, a.concordAchievementLines(width)...)
			} else {
				lines = append(lines, a.pluginAchievementLines(t, width, len(secs) == 1)...)
			}
		}
		return lines
	}
	for _, t := range secs {
		var head string
		var entries []achEntry
		if t.sc == nil {
			head, entries = "Concord", a.concordEntries(width)
		} else {
			head, entries = t.title, a.pluginEntries(t, width)
		}
		whole := fuzzyMatch(q, head)
		var rows []string
		for _, e := range entries {
			if whole || fuzzyMatch(q, e.text) {
				rows = append(rows, e.line)
			}
		}
		if len(rows) == 0 {
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		lines = append(lines, purple.Render(head))
		lines = append(lines, rows...)
	}
	if len(lines) == 0 {
		lines = []string{dim.Render(fmt.Sprintf("  Nothing matches %q here.", q))}
	}
	return lines
}

// fuzzyMatch reports whether every word of the query appears in s, its
// letters in order (not necessarily together), ignoring case.
func fuzzyMatch(query, s string) bool {
	s = strings.ToLower(s)
	for _, word := range strings.Fields(strings.ToLower(query)) {
		rest := s
		for _, r := range word {
			i := strings.IndexRune(rest, r)
			if i < 0 {
				return false
			}
			rest = rest[i+len(string(r)):]
		}
	}
	return true
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

// concordEntries are Concord's own achievements, secret ones left out
// until they're unlocked.
func (a *App) concordEntries(width int) []achEntry {
	coll := a.coll()
	var out []achEntry
	for _, ach := range achievements {
		date := coll.Achievements[ach.id]
		if date == "" && ach.secret {
			continue
		}
		text := ach.description()
		if date != "" {
			text = ach.name + " " + text
		}
		out = append(out, achEntry{text: text, line: a.achLine("", map[string]string{"🥉": "bronze", "🥈": "silver", "🥇": "gold"}[achievementTiers[ach.id]],
			ach.name, ach.description(), date, width)})
	}
	return out
}

// concordAchievementLines is Concord's section: the collection achievements.
func (a *App) concordAchievementLines(width int) []string {
	coll := a.coll()
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	earned, hidden := 0, 0
	for _, ach := range achievements {
		if coll.Achievements[ach.id] != "" {
			earned++
		} else if ach.secret {
			hidden++
		}
	}
	lines := []string{purple.Render(fmt.Sprintf("Concord  %d/%d", earned, len(achievements))), ""}
	for _, e := range a.concordEntries(width) {
		lines = append(lines, e.line)
	}
	if hidden > 0 {
		lines = append(lines, dim.Render(fmt.Sprintf("  … and %d secret ones", hidden)))
	}
	return lines
}

// pluginEntries are a plugin's achievements, secret ones left out until
// they're unlocked.
func (a *App) pluginEntries(t achTab, width int) []achEntry {
	t.sc.mu.RLock()
	rec := t.sc.PluginRecords[t.board.PluginID]
	t.sc.mu.RUnlock()
	unlocked := map[string]string{}
	for _, u := range rec.Unlocked {
		unlocked[u.ID] = u.At.Local().Format("2006-01-02")
	}
	var out []achEntry
	for _, d := range t.board.Achievements {
		date := unlocked[d.ID]
		if date == "" && d.Secret {
			continue
		}
		text := d.Description
		if date != "" {
			text = d.Name + " " + text
		}
		out = append(out, achEntry{text: text, line: a.achLine(d.Icon, d.Tier, d.Name, d.Description, date, width)})
	}
	return out
}

// pluginAchievementLines is a plugin's section: your stats, its
// leaderboard (when it's the only section), and its achievements.
func (a *App) pluginAchievementLines(t achTab, width int, withBoard bool) []string {
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
	if withBoard && t.board.LeaderboardStat != "" {
		title := "Leaderboard"
		if t.board.LeaderboardLabel != "" {
			title += " · " + t.board.LeaderboardLabel
		}
		lines = append(lines, "", purple.Render(title), "")
		lines = append(lines, a.leaderboardLines(t, rec)...)
	}

	// Achievements.
	if len(t.board.Achievements) > 0 {
		unlocked, hidden := 0, 0
		got := map[string]bool{}
		for _, u := range rec.Unlocked {
			got[u.ID] = true
		}
		for _, d := range t.board.Achievements {
			if got[d.ID] {
				unlocked++
			} else if d.Secret {
				hidden++
			}
		}
		lines = append(lines, "", purple.Render(fmt.Sprintf("Achievements  %d/%d", unlocked, len(t.board.Achievements))), "")
		for _, e := range a.pluginEntries(t, width) {
			lines = append(lines, e.line)
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
