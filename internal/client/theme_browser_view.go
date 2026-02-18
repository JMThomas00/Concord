package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
)

// ThemeBrowserState holds state for the interactive theme selection UI
type ThemeBrowserState struct {
	ThemeNames    []string      // all available theme slugs
	SelectedIndex int           // cursor position in the list
	PreviousTheme *themes.Theme // theme active before the browser was opened
	PreviousView  View          // view to return to on Esc/Enter
}

// openThemeBrowser transitions the app into the theme browser.
// Call from any view with a Ctrl+T or /theme action.
func (a *App) openThemeBrowser(returnTo View) {
	names := themes.ListAvailableThemes()
	if len(names) == 0 {
		names = []string{"dracula"}
	}

	// Find current theme in the list so the cursor starts there
	currentIdx := 0
	if a.uiConfig != nil {
		for i, n := range names {
			if n == a.uiConfig.Theme {
				currentIdx = i
				break
			}
		}
	}

	a.themeBrowserState = &ThemeBrowserState{
		ThemeNames:    names,
		SelectedIndex: currentIdx,
		PreviousTheme: a.theme,
		PreviousView:  returnTo,
	}
	a.view = ViewThemeBrowser
}

// handleThemeBrowserKey processes key events when in ViewThemeBrowser.
func (a *App) handleThemeBrowserKey(msg tea.KeyMsg) tea.Cmd {
	s := a.themeBrowserState
	if s == nil {
		a.view = ViewMain
		return nil
	}

	switch msg.String() {
	case "up", "k":
		if s.SelectedIndex > 0 {
			s.SelectedIndex--
			a.previewTheme(s.ThemeNames[s.SelectedIndex])
		}

	case "down", "j":
		if s.SelectedIndex < len(s.ThemeNames)-1 {
			s.SelectedIndex++
			a.previewTheme(s.ThemeNames[s.SelectedIndex])
		}

	case "enter":
		// Confirm selection — save to config
		chosen := s.ThemeNames[s.SelectedIndex]
		a.applyAndSaveTheme(chosen)
		returnTo := s.PreviousView
		a.themeBrowserState = nil
		a.view = returnTo
		a.statusMessage = fmt.Sprintf("Theme set to %q", themes.GetThemeDisplayName(chosen))

	case "esc", "ctrl+t":
		// Cancel — restore previous theme
		if s.PreviousTheme != nil {
			a.SetTheme(s.PreviousTheme)
		}
		returnTo := s.PreviousView
		a.themeBrowserState = nil
		a.view = returnTo
	}

	return nil
}

// previewTheme applies a theme temporarily (without saving) for live preview.
func (a *App) previewTheme(name string) {
	t, err := themes.GetTheme(name)
	if err != nil {
		return
	}
	a.SetTheme(t)
}

// applyAndSaveTheme applies a theme and persists it to config.json.
func (a *App) applyAndSaveTheme(name string) {
	t, err := themes.GetTheme(name)
	if err != nil {
		return
	}
	a.SetTheme(t)

	if a.uiConfig != nil {
		a.uiConfig.Theme = name
	}
	if a.configMgr != nil {
		cfg, err := a.configMgr.LoadAppConfig()
		if err != nil || cfg == nil {
			cfg = &AppConfig{Version: 1}
		}
		cfg.UI.Theme = name
		_ = a.configMgr.SaveAppConfig(cfg)
	}
}

// renderThemeBrowserView renders the full-screen theme browser.
func (a *App) renderThemeBrowserView() string {
	s := a.themeBrowserState
	if s == nil {
		return ""
	}

	totalWidth := a.width
	totalHeight := a.height
	if totalWidth < 40 {
		totalWidth = 40
	}
	if totalHeight < 10 {
		totalHeight = 10
	}

	// Split: left list (~28 chars) | right preview (rest)
	listWidth := 28
	previewWidth := totalWidth - listWidth - 1 // -1 for separator
	if previewWidth < 30 {
		previewWidth = 30
	}
	innerHeight := totalHeight - 4 // title + border

	// ── Left: theme list ──────────────────────────────────────────
	var listBuf strings.Builder
	listHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(listWidth - 2)
	listBuf.WriteString(listHeaderStyle.Render("SELECT THEME"))
	listBuf.WriteString("\n")
	listBuf.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(listWidth - 2).
		Render("↑↓ navigate · Enter save · Esc cancel"))
	listBuf.WriteString("\n\n")

	for i, slug := range s.ThemeNames {
		displayName := themes.GetThemeDisplayName(slug)
		// Truncate if needed
		maxLen := listWidth - 6
		if len([]rune(displayName)) > maxLen {
			displayName = string([]rune(displayName)[:maxLen-1]) + "…"
		}

		var line string
		if i == s.SelectedIndex {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(listWidth - 2).
				Render("▶ " + displayName)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(listWidth - 2).
				Render("  " + displayName)
		}
		listBuf.WriteString(line)
		listBuf.WriteString("\n")
	}

	listPanel := lipgloss.NewStyle().
		Width(listWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Render(listBuf.String())

	// ── Right: live preview ───────────────────────────────────────
	var prevBuf strings.Builder
	t := a.theme // currently previewed theme (already applied)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Purple)).
		Bold(true)
	prevBuf.WriteString(titleStyle.Render(t.Meta.Name))
	if t.Meta.Author != "" {
		prevBuf.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Colors.Comment)).
			Render("  by " + t.Meta.Author))
	}
	prevBuf.WriteString("\n\n")

	// Color swatches
	swatchLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Comment)).Render
	swatch := func(color, label string) string {
		block := lipgloss.NewStyle().
			Background(lipgloss.Color(color)).
			Foreground(lipgloss.Color(color)).
			Render("  ")
		return block + " " + swatchLabel(label)
	}
	swatches := []string{
		swatch(t.Colors.Background, "Background"),
		swatch(t.Colors.Foreground, "Foreground"),
		swatch(t.Colors.Purple, "Purple"),
		swatch(t.Colors.Cyan, "Cyan"),
		swatch(t.Colors.Green, "Green"),
		swatch(t.Colors.Red, "Red"),
		swatch(t.Colors.Orange, "Orange"),
		swatch(t.Colors.Yellow, "Yellow"),
	}
	cols := 2
	for i := 0; i < len(swatches); i += cols {
		for j := 0; j < cols && i+j < len(swatches); j++ {
			if j > 0 {
				prevBuf.WriteString("   ")
			}
			prevBuf.WriteString(lipgloss.NewStyle().Width((previewWidth-6)/cols).Render(swatches[i+j]))
		}
		prevBuf.WriteString("\n")
	}

	prevBuf.WriteString("\n")

	// Sample chat preview
	chatHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Comment)).Bold(true)
	prevBuf.WriteString(chatHeaderStyle.Render("── Chat Preview ──"))
	prevBuf.WriteString("\n")

	sampleMessages := []struct{ name, color, text string }{
		{"alice", t.Semantic.ChatUsernameOther, "Hey everyone! Welcome to Concord."},
		{"you", t.Semantic.ChatUsernameSelf, "Thanks! Love the new " + t.Meta.Name + " theme."},
		{"bob", t.Semantic.ChatUsernameOther, "Don't forget: @you mentioned something!"},
	}
	for _, msg := range sampleMessages {
		nameStr := lipgloss.NewStyle().Foreground(lipgloss.Color(msg.color)).Bold(true).Render(msg.name)
		// highlight @you in mentions
		content := msg.text
		if strings.Contains(content, "@you") {
			parts := strings.SplitN(content, "@you", 2)
			mentionStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(t.Semantic.ChatMention)).Bold(true).Render("@you")
			content = parts[0] + mentionStr + parts[1]
		}
		prevBuf.WriteString(nameStr + "  " + content + "\n")
	}

	prevBuf.WriteString("\n")

	// Status bar preview
	statusStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(t.Colors.Selection)).
		Foreground(lipgloss.Color(t.Colors.Foreground)).
		Width(previewWidth - 4)
	prevBuf.WriteString(statusStyle.Render(
		fmt.Sprintf(" [you@server:8080] #general  variant: %s", t.Meta.Variant)))

	_ = innerHeight // used implicitly through Height() below

	previewPanel := lipgloss.NewStyle().
		Width(previewWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(prevBuf.String())

	// ── Assemble ──────────────────────────────────────────────────
	content := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, previewPanel)

	// Title bar
	titleBar := lipgloss.NewStyle().
		Width(totalWidth).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Render("  Concord Theme Browser")

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
}
