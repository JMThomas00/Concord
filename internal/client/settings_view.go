package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/themes"
	"github.com/google/uuid"
)

// Settings page layout configuration
const (
	// Fixed section sizes (lines of content, NOT including borders)
	settingsTopBaseLines    = 4  // Header + subtitle + status + separator
	settingsBottomBaseLines = 3  // Separator + navigation (1-2 lines)

	// Page-specific top section additions
	themePreviewLines = 9  // Preview header, author, swatches (2 rows), theme count
	defaultStatusLines = 3 // "Feature in development", blank line, centered message

	// Page-specific bottom section additions
	themeBottomExtraLines = 1  // Extra padding for theme page footer

	// Minimum middle section height for scrolling
	settingsMinMiddleLines = 5

	// Border accounting
	settingsBorderLines = 2  // RoundedBorder adds 2 lines (top + bottom)
	settingsBorderChars = 2  // RoundedBorder adds 2 chars to width (left + right)
)

// ═══════════════════════════════════════════════════════════════════════════
// SETTINGS PAGE LAYOUT GUIDE - READ BEFORE CREATING NEW PAGES
// ═══════════════════════════════════════════════════════════════════════════
//
// For a complete template and examples, see: SETTINGS_PAGE_TEMPLATE.md
//
// CRITICAL RULE: The +2 Padding Pattern
// -------------------------------------
// ALL settings pages MUST allocate +2 extra lines in the top section beyond
// what they actually write. This padding is REQUIRED for proper border alignment.
//
// Formula:
//   pageTopExtra = (lines you write) - 4 (base) + 2 (padding)
//
// Examples:
//   - 4 lines written (header+subtitle+blank+sep) → pageTopExtra = 2
//   - 5 lines written (header+subtitle+stats+blank+sep) → pageTopExtra = 3
//   - 6 lines written (header+subtitle+stats+filter+blank+sep) → pageTopExtra = 4
//
// Bottom Section:
//   - 1 help line → pageBottomExtra = 0  (separator + help = 2 lines)
//   - 2 help lines → pageBottomExtra = 1  (separator + blank + 2 help = 4 lines)
//
// Content Pages Pattern (NOT sidebars):
//   return lipgloss.NewStyle().
//       Width(width).Height(height).
//       Border(lipgloss.RoundedBorder()).
//       BorderForeground(...).
//       Padding(0, 1).  // ← REQUIRED for content pages!
//       Render(content)
//
// Sidebar Pattern (different!):
//   return lipgloss.NewStyle().
//       Width(width).Height(height).
//       Border(lipgloss.RoundedBorder()).
//       BorderForeground(...).
//       Render(buf.String())  // ← NO Padding() on sidebars!
//
// Why This Works:
//   The +2 padding ensures all pages have consistent top section heights,
//   preventing overflow that would cause lipgloss to truncate and cut off
//   top borders. Without it, borders misalign when switching between pages.
//
// ═══════════════════════════════════════════════════════════════════════════

// settingsLayout calculates exact section heights for a settings page.
type settingsLayout struct {
	topLines      int  // Top section content lines (before middle separator)
	middleLines   int  // Middle section scrollable area lines
	bottomLines   int  // Bottom section content lines (after middle separator)
	interiorWidth int  // Width for content (totalWidth - 4 for padding)

	hasTopSeparator    bool
	hasBottomSeparator bool
}

// calculateSettingsLayout computes the exact line allocations for a settings page.
// pageTopExtra: additional lines for page-specific top content (e.g., theme preview = 9)
// pageBottomExtra: additional lines for page-specific bottom content (e.g., extra padding = 1)
func calculateSettingsLayout(totalWidth, totalHeight, pageTopExtra, pageBottomExtra int) settingsLayout {
	// Interior width for content (account for 2 chars padding on each side)
	interiorWidth := totalWidth - 4
	if interiorWidth < 40 {
		interiorWidth = 40
	}

	// Account for border (2 lines: top + bottom)
	// The content area is totalHeight minus the border
	contentHeight := totalHeight - settingsBorderLines

	// Top section: base + page-specific extras
	topLines := settingsTopBaseLines + pageTopExtra

	// Bottom section: base + page-specific extras
	bottomLines := settingsBottomBaseLines + pageBottomExtra

	// Middle section: remainder
	middleLines := contentHeight - topLines - bottomLines

	// Enforce minimum middle height
	if middleLines < settingsMinMiddleLines {
		middleLines = settingsMinMiddleLines
		// Shrink other sections proportionally if terminal too small
		shrinkAmount := settingsMinMiddleLines - middleLines
		topShrink := shrinkAmount / 2
		bottomShrink := shrinkAmount - topShrink
		topLines -= topShrink
		bottomLines -= bottomShrink
		if topLines < 2 {
			topLines = 2
		}
		if bottomLines < 2 {
			bottomLines = 2
		}
		middleLines = contentHeight - topLines - bottomLines
	}

	return settingsLayout{
		topLines:           topLines,
		middleLines:        middleLines,
		bottomLines:        bottomLines,
		interiorWidth:      interiorWidth,
		hasTopSeparator:    true,
		hasBottomSeparator: true,
	}
}

// settingsSectionBuilder helps construct properly-sized sections.
type settingsSectionBuilder struct {
	buf          *strings.Builder
	linesWritten int
	targetLines  int
	width        int
}

// newSectionBuilder creates a section builder.
func newSectionBuilder(targetLines, width int) *settingsSectionBuilder {
	return &settingsSectionBuilder{
		buf:          &strings.Builder{},
		linesWritten: 0,
		targetLines:  targetLines,
		width:        width,
	}
}

// writeLine writes a line and tracks count.
func (sb *settingsSectionBuilder) writeLine(content string) {
	sb.buf.WriteString(content)
	sb.buf.WriteString("\n")
	sb.linesWritten++
}

// writeBlank writes a blank line.
func (sb *settingsSectionBuilder) writeBlank() {
	sb.buf.WriteString("\n")
	sb.linesWritten++
}

// pad fills remaining lines with blanks to reach targetLines.
func (sb *settingsSectionBuilder) pad() {
	remaining := sb.targetLines - sb.linesWritten
	if remaining > 0 {
		sb.buf.WriteString(strings.Repeat("\n", remaining))
		sb.linesWritten += remaining
	}
}

// String returns the built content.
func (sb *settingsSectionBuilder) String() string {
	return sb.buf.String()
}

// renderSeparator creates a horizontal separator line.
func (a *App) renderSeparator(width int) string {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render(strings.Repeat("─", width))
}

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

	categories := []string{"Theme", "Notifications", "Display", "Manage Servers"}

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
		// If delete confirmation is shown, cancel it
		if a.deleteConfirmServerID != nil {
			a.deleteConfirmServerID = nil
			return nil
		}

		// Cancel and return to previous view
		// Restore original theme ONLY if currently IN the theme form (not just category selected)
		// If user has Tab'd back to categories, theme is already applied - don't revert
		if s.SelectedCategory == 0 && s.FocusOnForm && s.OriginalTheme != "" {
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
			case 3: // Manage Servers category
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
			case 3: // Manage Servers category
				serverCount := len(a.connMgr.GetAllConnections())
				if s.SelectedServer < serverCount-1 {
					s.SelectedServer++
				}
			}
		}

	case "tab":
		// When leaving theme form, auto-apply the selected theme
		if s.FocusOnForm && s.SelectedCategory == 0 {
			chosen := s.AvailableThemes[s.SelectedTheme]
			a.applyAndSaveTheme(chosen)
			a.statusMessage = fmt.Sprintf("Theme set to %q", themes.GetThemeDisplayName(chosen))
		}
		// Toggle between category list and form content
		s.FocusOnForm = !s.FocusOnForm

	case "enter":
		if !s.FocusOnForm {
			// Enter into the selected category
			s.FocusOnForm = true
		} else {
			// Confirm action in form
			switch s.SelectedCategory {
			case 0: // Theme category - save theme and return to categories
				chosen := s.AvailableThemes[s.SelectedTheme]
				a.applyAndSaveTheme(chosen)
				a.statusMessage = fmt.Sprintf("Theme set to %q", themes.GetThemeDisplayName(chosen))
				// Go back to categories list after applying
				s.FocusOnForm = false
			}
		}

	case "shift+up":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Reorder servers: move selected server up
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer > 0 && s.SelectedServer < len(servers) {
				// Swap server positions in the list
				servers[s.SelectedServer], servers[s.SelectedServer-1] =
					servers[s.SelectedServer-1], servers[s.SelectedServer]

				// Reassign Order values based on new positions
				for i, srv := range servers {
					srv.ServerInfo.Order = i
				}

				s.SelectedServer--
				// Save the updated order
				a.saveServerOrder()
			}
		}

	case "shift+down":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Reorder servers: move selected server down
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer >= 0 && s.SelectedServer < len(servers)-1 {
				// Swap server positions in the list
				servers[s.SelectedServer], servers[s.SelectedServer+1] =
					servers[s.SelectedServer+1], servers[s.SelectedServer]

				// Reassign Order values based on new positions
				for i, srv := range servers {
					srv.ServerInfo.Order = i
				}

				s.SelectedServer++
				// Save the updated order
				a.saveServerOrder()
			}
		}

	case "ctrl+n":
		if s.SelectedCategory == 3 {
			// Open add server dialog
			a.editingServerID = nil
			a.initAddServerForm()
			a.addServerName.Focus()
			a.view = ViewAddServer
		}

	case "e":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Edit selected server
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer >= 0 && s.SelectedServer < len(servers) {
				srv := servers[s.SelectedServer]

				// Find this server in clientServers
				for i, cs := range a.clientServers {
					if cs.ID == srv.ServerInfo.ID {
						a.editingServerIndex = i
						serverID := cs.ID
						a.editingServerID = &serverID
						break
					}
				}

				// Pre-fill add server form with existing values
				a.initAddServerForm()
				a.addServerName.SetValue(srv.ServerInfo.Name)
				a.addServerAddress.SetValue(srv.ServerInfo.Address)
				a.addServerPort.SetValue(fmt.Sprintf("%d", srv.ServerInfo.Port))
				a.addServerUseTLS = srv.ServerInfo.UseTLS
				a.addServerName.Focus()
				a.view = ViewAddServer
			}
		}

	case "d":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Delete selected server - show confirmation
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer >= 0 && s.SelectedServer < len(servers) {
				srv := servers[s.SelectedServer]
				serverID := srv.ServerInfo.ID
				a.deleteConfirmServerID = &serverID
			}
		}

	case "y", "Y":
		// Confirm delete (when confirmation dialog is shown)
		if a.deleteConfirmServerID != nil && s.SelectedCategory == 3 {
			serverID := *a.deleteConfirmServerID
			a.deleteConfirmServerID = nil

			// Find the server in clientServers and remove it
			serverFound := false
			for i, cs := range a.clientServers {
				if cs.ID == serverID {
					serverFound = true

					// Remove from config
					if err := a.configMgr.RemoveServer(serverID); err != nil {
						a.statusMessage = fmt.Sprintf("Failed to remove server from config: %v", err)
						a.statusError = true
						return nil
					}

					// Remove from connection manager
					a.connMgr.RemoveServer(serverID)

					// Remove from in-memory clientServers list
					a.clientServers = append(a.clientServers[:i], a.clientServers[i+1:]...)

					// Clear state if deleted server was active
					if a.activeConn != nil && a.activeConn.ServerID == serverID {
						a.activeConn = nil
						a.currentServer = nil
						a.currentChannel = nil
					}

					// Adjust selection
					if s.SelectedServer >= len(a.clientServers) && s.SelectedServer > 0 {
						s.SelectedServer--
					}

					// Clear ping result
					if a.pingResults != nil {
						delete(a.pingResults, serverID)
					}

					a.statusMessage = "Server deleted successfully"
					a.statusError = false
					break
				}
			}

			if !serverFound {
				a.statusMessage = "Error: Server not found"
				a.statusError = true
			}
		}

	case "n", "N":
		// Cancel delete confirmation
		if a.deleteConfirmServerID != nil {
			a.deleteConfirmServerID = nil
			return nil
		}

	case "p":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Ping selected server
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer >= 0 && s.SelectedServer < len(servers) {
				srv := servers[s.SelectedServer]

				// Initialize ping results map if needed
				if a.pingResults == nil {
					a.pingResults = make(map[uuid.UUID]*PingResult)
				}

				// Mark as in progress
				a.pingResults[srv.ServerInfo.ID] = &PingResult{InProgress: true}

				// Trigger ping
				return PingServerCmd(srv.ServerInfo)
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

	// Split: left categories (~24 chars) | right content (rest)
	catWidth := 24
	contentWidth := totalWidth - catWidth - 1
	if contentWidth < 40 {
		contentWidth = 40
	}
	contentHeight := totalHeight - 2

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
		contentBuf.WriteString(a.renderThemeContent(s, contentWidth, contentHeight))
	case 1: // Notifications category
		contentBuf.WriteString(a.renderNotificationsContent(contentWidth, contentHeight))
	case 2: // Display category
		contentBuf.WriteString(a.renderDisplayContent(contentWidth, contentHeight))
	case 3: // Manage Servers category
		contentBuf.WriteString(a.renderManageServersContent(s, contentWidth, contentHeight))
	default:
		contentBuf.WriteString("Coming soon...")
	}

	contentPanel := contentBuf.String()

	// ── Assemble ───────────────────────────────────────────────────
	content := lipgloss.JoinHorizontal(lipgloss.Top, catPanel, contentPanel)

	// Title bar
	titleBar := lipgloss.NewStyle().
		Width(totalWidth).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Render("  Concord Settings  •  Esc: Cancel")

	// Show delete confirmation dialog if active
	if a.deleteConfirmServerID != nil {
		return a.renderDeleteServerConfirmationDialog()
	}

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
}

// renderDeleteServerConfirmationDialog renders a centered delete confirmation dialog
func (a *App) renderDeleteServerConfirmationDialog() string {
	// Find the server being deleted
	var serverName string
	if a.deleteConfirmServerID != nil {
		for _, cs := range a.clientServers {
			if cs.ID == *a.deleteConfirmServerID {
				serverName = cs.Name
				break
			}
		}
	}

	// Dialog dimensions
	dialogWidth := 60
	dialogHeight := 12

	// Calculate centering
	leftPadding := (a.width - dialogWidth) / 2
	topPadding := (a.height - dialogHeight) / 2

	if leftPadding < 0 {
		leftPadding = 0
	}
	if topPadding < 0 {
		topPadding = 0
	}

	// Build dialog content
	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("⚠  Delete Server"))
	content.WriteString("\n\n")

	// Message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	message := fmt.Sprintf("Are you sure you want to delete \"%s\"?", serverName)
	content.WriteString(msgStyle.Render(message))
	content.WriteString("\n\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center).
		Italic(true)

	content.WriteString(warningStyle.Render("This action cannot be undone."))
	content.WriteString("\n\n")

	// Buttons
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	content.WriteString(helpStyle.Render("[Y] Yes, delete  •  [Esc] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Red)).
		Padding(1, 2).
		Width(dialogWidth).
		Height(dialogHeight)

	dialog := dialogStyle.Render(content.String())

	// Center the dialog on screen
	centeredDialog := lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		AlignHorizontal(lipgloss.Center).
		AlignVertical(lipgloss.Center).
		Render(dialog)

	return centeredDialog
}

// renderThemeContent renders the theme selection panel with live preview
func (a *App) renderThemeContent(s *SettingsState, width, height int) string {
	layout := calculateSettingsLayout(width, height, themePreviewLines, themeBottomExtraLines)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Theme Selection"))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Choose your terminal color theme"))
	top.writeBlank()

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
	top.writeLine(previewHeaderStyle.Render(fmt.Sprintf("Preview: %s%s", currentTheme.Meta.Name, variantText)))

	// Author
	if currentTheme.Meta.Author != "" {
		authorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		top.writeLine(authorStyle.Render(fmt.Sprintf("by %s", currentTheme.Meta.Author)))
	}
	top.writeBlank()

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
	var swatchRow1 strings.Builder
	for i := 0; i < 3; i++ {
		swatch := lipgloss.NewStyle().
			Background(lipgloss.Color(colors[i].color)).
			Foreground(lipgloss.Color(colors[i].color)).
			Render("   ")
		swatchRow1.WriteString(swatch)
		swatchRow1.WriteString(" ")
		swatchRow1.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Render(colors[i].name))
		if i < 2 {
			swatchRow1.WriteString("  ")
		}
	}
	top.writeLine(swatchRow1.String())

	// Second row of swatches
	var swatchRow2 strings.Builder
	for i := 3; i < 6; i++ {
		swatch := lipgloss.NewStyle().
			Background(lipgloss.Color(colors[i].color)).
			Foreground(lipgloss.Color(colors[i].color)).
			Render("   ")
		swatchRow2.WriteString(swatch)
		swatchRow2.WriteString(" ")
		swatchRow2.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Render(colors[i].name))
		if i < 5 {
			swatchRow2.WriteString("  ")
		}
	}
	top.writeLine(swatchRow2.String())
	top.writeBlank()

	// Theme count
	themeCountStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(themeCountStyle.Render(fmt.Sprintf("%d themes available", len(s.AvailableThemes))))
	top.writeBlank()

	// Top section separator (last line of top section)
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Calculate scrollable area for theme list
	maxVisible := layout.middleLines - 2 // Reserve space for possible "↑/↓ more" indicators
	if maxVisible < 3 {
		maxVisible = 3
	}

	visibleStart := 0
	visibleEnd := len(s.AvailableThemes)

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
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↑ %d more", visibleStart)))
	}

	// Show visible themes
	for i := visibleStart; i < visibleEnd; i++ {
		displayName := themes.GetThemeDisplayName(s.AvailableThemes[i])

		var line string
		prefix := "  "
		if i == s.SelectedTheme {
			prefix = "▶ "
			if s.FocusOnForm {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Background)).
					Background(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(layout.interiorWidth).
					Render(prefix + displayName)
			} else {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(layout.interiorWidth).
					Render(prefix + displayName)
			}
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(layout.interiorWidth).
				Render(prefix + displayName)
		}
		middle.writeLine(line)
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(s.AvailableThemes) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(s.AvailableThemes)-visibleEnd)))
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator (first line of bottom section)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ preview · Enter apply · Tab apply & back · Esc cancel"))

	// Fill remaining bottom section space
	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderManageServersContent renders the server management panel
func (a *App) renderManageServersContent(s *SettingsState, width, height int) string {
	layout := calculateSettingsLayout(width, height, defaultStatusLines, 0)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Manage Servers"))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Configure your server connections"))

	// Server count/status
	servers := a.connMgr.GetAllConnections()
	connectedCount := 0
	for _, srv := range servers {
		if srv.State == StateReady {
			connectedCount++
		}
	}

	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statusStyle.Render(fmt.Sprintf("%d servers · %d connected", len(servers), connectedCount)))
	top.writeBlank()

	// Top section separator (last line of top section)
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Calculate scrollable area for server list
	maxVisible := layout.middleLines - 2 // Reserve space for possible "↑/↓ more" indicators
	if maxVisible < 3 {
		maxVisible = 3
	}

	visibleStart := 0
	visibleEnd := len(servers)

	if len(servers) > maxVisible {
		halfVisible := maxVisible / 2
		visibleStart = s.SelectedServer - halfVisible
		visibleEnd = s.SelectedServer + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(servers) {
			visibleEnd = len(servers)
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
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↑ %d more", visibleStart)))
	}

	// Show visible servers
	for i := visibleStart; i < visibleEnd; i++ {
		srv := servers[i]
		name := srv.ServerInfo.Name
		if name == "" {
			name = srv.ServerInfo.Address
		}

		// Online indicator
		indicator := "●"
		indicatorColor := a.theme.Colors.Green
		if srv.State != StateReady {
			indicatorColor = a.theme.Colors.Comment
		}

		address := fmt.Sprintf("%s:%d", srv.ServerInfo.Address, srv.ServerInfo.Port)

		// Ping status
		var pingStatus string
		if a.pingResults != nil {
			if result, exists := a.pingResults[srv.ServerInfo.ID]; exists {
				if result.InProgress {
					pingStatus = " (pinging...)"
				} else if result.Success {
					pingStatus = fmt.Sprintf(" (✓ %dms)", result.Latency.Milliseconds())
				} else {
					if result.Attempts > 0 {
						pingStatus = fmt.Sprintf(" (✗ %d attempts)", result.Attempts)
					} else {
						pingStatus = " (✗ failed)"
					}
				}
			}
		}

		var line string
		prefix := "  "
		if i == s.SelectedServer {
			prefix = "▶ "
			if s.FocusOnForm {
				line = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Background)).
					Background(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(layout.interiorWidth).
					Render(fmt.Sprintf("%s%s %s %s%s", prefix, name, indicator, address, pingStatus))
			} else {
				nameStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true)
				indStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(indicatorColor))
				addrStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Comment))

				line = fmt.Sprintf("%s%s %s %s%s",
					prefix,
					nameStyle.Render(name),
					indStyle.Render(indicator),
					addrStyle.Render(address),
					pingStatus)
			}
		} else {
			nameStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			indStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(indicatorColor))
			addrStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment))

			line = fmt.Sprintf("%s%s %s %s%s",
				prefix,
				nameStyle.Render(name),
				indStyle.Render(indicator),
				addrStyle.Render(address),
				pingStatus)
		}
		middle.writeLine(line)
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(servers) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(servers)-visibleEnd)))
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator (first line of bottom section)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help (2 lines)
	navStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(navStyle.Render("Navigation: ↑↓ select · Shift+↑↓ reorder · Esc close"))
	bottom.writeLine(navStyle.Render("Actions: Ctrl+N add · E edit · D delete · P ping"))

	// Fill remaining bottom section space
	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderNotificationsContent renders the notifications settings panel
func (a *App) renderNotificationsContent(width, height int) string {
	layout := calculateSettingsLayout(width, height, defaultStatusLines, 0)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Notifications Settings"))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Configure notification preferences and behavior"))

	// Feature status
	featureStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Orange)).
		Italic(true)
	top.writeLine(featureStyle.Render("Feature in development"))
	top.writeBlank()

	// Top section separator (last line of top section)
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Coming soon message (centered)
	centerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(centerStyle.Render("                         This feature is coming soon!"))
	middle.writeLine(centerStyle.Render("                      Stay tuned for future updates."))
	middle.writeBlank()
	middle.writeBlank()

	// Planned features
	plannedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Bold(true)
	middle.writeLine(plannedStyle.Render("Planned features:"))

	bulletStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(bulletStyle.Render("  • Desktop notifications for @mentions"))
	middle.writeLine(bulletStyle.Render("  • Sound alerts for messages"))
	middle.writeLine(bulletStyle.Render("  • Per-channel notification muting"))
	middle.writeLine(bulletStyle.Render("  • DND mode scheduling"))
	middle.writeLine(bulletStyle.Render("  • Notification history log"))

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator (first line of bottom section)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help
	navStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(navStyle.Render("Navigation: Esc close · Tab back to sections"))

	// Fill remaining bottom section space
	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderDisplayContent renders the display settings panel
func (a *App) renderDisplayContent(width, height int) string {
	layout := calculateSettingsLayout(width, height, defaultStatusLines, 0)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Display Settings"))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Customize display and appearance settings"))

	// Feature status
	featureStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Orange)).
		Italic(true)
	top.writeLine(featureStyle.Render("Feature in development"))
	top.writeBlank()

	// Top section separator (last line of top section)
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Coming soon message (centered)
	centerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(centerStyle.Render("                         This feature is coming soon!"))
	middle.writeLine(centerStyle.Render("                      Stay tuned for future updates."))
	middle.writeBlank()
	middle.writeBlank()

	// Planned features
	plannedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Bold(true)
	middle.writeLine(plannedStyle.Render("Planned features:"))

	bulletStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(bulletStyle.Render("  • Font size adjustment"))
	middle.writeLine(bulletStyle.Render("  • Timestamp format options (12h/24h, relative/absolute)"))
	middle.writeLine(bulletStyle.Render("  • Message density (compact, normal, spacious)"))
	middle.writeLine(bulletStyle.Render("  • Avatar display preferences"))
	middle.writeLine(bulletStyle.Render("  • Color blindness modes"))
	middle.writeLine(bulletStyle.Render("  • Custom color overrides"))

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator (first line of bottom section)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help
	navStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(navStyle.Render("Navigation: Esc close · Tab back to sections"))

	// Fill remaining bottom section space
	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// saveServerOrder saves the updated server order to the config file
func (a *App) saveServerOrder() {
	if a.configMgr == nil {
		return
	}

	// Get all servers from connection manager
	servers := a.connMgr.GetAllConnections()

	// Build ServerInfo array with updated order
	serverInfos := make([]*ClientServerInfo, len(servers))
	for i, srv := range servers {
		serverInfos[i] = srv.ServerInfo
	}

	// Load current config
	config, err := a.configMgr.LoadServers()
	if err != nil {
		a.statusMessage = fmt.Sprintf("Failed to load servers config: %v", err)
		return
	}

	// Update servers list
	config.Servers = serverInfos

	// Save config
	if err := a.configMgr.SaveServers(config); err != nil {
		a.statusMessage = fmt.Sprintf("Failed to save server order: %v", err)
	}
}
