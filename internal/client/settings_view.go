package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
)

// openSettings transitions the app into the Settings view
func (a *App) openSettings(returnTo View) {
	names := themes.ListAvailableThemes()
	if len(names) == 0 {
		names = []string{"dracula"}
	}

	// Find current theme in the list
	currentIdx := 0
	originalTheme := "dracula"
	if a.uiConfig != nil {
		originalTheme = a.uiConfig.Theme
		for i, n := range names {
			if n == a.uiConfig.Theme {
				currentIdx = i
				break
			}
		}
	}

	categories := []string{"Theme", "Manage Servers", "Notifications", "Keybindings"}

	a.settingsState = &SettingsState{
		Categories:       categories,
		SelectedCategory: 0,
		FocusOnForm:      false,
		PreviousView:     returnTo,
		AvailableThemes:  names,
		SelectedTheme:    currentIdx,
		OriginalTheme:    originalTheme,
		SelectedServer:   0,
		ServerFormOpen:   false,
	}
	a.view = ViewSettings
}

// handleSettingsKey processes key events when in ViewSettings
func (a *App) handleSettingsKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		a.view = ViewMain
		return nil
	}

	switch msg.String() {
	case "esc":
		// Cancel and return to previous view
		// Restore original theme if in Theme category
		if s.SelectedCategory == 0 && s.OriginalTheme != "" {
			a.applyAndSaveTheme(s.OriginalTheme)
		}
		returnTo := s.PreviousView
		a.settingsState = nil
		a.view = returnTo

	case "up", "k":
		if !s.FocusOnForm {
			// Navigate categories
			if s.SelectedCategory > 0 {
				s.SelectedCategory--
			}
		} else {
			// Navigate within form content
			switch s.SelectedCategory {
			case 0: // Theme category
				if s.SelectedTheme > 0 {
					s.SelectedTheme--
					a.previewTheme(s.AvailableThemes[s.SelectedTheme])
				}
			case 1: // Manage Servers category
				if s.SelectedServer > 0 {
					s.SelectedServer--
				}
			}
		}

	case "down", "j":
		if !s.FocusOnForm {
			// Navigate categories
			if s.SelectedCategory < len(s.Categories)-1 {
				s.SelectedCategory++
			}
		} else {
			// Navigate within form content
			switch s.SelectedCategory {
			case 0: // Theme category
				if s.SelectedTheme < len(s.AvailableThemes)-1 {
					s.SelectedTheme++
					a.previewTheme(s.AvailableThemes[s.SelectedTheme])
				}
			case 1: // Manage Servers category
				serverCount := len(a.connMgr.GetAllConnections())
				if s.SelectedServer < serverCount-1 {
					s.SelectedServer++
				}
			}
		}

	case "tab":
		// Toggle between category list and form content
		s.FocusOnForm = !s.FocusOnForm

	case "enter":
		if !s.FocusOnForm {
			// Enter into the selected category
			s.FocusOnForm = true
		} else {
			// Confirm action in form
			switch s.SelectedCategory {
			case 0: // Theme category - save theme
				chosen := s.AvailableThemes[s.SelectedTheme]
				a.applyAndSaveTheme(chosen)
				a.statusMessage = fmt.Sprintf("Theme set to %q", themes.GetThemeDisplayName(chosen))
			}
		}
	}

	return nil
}

// renderSettingsView renders the Settings view
func (a *App) renderSettingsView() string {
	s := a.settingsState
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

	// Split: left categories (~24 chars) | right content (rest)
	catWidth := 24
	contentWidth := totalWidth - catWidth - 1
	if contentWidth < 40 {
		contentWidth = 40
	}

	// ── Left: Category list ────────────────────────────────────────
	var catBuf strings.Builder
	catHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(catWidth - 2)
	catBuf.WriteString(catHeaderStyle.Render("SETTINGS"))
	catBuf.WriteString("\n")
	catBuf.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(catWidth - 2).
		Render("↑↓ navigate · Tab switch"))
	catBuf.WriteString("\n\n")

	for i, category := range s.Categories {
		var line string
		selected := i == s.SelectedCategory

		if selected && !s.FocusOnForm {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(catWidth - 2).
				Render("▶ " + category)
		} else if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(catWidth - 2).
				Render("▶ " + category)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(catWidth - 2).
				Render("  " + category)
		}
		catBuf.WriteString(line)
		catBuf.WriteString("\n")
	}

	catPanel := lipgloss.NewStyle().
		Width(catWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Render(catBuf.String())

	// ── Right: Content panel ───────────────────────────────────────
	var contentBuf strings.Builder

	// Render content based on selected category
	switch s.SelectedCategory {
	case 0: // Theme category
		contentBuf.WriteString(a.renderThemeContent(s, contentWidth))
	case 1: // Manage Servers category
		contentBuf.WriteString(a.renderManageServersContent(s, contentWidth))
	case 2: // Notifications category
		contentBuf.WriteString(a.renderNotificationsContent(contentWidth))
	case 3: // Keybindings category
		contentBuf.WriteString(a.renderKeybindingsContent(contentWidth))
	default:
		contentBuf.WriteString("Coming soon...")
	}

	contentPanel := lipgloss.NewStyle().
		Width(contentWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(contentBuf.String())

	// ── Assemble ───────────────────────────────────────────────────
	content := lipgloss.JoinHorizontal(lipgloss.Top, catPanel, contentPanel)

	// Title bar
	titleBar := lipgloss.NewStyle().
		Width(totalWidth).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Render("  Concord Settings  •  Esc: Cancel")

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
}

// renderThemeContent renders the theme selection panel with live preview
func (a *App) renderThemeContent(s *SettingsState, width int) string {
	var buf strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Theme Selection"))
	buf.WriteString("\n")

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(subtitleStyle.Render("Choose your terminal color theme"))
	buf.WriteString("\n\n")

	// Get current theme for preview
	currentTheme := a.theme
	
	// Preview section
	previewHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Bold(true)
	variantText := ""
	if currentTheme.Meta.Variant != "" {
		variantText = fmt.Sprintf(" (%s)", currentTheme.Meta.Variant)
	}
	buf.WriteString(previewHeaderStyle.Render(fmt.Sprintf("Preview: %s%s", currentTheme.Meta.Name, variantText)))
	buf.WriteString("\n")

	// Author
	if currentTheme.Meta.Author != "" {
		authorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		buf.WriteString(authorStyle.Render(fmt.Sprintf("by %s", currentTheme.Meta.Author)))
		buf.WriteString("\n")
	}
	buf.WriteString("\n")

	// Color swatches (2 rows of 3)
	colors := []struct{ color, name string }{
		{currentTheme.Colors.Purple, currentTheme.Colors.Purple},
		{currentTheme.Colors.Cyan, currentTheme.Colors.Cyan},
		{currentTheme.Colors.Green, currentTheme.Colors.Green},
		{currentTheme.Colors.Orange, currentTheme.Colors.Orange},
		{currentTheme.Colors.Red, currentTheme.Colors.Red},
		{currentTheme.Colors.Yellow, currentTheme.Colors.Yellow},
	}

	// First row of swatches
	for i := 0; i < 3; i++ {
		swatch := lipgloss.NewStyle().
			Background(lipgloss.Color(colors[i].color)).
			Foreground(lipgloss.Color(colors[i].color)).
			Render("   ")
		buf.WriteString(swatch)
		buf.WriteString(" ")
		buf.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Render(colors[i].name))
		if i < 2 {
			buf.WriteString("  ")
		}
	}
	buf.WriteString("\n")

	// Second row of swatches
	for i := 3; i < 6; i++ {
		swatch := lipgloss.NewStyle().
			Background(lipgloss.Color(colors[i].color)).
			Foreground(lipgloss.Color(colors[i].color)).
			Render("   ")
		buf.WriteString(swatch)
		buf.WriteString(" ")
		buf.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Render(colors[i].name))
		if i < 5 {
			buf.WriteString("  ")
		}
	}
	buf.WriteString("\n\n")

	// Theme count
	themeCountStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(themeCountStyle.Render(fmt.Sprintf("%d themes available", len(s.AvailableThemes))))
	buf.WriteString("\n\n")

	// Separator
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	// Scrollable theme list - show current selection area
	visibleStart := 0
	visibleEnd := len(s.AvailableThemes)
	const maxVisible = 12
	
	if len(s.AvailableThemes) > maxVisible {
		halfVisible := maxVisible / 2
		visibleStart = s.SelectedTheme - halfVisible
		visibleEnd = s.SelectedTheme + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(s.AvailableThemes) {
			visibleEnd = len(s.AvailableThemes)
			visibleStart = visibleEnd - maxVisible
			if visibleStart < 0 {
				visibleStart = 0
			}
		}
	}

	// Show "↑ X more" if not at top
	if visibleStart > 0 {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		buf.WriteString(moreStyle.Render(fmt.Sprintf("  ↑ %d more", visibleStart)))
		buf.WriteString("\n")
	}

	// Show visible themes
	for i := visibleStart; i < visibleEnd; i++ {
		displayName := themes.GetThemeDisplayName(s.AvailableThemes[i])

		var line string
		prefix := "  "
		if i == s.SelectedTheme {
			prefix = "⚑ "
			if s.FocusOnForm {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Background)).
					Background(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(width - 4).
					Render(prefix + displayName)
			} else {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(width - 4).
					Render(prefix + displayName)
			}
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 4).
				Render(prefix + displayName)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(s.AvailableThemes) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		buf.WriteString(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(s.AvailableThemes)-visibleEnd)))
		buf.WriteString("\n")
	}

	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	// Navigation help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(helpStyle.Render("Navigation: ↑↓ select · Enter apply · Esc cancel · Tab back to sections"))

	return buf.String()
}

// renderManageServersContent renders the server management panel
func (a *App) renderManageServersContent(s *SettingsState, width int) string {
	var buf strings.Builder

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Manage Servers"))
	buf.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(helpStyle.Render("Server management - Coming soon"))
	buf.WriteString("\n\n")

	servers := a.connMgr.GetAllConnections()
	for i, srv := range servers {
		name := srv.ServerInfo.Name
		if name == "" {
			name = srv.ServerInfo.Address
		}

		var line string
		if i == s.SelectedServer && s.FocusOnForm {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(width - 4).
				Render("▶ " + name)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 4).
				Render("  " + name)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	return buf.String()
}

// renderNotificationsContent renders the notifications settings panel
func (a *App) renderNotificationsContent(width int) string {
	var buf strings.Builder

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Notifications"))
	buf.WriteString("\n\n")

	buf.WriteString("Notification settings - Coming soon\n")

	return buf.String()
}

// renderKeybindingsContent renders the keybindings settings panel
func (a *App) renderKeybindingsContent(width int) string {
	var buf strings.Builder

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Keybindings"))
	buf.WriteString("\n\n")

	buf.WriteString("Keybinding customization - Coming soon\n")

	return buf.String()
}
