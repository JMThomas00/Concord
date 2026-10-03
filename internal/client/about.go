package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Settings > About: the client's (and connected server's) build, this
// launch's mood with its code (L locks it for every launch, N rolls a new
// one), and the collection: what you've discovered of each layer, easter
// eggs, and achievements. Everything here is kept in ~/.concord/ and never
// sent anywhere.

// aboutShowing reports whether Settings > About is on screen.
func (a *App) aboutShowing() bool {
	s := a.settingsState
	return a.view == ViewSettings && s != nil && s.SelectedCategory == settingsCatAbout
}

// handleAboutKey handles the About page's keys; false for keys it leaves
// to Settings.
func (a *App) handleAboutKey(msg tea.KeyMsg) bool {
	s := a.settingsState
	switch msg.String() {
	case "up", "k":
		s.AboutScroll = max(0, s.AboutScroll-1)
	case "down", "j":
		s.AboutScroll++
	case "pgup":
		s.AboutScroll = max(0, s.AboutScroll-10)
	case "pgdown":
		s.AboutScroll += 10
	case "a", "A":
		a.openAchievements()
	case "l", "L":
		a.toggleMoodLock()
	case "n", "N":
		a.rerollMood()
	case "c", "C": // choose a bottle in the cellar (newest first)
		if n := len(a.coll().Cellar); n > 0 {
			if s.AboutCellar <= 0 || s.AboutCellar >= n {
				s.AboutCellar = n - 1
			} else {
				s.AboutCellar--
			}
		}
	case "u", "U": // uncork it: that mood next launch
		if c := a.coll().Cellar; s.AboutCellar >= 0 && s.AboutCellar < len(c) && a.uiConfig != nil {
			a.uiConfig.Display.MoodLock = c[s.AboutCellar].Code
			a.saveDisplayConfig()
			a.toasts = append(a.toasts, &toast{label: "🍾 Uncorked", title: c[s.AboutCellar].Code + " is your mood next launch"})
		}
	default:
		return false
	}
	return true
}

// renderAboutContent renders the About category: the client binary's own
// build identity (set once at startup via SetBuildInfo), the connected
// server's if one is connected (from its Ready payload), and the mood and
// collection beside them when there's room.
func (a *App) renderAboutContent(width, height int) string {
	if s := a.settingsState; s != nil && s.AboutAch {
		return a.renderAchievementsPage(width, height)
	}
	// Top: header + subtitle + blank + separator: the 4 base lines, no extra.
	// Bottom: separator + help line = 2 lines → pageBottomExtra = 0
	layout := calculateSettingsLayout(width, height, 0, 0)
	const leftW, gap = 44, 4 // the two columns, when there's room for both
	c := a.theme.Colors
	label := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	normal := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Foreground))
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	purple := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(label.Render("About"))
	top.writeLine(dim.Render("Build information, your mood, and what you've found"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// Left: build info and the mood.
	var left []string
	// One line per detail, so the mood below fits without scrolling.
	row := func(name, value string) {
		left = append(left, "  "+label.Render(fmt.Sprintf("%-11s", name))+" "+normal.Render(value))
	}
	left = append(left, purple.Render("Concord Client"))
	row("Version", a.clientVersion)
	row("Git Commit", a.clientGitCommit)
	row("Build Time", a.clientBuildTime)
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		v, g, b := a.activeConn.ServerVersion, a.activeConn.ServerGitCommit, a.activeConn.ServerBuildTime
		a.activeConn.mu.RUnlock()
		left = append(left, "", purple.Render("Connected Server"))
		row("Version", v)
		row("Git Commit", g)
		row("Build Time", b)
	}
	left = append(left, "", dim.Render(strings.Repeat("─", leftW-2)), "", purple.Render("Your Mood"), "")
	lock := dim.Render("  L lock it · N new one")
	if a.uiConfig != nil && a.uiConfig.Display.MoodLock != "" {
		lock = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)).Render("  🔒 locked") + dim.Render(" · L unlock")
	}
	left = append(left, "    "+purple.Render(a.mood.code())+lock)
	name, notes := a.mood.label(moodClock().Year())
	for _, l := range strings.Split(lipgloss.NewStyle().Width(leftW-6).Render(name), "\n") {
		left = append(left, "    "+normal.Render(strings.TrimRight(l, " "))) // a long name wraps inside the column
	}
	for _, l := range strings.Split(lipgloss.NewStyle().Width(leftW-6).Render(notes), "\n") {
		left = append(left, "    "+dim.Italic(true).Render(strings.TrimRight(l, " ")))
	}
	left = append(left, "")
	for _, l := range moodLayers {
		if l.id == layerLoading || l.id == layerTransition {
			continue // they've played already; the collection shows them
		}
		if o, ok := findOption(l.id, a.mood.picks[l.id]); ok {
			name := o.name
			if o.rarity != common {
				name += dim.Render("  " + o.rarity.String())
			}
			left = append(left, dim.Render(fmt.Sprintf("    %-16s", l.name))+normal.Render(name))
		}
	}
	if lvl := a.surprise(); lvl != surpriseFull {
		left = append(left, "", dim.Render("    Surprise Me is "+[]string{"", "calm", "off"}[lvl]+" (Settings > Display)"))
	}

	// Right: the collection and achievements.
	rightW := layout.interiorWidth
	if layout.interiorWidth >= leftW+gap+50 {
		rightW -= leftW + gap
	}
	coll := a.coll()
	var right []string
	right = append(right, purple.Render("Collection"))
	bar := func(n, total int) string {
		const w = 12
		f := 0
		if total > 0 {
			f = n * w / total
		}
		return purple.Render(strings.Repeat("▰", f)) + dim.Render(strings.Repeat("▱", w-f))
	}
	entry := func(name string, n, total int) {
		right = append(right, fmt.Sprintf("  %s %s %s", dim.Render(fmt.Sprintf("%-16s", name)), bar(n, total),
			normal.Render(fmt.Sprintf("%d/%d", n, total))))
	}
	entry("Banners", len(coll.Banners), len(banners))
	for _, l := range moodLayers {
		entry(l.name, coll.seenCount(l.id), len(l.options))
	}
	right = append(right, fmt.Sprintf("  %s %s", dim.Render(fmt.Sprintf("%-16s", "Easter eggs")),
		normal.Render(fmt.Sprintf("%d found", len(coll.Eggs)))))
	right = append(right, fmt.Sprintf("  %s %s", dim.Render(fmt.Sprintf("%-16s", "Launches")),
		normal.Render(fmt.Sprint(coll.Launches))))

	// The cellar: every legendary seen, newest first.
	if len(coll.Cellar) > 0 {
		right = append(right, "", purple.Render(fmt.Sprintf("Cellar  %d", len(coll.Cellar))),
			dim.Render("  Legendary moods you've seen, kept so you can see them again."),
			dim.Render("  C picks a bottle; U uncorks it: that mood comes back next launch."), "")
		sel := -1
		if a.settingsState != nil {
			sel = a.settingsState.AboutCellar
		}
		for i := len(coll.Cellar) - 1; i >= 0; i-- {
			b := coll.Cellar[i]
			name := b.Option
			if o, ok := findOption(moodLayer(b.Layer), b.Option); ok {
				name = o.name
			}
			mark := "  🍾 "
			line := normal.Render(fmt.Sprintf("%-20s", name)) + dim.Render(fmt.Sprintf("  %s  %s", b.Code, b.Date))
			if i == sel {
				mark = "  ▸ "
				line = purple.Render(fmt.Sprintf("%-20s", name)) + dim.Render(fmt.Sprintf("  %s  %s", b.Code, b.Date))
			}
			right = append(right, mark+line)
		}
	}

	// Achievements live on their own page now (achievements_page.go): a
	// tab for Concord's and one per plugin, with leaderboards.
	earned := 0
	for _, ach := range achievements {
		if coll.Achievements[ach.id] != "" {
			earned++
		}
	}
	tabs := a.achievementTabs()
	right = append(right, "", purple.Render(fmt.Sprintf("Achievements  %d/%d", earned, len(achievements))))
	more := "  A opens them"
	if n := len(tabs) - 1; n > 0 {
		more += fmt.Sprintf(", with %d plugin%s and leaderboards", n, plural(n))
	}
	right = append(right, dim.Render(more))

	// Side by side when there's room, else one after the other.
	var lines []string
	if layout.interiorWidth >= leftW+gap+50 {
		joined := lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(leftW).Render(strings.Join(left, "\n")), strings.Repeat(" ", gap),
			strings.Join(right, "\n"))
		lines = strings.Split(joined, "\n")
	} else {
		lines = append(append(left, ""), right...)
	}

	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	s := a.settingsState
	maxScroll := max(0, len(lines)-layout.middleLines)
	if s != nil {
		s.AboutScroll = min(s.AboutScroll, maxScroll)
		lines = lines[s.AboutScroll:]
	}
	for i, l := range lines {
		if i >= layout.middleLines {
			break
		}
		middle.writeLine(l)
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	help := "A achievements · L lock mood · N new mood · Tab back to menu · Esc close"
	if len(coll.Cellar) > 0 {
		help = "C/U cellar · " + help
	}
	if maxScroll > 0 {
		help = "↑↓ scroll · " + help
	}
	bottom.writeLine(dim.Render(help))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height-2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Selection)).
		Padding(0, 1).Render(content)
}
