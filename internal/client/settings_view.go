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

// openSettings transitions the app into the Settings view with a slide-from-left animation.
func (a *App) openSettings(returnTo View) tea.Cmd {
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

	categories := []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "Help & Guide"}

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
	if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
		return nil
	}
	a.settingsAnimFrame = 0
	a.settingsAnimClosing = false
	a.settingsAnimating = true
	return settingsPanelAnimTick()
}

// handleSettingsKey processes key events when in ViewSettings
func (a *App) handleSettingsKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		a.view = ViewMain
		return nil
	}

	// Route to server form when it's open
	if s.ServerFormOpen {
		return a.handleSettingsServerFormKey(msg)
	}

	// Route server sound override sub-page
	if s.ServerSoundPageOpen {
		return a.handleServerSoundPageKey(msg)
	}

	// Route notification sub-pages
	if s.SelectedCategory == 1 && s.FocusOnForm {
		if s.NotifSoundPickerOpen {
			return a.handleNotifSoundPickerKey(msg)
		}
		if s.NotifMutePickerOpen {
			return a.handleNotifMutePickerKey(msg)
		}
	}

	switch msg.String() {
	case "esc":
		// If delete confirmation is shown, cancel it
		if a.deleteConfirmServerID != nil {
			a.deleteConfirmServerID = nil
			return nil
		}

		// Close audio slider mode if active
		if s.AudioSliderActive {
			s.AudioSliderActive = false
			return nil
		}

		// Close audio device picker if open
		if s.AudioPickerOpen {
			s.AudioPickerOpen = false
			s.AudioPickerDevices = nil
			return nil
		}

		// Cancel and return to previous view (with slide-out animation if enabled).
		// Restore original theme ONLY if currently IN the theme form (not just category selected)
		if s.SelectedCategory == 0 && s.FocusOnForm && s.OriginalTheme != "" {
			a.applyAndSaveTheme(s.OriginalTheme)
		}
		if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
			returnTo := s.PreviousView
			a.settingsState = nil
			a.view = returnTo
			return nil
		}
		a.settingsAnimClosing = true
		a.settingsAnimating = true
		return settingsPanelAnimTick()

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
			case 1: // Notifications category
				if s.NotifFocusField > 0 {
					s.NotifFocusField--
				}
			case 2: // Display category
				if s.DisplayFocusField > 0 {
					s.DisplayFocusField--
					a.updateDisplayScroll(s)
				}
			case 3: // Audio category
				if s.AudioPickerOpen {
					if s.AudioPickerCursor > 0 {
						s.AudioPickerCursor--
					}
				} else if s.AudioSliderActive {
					s.AudioSliderActive = false // exit slider before moving
				} else if s.AudioFocusField > 0 {
					s.AudioFocusField--
				}
			case 4: // Manage Servers category
				if s.SelectedServer > 0 {
					s.SelectedServer--
				}
			case len(s.Categories) - 1: // Help & Guide — scroll up
				if s.HelpScrollOffset > 0 {
					s.HelpScrollOffset--
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
			case 1: // Notifications category
				if s.NotifFocusField < 5 {
					s.NotifFocusField++
				}
			case 2: // Display category
				if s.DisplayFocusField < 12 {
					s.DisplayFocusField++
					a.updateDisplayScroll(s)
				}
			case 3: // Audio category
				if s.AudioPickerOpen {
					if s.AudioPickerCursor < len(s.AudioPickerDevices)-1 {
						s.AudioPickerCursor++
					}
				} else if s.AudioSliderActive {
					s.AudioSliderActive = false // exit slider before moving
				} else if s.AudioFocusField < 10 {
					s.AudioFocusField++
				}
			case 4: // Manage Servers category
				serverCount := len(a.connMgr.GetAllConnections())
				if s.SelectedServer < serverCount-1 {
					s.SelectedServer++
				}
			case len(s.Categories) - 1: // Help & Guide — scroll down
				s.HelpScrollOffset++
			}
		}

	case "pgup":
		if s.FocusOnForm && s.SelectedCategory == len(s.Categories)-1 {
			s.HelpScrollOffset -= 10
			if s.HelpScrollOffset < 0 {
				s.HelpScrollOffset = 0
			}
		}
		return nil

	case "pgdown":
		if s.FocusOnForm && s.SelectedCategory == len(s.Categories)-1 {
			s.HelpScrollOffset += 10
		}
		return nil

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
				s.FocusOnForm = false
			case 1: // Notifications category
				a.handleNotifFieldActivate(s)
			case 2: // Display category
				a.handleDisplayFieldActivate(s)
			case 3: // Audio category
				if s.AudioPickerOpen {
					a.handleAudioPickerSelect(s)
				} else {
					a.handleAudioFieldActivate(s)
				}
			}
		}

	case "left", "h":
		if s.FocusOnForm && s.SelectedCategory == 3 && s.AudioSliderActive {
			a.adjustAudioSlider(s, -1)
			return nil
		}

	case "right", "l":
		if s.FocusOnForm && s.SelectedCategory == 3 && s.AudioSliderActive {
			a.adjustAudioSlider(s, +1)
			return nil
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
			// Open add server form as sub-page
			a.editingServerID = nil
			a.initAddServerForm()
			a.addServerName.Focus()
			s.ServerFormOpen = true
		}

	case "e":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Edit selected server as sub-page
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
				s.ServerFormOpen = true
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

	case "space":
		if s.FocusOnForm && s.SelectedCategory == 1 {
			switch s.NotifFocusField {
			case 0: // Notification Sounds toggle
				a.notifConfig.SoundsMuted = !a.notifConfig.SoundsMuted
				a.saveNotifConfig()
			case 1: // Mentions Only toggle
				a.notifConfig.MentionsOnly = !a.notifConfig.MentionsOnly
				a.saveNotifConfig()
			case 2: // Terminal Bell on Mention toggle
				a.notifConfig.BellOnMention = !a.notifConfig.BellOnMention
				a.saveNotifConfig()
			}
		}
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleDisplayFieldActivate(s)
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

	case "s", "S":
		if s.FocusOnForm && s.SelectedCategory == 3 {
			// Open per-server sound override sub-page
			servers := a.connMgr.GetAllConnections()
			if s.SelectedServer >= 0 && s.SelectedServer < len(servers) {
				srv := servers[s.SelectedServer]
				serverID := srv.ServerInfo.ID
				s.ServerSoundServerID = &serverID
				s.ServerSoundFocus = 0
				s.ServerSoundPickerOpen = false
				s.ServerSoundPageOpen = true
			}
		}
	}

	return nil
}

// handleNotifFieldActivate is called on Enter for the Notifications category.
func (a *App) handleNotifFieldActivate(s *SettingsState) {
	switch s.NotifFocusField {
	case 0: // Notification Sounds toggle
		a.notifConfig.SoundsMuted = !a.notifConfig.SoundsMuted
		a.saveNotifConfig()
	case 1: // Mentions Only toggle
		a.notifConfig.MentionsOnly = !a.notifConfig.MentionsOnly
		a.saveNotifConfig()
	case 2: // Terminal Bell on Mention toggle
		a.notifConfig.BellOnMention = !a.notifConfig.BellOnMention
		a.saveNotifConfig()
	case 3: // @Mention sound picker
		s.NotifSoundPickerOpen = true
		s.NotifSoundTarget = 0
		s.NotifSoundCursor = FindSoundIndex(a.notifConfig.MentionSound)
	case 4: // Message sound picker
		s.NotifSoundPickerOpen = true
		s.NotifSoundTarget = 1
		s.NotifSoundCursor = FindSoundIndex(a.notifConfig.MessageSound)
	case 5: // Mute manager
		s.NotifMutePickerOpen = true
		s.NotifMuteTab = 0
		s.NotifMuteServerIdx = 0
		s.NotifMuteChanIdx = 0
	}
}

// handleNotifSoundPickerKey handles key events on the sound picker sub-page.
func (a *App) handleNotifSoundPickerKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		s.NotifSoundPickerOpen = false
	case "up", "k":
		if s.NotifSoundCursor > 0 {
			s.NotifSoundCursor--
		}
	case "down", "j":
		if s.NotifSoundCursor < len(SoundOptions)-1 {
			s.NotifSoundCursor++
		}
	case "p":
		// Preview the highlighted sound
		a.playSound(SoundOptions[s.NotifSoundCursor].Name)
	case "enter", " ":
		chosen := SoundOptions[s.NotifSoundCursor].Name
		if s.NotifSoundTarget == 0 {
			a.notifConfig.MentionSound = chosen
		} else {
			a.notifConfig.MessageSound = chosen
		}
		a.saveNotifConfig()
		s.NotifSoundPickerOpen = false
	}
	return nil
}

// handleNotifMutePickerKey handles key events on the mute picker sub-page.
func (a *App) handleNotifMutePickerKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		return nil
	}
	switch msg.String() {
	case "esc":
		s.NotifMutePickerOpen = false
	case "tab":
		s.NotifMuteTab = 1 - s.NotifMuteTab // toggle 0↔1
		s.NotifMuteServerIdx = 0
		s.NotifMuteChanIdx = 0
	case "up", "k":
		if s.NotifMuteTab == 0 {
			if s.NotifMuteServerIdx > 0 {
				s.NotifMuteServerIdx--
			}
		} else {
			if s.NotifMuteChanIdx > 0 {
				s.NotifMuteChanIdx--
			}
		}
	case "down", "j":
		if s.NotifMuteTab == 0 {
			if s.NotifMuteServerIdx < len(a.clientServers)-1 {
				s.NotifMuteServerIdx++
			}
		} else {
			// Count total channels
			total := a.notifMuteChanTotal()
			if s.NotifMuteChanIdx < total-1 {
				s.NotifMuteChanIdx++
			}
		}
	case " ", "enter":
		if s.NotifMuteTab == 0 {
			a.toggleMuteServer(s.NotifMuteServerIdx)
		} else {
			a.toggleMuteChanByIndex(s.NotifMuteChanIdx)
		}
	}
	return nil
}

// notifMuteChanTotal returns the total number of text channels across all servers.
func (a *App) notifMuteChanTotal() int {
	total := 0
	for _, srv := range a.connMgr.GetAllConnections() {
		for _, channels := range srv.Channels {
			for _, ch := range channels {
				if ch.Type == 0 { // text channel
					total++
				}
			}
		}
	}
	return total
}

// toggleMuteServer toggles the mute state for the server at the given list index.
func (a *App) toggleMuteServer(idx int) {
	servers := a.clientServers
	if idx < 0 || idx >= len(servers) {
		return
	}
	id := servers[idx].ID
	if a.mutedServers[id] {
		delete(a.mutedServers, id)
	} else {
		a.mutedServers[id] = true
	}
	a.saveMutedServers()
}

// toggleMuteChanByIndex toggles the mute state for the nth text channel across all servers.
func (a *App) toggleMuteChanByIndex(idx int) {
	i := 0
	for _, srv := range a.connMgr.GetAllConnections() {
		for _, channels := range srv.Channels {
			for _, ch := range channels {
				if ch.Type != 0 {
					continue
				}
				if i == idx {
					if a.mutedChannels[ch.ID] {
						delete(a.mutedChannels, ch.ID)
					} else {
						a.mutedChannels[ch.ID] = true
					}
					a.saveMutedChannels()
					return
				}
				i++
			}
		}
	}
}

// handleSettingsServerFormKey handles key events when the server add/edit form is open
func (a *App) handleSettingsServerFormKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "esc":
		s.ServerFormOpen = false
		a.addServerError = ""
		a.editingServerID = nil
		return nil
	case "tab":
		a.cycleAddServerFocus()
	case "shift+tab":
		a.cycleAddServerFocusReverse()
	case "space":
		if a.addServerFocus == 3 {
			a.addServerUseTLS = !a.addServerUseTLS
		}
	case "enter":
		if a.addServerFocus == 3 {
			a.addServerUseTLS = !a.addServerUseTLS
		} else {
			return a.handleAddServerSubmit()
		}
	}

	return a.updateAddServerForm(msg)
}

// renderServerFormPage renders the Add/Edit Server form as a full settings sub-page
func (a *App) renderServerFormPage(width, height int) string {
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + blank + 2 help lines = 4 lines → pageBottomExtra = 1
	layout := calculateSettingsLayout(width, height, 2, 1)

	title := "Add New Server"
	if a.editingServerID != nil {
		title = "Edit Server"
	}

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render(title))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("Tab / Shift+Tab to navigate fields · Space to toggle TLS"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	// Error message
	if a.addServerError != "" {
		middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Render("✗ " + a.addServerError))
		middle.writeBlank()
	}

	// Server Name
	middle.writeLine(labelStyle.Render("Server Name:"))
	middle.writeLine("  " + a.addServerName.View())
	middle.writeBlank()

	// Address
	middle.writeLine(labelStyle.Render("Address:"))
	middle.writeLine("  " + a.addServerAddress.View())
	middle.writeBlank()

	// Port
	middle.writeLine(labelStyle.Render("Port:"))
	middle.writeLine("  " + a.addServerPort.View())
	middle.writeBlank()

	// TLS toggle
	middle.writeLine(labelStyle.Render("Use TLS (WSS):"))
	tlsValue := "[ ] No"
	if a.addServerUseTLS {
		tlsValue = "[✓] Yes"
	}
	if a.addServerFocus == 3 {
		middle.writeLine("  " + selectedStyle.Render(tlsValue))
	} else {
		middle.writeLine("  " + normalStyle.Render(tlsValue))
	}
	middle.writeBlank()

	saveLabel := "Add Server"
	if a.editingServerID != nil {
		saveLabel = "Save Changes"
	}
	saveBtn := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Bold(true).
		Render("[Enter] " + saveLabel)
	cancelBtn := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Render("[Esc] Cancel")
	middle.writeLine(fmt.Sprintf("%s  %s", saveBtn, cancelBtn))
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(helpStyle.Render("Tab / Shift+Tab · navigate fields"))
	bottom.writeLine(helpStyle.Render("Space · toggle TLS · Enter · submit · Esc · cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
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

	helpIdx := len(s.Categories) - 1 // "Help & Guide" is always last
	for i, category := range s.Categories {
		var line string
		selected := i == s.SelectedCategory
		isHelp := i == helpIdx

		// "Help & Guide" uses yellow to visually separate it from the other entries.
		accentColor := lipgloss.Color(a.theme.Colors.Purple)
		labelColor := lipgloss.Color(a.theme.Colors.Foreground)
		if isHelp {
			accentColor = lipgloss.Color(a.theme.Colors.Yellow)
			labelColor = lipgloss.Color(a.theme.Colors.Yellow)
		}

		if selected && !s.FocusOnForm {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(accentColor).
				Bold(true).
				Width(catWidth - 2).
				Render("▶ " + category)
		} else if selected {
			line = lipgloss.NewStyle().
				Foreground(accentColor).
				Bold(true).
				Width(catWidth - 2).
				Render("▶ " + category)
		} else {
			line = lipgloss.NewStyle().
				Foreground(labelColor).
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
		if s.NotifSoundPickerOpen {
			contentBuf.WriteString(a.renderNotifSoundPickerPage(contentWidth, contentHeight))
		} else if s.NotifMutePickerOpen {
			contentBuf.WriteString(a.renderNotifMutePickerPage(contentWidth, contentHeight))
		} else {
			contentBuf.WriteString(a.renderNotificationsContent(contentWidth, contentHeight))
		}
	case 2: // Display category
		contentBuf.WriteString(a.renderDisplayContent(contentWidth, contentHeight))
	case 3: // Audio category
		contentBuf.WriteString(a.renderAudioContent(contentWidth, contentHeight))
	case 4: // Manage Servers category
		if s.ServerSoundPageOpen {
			contentBuf.WriteString(a.renderServerSoundPage(contentWidth, contentHeight))
		} else if s.ServerFormOpen {
			contentBuf.WriteString(a.renderServerFormPage(contentWidth, contentHeight))
		} else {
			contentBuf.WriteString(a.renderManageServersContent(s, contentWidth, contentHeight))
		}
	case len(s.Categories) - 1: // Help & Guide
		contentBuf.WriteString(a.renderHelpContent(contentWidth, contentHeight))
	default:
		// No-op: navigation clamps SelectedCategory to valid range; this guards
		// against future category additions that forget a matching content case.
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
					Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
					Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
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
					Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
					Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
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
	bottom.writeLine(navStyle.Render("Actions: Ctrl+N add · E edit · D delete · P ping · S sounds"))

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

// renderNotificationsContent renders the main notifications settings panel.
func (a *App) renderNotificationsContent(width, height int) string {
	s := a.settingsState
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + blank + 2 help lines = 4 lines → pageBottomExtra = 1
	layout := calculateSettingsLayout(width, height, 2, 1)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	focused := s != nil && s.FocusOnForm
	focusField := 0
	if s != nil {
		focusField = s.NotifFocusField
	}

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Notification Settings"))
	top.writeLine(dimStyle.Render("Sounds, desktop alerts, and muting"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	cfg := a.notifConfig

	// helper to render a settings row (label + value line) with focus highlight
	writeField := func(fieldIdx int, label, value string) {
		isSelected := focused && focusField == fieldIdx
		lStyle := labelStyle
		vStyle := normalStyle
		marker := "  "
		if isSelected {
			lStyle = selectedStyle
			vStyle = selectedStyle
			marker = "▶ "
		}
		middle.writeLine(lStyle.Render(marker + label))
		middle.writeLine(vStyle.Render("    " + value))
		middle.writeBlank()
	}

	// Field 0: Notification sounds toggle
	soundsVal := "[✓] Enabled"
	if cfg.SoundsMuted {
		soundsVal = "[ ] Disabled"
	}
	writeField(0, "Notification Sounds", soundsVal)

	// Field 1: Mentions only toggle
	mentionsOnlyVal := "[ ] Off  (sounds for all messages)"
	if cfg.MentionsOnly {
		mentionsOnlyVal = "[✓] On   (sounds for @mentions only)"
	}
	writeField(1, "Mentions Only", mentionsOnlyVal)

	// Field 2: Terminal bell on mention toggle
	bellVal := "[ ] Off"
	if cfg.BellOnMention {
		bellVal = "[✓] On"
	}
	writeField(2, "Terminal Bell on Mention", bellVal)

	// Field 3: @Mention sound
	mentionSound := cfg.MentionSound
	if mentionSound == "" {
		mentionSound = "None"
	}
	writeField(3, "@Mention Alert Sound", mentionSound+" ▾")

	// Field 4: Message sound
	msgSound := cfg.MessageSound
	if msgSound == "" {
		msgSound = "None"
	}
	writeField(4, "Message Alert Sound", msgSound+" ▾")

	// Divider
	middle.writeLine(dimStyle.Render(a.renderSeparator(layout.interiorWidth)))
	middle.writeBlank()

	// Field 5: Mute manager link
	muteLabel := "Manage Muted Servers & Channels"
	numMuted := len(a.mutedServers) + len(a.mutedChannels)
	muteHint := "none muted"
	if numMuted > 0 {
		muteHint = fmt.Sprintf("%d muted", numMuted)
	}
	isSelected5 := focused && focusField == 5
	marker5 := "  "
	lStyle5 := labelStyle
	if isSelected5 {
		marker5 = "▶ "
		lStyle5 = selectedStyle
	}
	middle.writeLine(lStyle5.Render(marker5 + muteLabel))
	middle.writeLine(dimStyle.Render("    " + muteHint))

	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(helpStyle.Render("↑↓ navigate · Space / Enter toggle or open · Tab back to menu"))
	bottom.writeLine(helpStyle.Render("P preview sound · Esc back"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderNotifSoundPickerPage renders the sound selection sub-page.
func (a *App) renderNotifSoundPickerPage(width, height int) string {
	s := a.settingsState
	if s == nil {
		return ""
	}
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + 1 help line → pageBottomExtra = 0
	layout := calculateSettingsLayout(width, height, 2, 0)

	title := "Select @Mention Sound"
	subtitle := "Sound played when someone @mentions you"
	if s.NotifSoundTarget == 1 {
		title = "Select Message Alert Sound"
		subtitle = "Sound played for messages in other channels"
	}

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).Render(title))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(subtitle))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	currentSound := a.notifConfig.MentionSound
	if s.NotifSoundTarget == 1 {
		currentSound = a.notifConfig.MessageSound
	}

	for i, opt := range SoundOptions {
		isCursor := i == s.NotifSoundCursor
		isCurrent := opt.Name == currentSound || (currentSound == "" && opt.Name == "None")

		prefix := "  ○ "
		if isCurrent {
			prefix = "  ● "
		}
		line := prefix + opt.Name

		if isCursor {
			middle.writeLine(lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true).
				Render("▶ " + line[2:]))
		} else {
			middle.writeLine(lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(line))
		}
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("↑↓ navigate · P preview · Enter select · Esc cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderNotifMutePickerPage renders the mute manager sub-page.
func (a *App) renderNotifMutePickerPage(width, height int) string {
	s := a.settingsState
	if s == nil {
		return ""
	}
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + blank + 2 help lines → pageBottomExtra = 1
	layout := calculateSettingsLayout(width, height, 2, 1)

	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	mutedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Orange)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	cursorStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Muted Servers & Channels"))
	top.writeLine(dimStyle.Render("Space / Enter to toggle muting · Tab to switch tab"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Tab bar
	serversTab := "  Servers  "
	channelsTab := "  Channels  "
	activeTabStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)
	inactiveTabStyle := dimStyle
	if s.NotifMuteTab == 0 {
		middle.writeLine(activeTabStyle.Render(serversTab) + "  " + inactiveTabStyle.Render(channelsTab))
	} else {
		middle.writeLine(inactiveTabStyle.Render(serversTab) + "  " + activeTabStyle.Render(channelsTab))
	}
	middle.writeBlank()

	if s.NotifMuteTab == 0 {
		// Servers list
		if len(a.clientServers) == 0 {
			middle.writeLine(dimStyle.Render("  No servers configured"))
		}
		for i, srv := range a.clientServers {
			isMuted := a.mutedServers[srv.ID]
			isCursor := i == s.NotifMuteServerIdx
			dot := "○"
			dStyle := normalStyle
			if isMuted {
				dot = "●"
				dStyle = mutedStyle
			}
			label := fmt.Sprintf("  %s  %s  %s:%d", dot, srv.Name, srv.Address, srv.Port)
			if isMuted {
				label += "  (muted)"
			}
			if isCursor {
				middle.writeLine(cursorStyle.Render("▶" + label[1:]))
			} else {
				middle.writeLine(dStyle.Render(label))
			}
		}
	} else {
		// Channels list — flat list across all servers
		idx := 0
		servers := a.connMgr.GetAllConnections()
		if len(servers) == 0 {
			middle.writeLine(dimStyle.Render("  No connected servers"))
		}
		for _, srv := range servers {
			srvName := ""
			if srv.ServerInfo != nil {
				srvName = srv.ServerInfo.Name
			}
			for _, channels := range srv.Channels {
				for _, ch := range channels {
					if ch.Type != 0 {
						continue
					}
					isMuted := a.mutedChannels[ch.ID]
					isCursor := idx == s.NotifMuteChanIdx
					dot := "○"
					dStyle := normalStyle
					if isMuted {
						dot = "●"
						dStyle = mutedStyle
					}
					label := fmt.Sprintf("  %s  #%s", dot, ch.Name)
					if srvName != "" {
						label += dimStyle.Render(fmt.Sprintf("  (%s)", srvName))
					}
					if isMuted {
						label += mutedStyle.Render("  muted")
					}
					if isCursor {
						middle.writeLine(cursorStyle.Render("▶ " + dot + "  #" + ch.Name))
					} else {
						middle.writeLine(dStyle.Render(label))
					}
					idx++
				}
			}
		}
		if idx == 0 {
			middle.writeLine(dimStyle.Render("  No text channels found"))
		}
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(helpStyle.Render("↑↓ navigate · Space / Enter toggle mute"))
	bottom.writeLine(helpStyle.Render("Tab switch tab · Esc close"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderDisplayContent renders the display settings panel.
func (a *App) renderDisplayContent(width, height int) string {
	s := a.settingsState
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + blank + 1 help line → pageBottomExtra = 0
	layout := calculateSettingsLayout(width, height, 2, 0)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	focused := s != nil && s.FocusOnForm
	focusField := 0
	if s != nil {
		focusField = s.DisplayFocusField
	}

	cfg := DisplayConfig{}
	showMembers := true
	if a.uiConfig != nil {
		cfg = a.uiConfig.Display
		showMembers = a.uiConfig.ShowMembersList
	}

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Display Settings"))
	top.writeLine(dimStyle.Render("Timestamps, message layout, and appearance"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	// Build all middle lines into a slice first so we can scroll-window them.
	var allMiddleLines []string
	addLine := func(s string) { allMiddleLines = append(allMiddleLines, s) }
	addBlank := func() { allMiddleLines = append(allMiddleLines, "") }

	writeField := func(fieldIdx int, label, value string) {
		isSelected := focused && focusField == fieldIdx
		lStyle := labelStyle
		vStyle := normalStyle
		marker := "  "
		if isSelected {
			lStyle = selectedStyle
			vStyle = selectedStyle
			marker = "▶ "
		}
		addLine(lStyle.Render(marker + label))
		addLine(vStyle.Render("    " + value))
		addBlank()
	}

	// Field 0: Timestamp Format
	tsFormat := cfg.TimestampFormat
	if tsFormat == "" {
		tsFormat = "24h"
	}
	tsFormatVal := "24h  (01/02/06 15:04)"
	if tsFormat == "12h" {
		tsFormatVal = "12h  (01/02/06 3:04 PM)"
	}
	writeField(0, "Timestamp Format", tsFormatVal+" ◀▶")

	// Field 1: Timestamp Style
	tsStyle := cfg.TimestampStyle
	if tsStyle == "" {
		tsStyle = "absolute"
	}
	tsStyleVal := "Absolute  (01/02/06 15:04)"
	if tsStyle == "relative" {
		tsStyleVal = "Relative  (Today at 15:04)"
	}
	writeField(1, "Timestamp Style", tsStyleVal+" ◀▶")

	// Field 2: Message Density
	density := cfg.MessageDensity
	if density == "" {
		density = "normal"
	}
	densityVal := map[string]string{
		"compact":  "Compact   (no blank lines between messages)",
		"normal":   "Normal    (one blank line between messages)",
		"spacious": "Spacious  (extra space between sender groups)",
	}[density]
	writeField(2, "Message Density", densityVal+" ◀▶")

	// Field 3: Show Avatars
	avatarVal := "[ ] Off"
	if cfg.ShowAvatars {
		avatarVal = "[✓] On   (colored circle before username)"
	}
	writeField(3, "Show Avatars", avatarVal)

	// Field 4: Date Separators
	dateSepVal := "[ ] Off"
	if cfg.ShowDateSeps {
		dateSepVal = "[✓] On   (──── Today ──── between days)"
	}
	writeField(4, "Date Separators", dateSepVal)

	// Field 5: Message Grouping Gap
	gapMins := cfg.GroupingGapMins
	if gapMins == 0 {
		gapMins = 5
	}
	gapVal := fmt.Sprintf("%d min  (group consecutive messages from same sender)", gapMins)
	writeField(5, "Message Grouping Gap", gapVal+" ◀▶")

	// Divider
	addLine(dimStyle.Render(a.renderSeparator(layout.interiorWidth)))
	addBlank()

	// Field 6: Show Members Panel
	membersVal := "[ ] Hidden"
	if showMembers {
		membersVal = "[✓] Visible"
	}
	writeField(6, "Show Members Panel", membersVal)

	// Field 7: Show Server List
	showServerList := true
	if a.uiConfig != nil {
		showServerList = !a.uiConfig.Display.ServerListCollapsed
	}
	serverListVal := "[ ] Collapsed"
	if showServerList {
		serverListVal = "[✓] Expanded"
	}
	writeField(7, "Server List Panel", serverListVal)

	// Field 8: Members Panel collapsed
	showMembersExpanded := true
	if a.uiConfig != nil {
		showMembersExpanded = !a.uiConfig.Display.MembersListCollapsed
	}
	membersExpandedVal := "[ ] Collapsed"
	if showMembersExpanded {
		membersExpandedVal = "[✓] Expanded"
	}
	writeField(8, "Members Panel", membersExpandedVal)

	// Divider — Members Panel section
	addLine(dimStyle.Render(a.renderSeparator(layout.interiorWidth)))
	addLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("  Members Panel"))
	addBlank()

	// Field 9: Voice level bar (VU meter)
	vuVal := "[✓] On   (↑[████░░] input/output level bar)"
	if a.uiConfig != nil && a.uiConfig.Display.MembersHideVUMeter {
		vuVal = "[ ] Off"
	}
	writeField(9, "Voice Level Bar", vuVal)

	// Field 10: Connection quality
	qualVal := "[✓] On   (◆◆◆◇ connection quality)"
	if a.uiConfig != nil && a.uiConfig.Display.MembersHideQuality {
		qualVal = "[ ] Off"
	}
	writeField(10, "Connection Quality", qualVal)

	// Divider — Animations section
	addLine(dimStyle.Render(a.renderSeparator(layout.interiorWidth)))
	addLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("  Animations"))
	addBlank()

	// Field 11: Panel slide animations
	panelAnimVal := "[✓] On   (settings and server panels slide in/out)"
	if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
		panelAnimVal = "[ ] Off"
	}
	writeField(11, "Panel Animations", panelAnimVal)

	// Field 12: Typing indicator animation style
	typingAnim := cfg.TypingAnimation
	if typingAnim == "" {
		typingAnim = "braille"
	}
	typingAnimPreviews := map[string]string{
		"braille":   "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏",
		"dot":       "⣾⣽⣻⢿⡿⣟⣯⣷",
		"line":      "|/-\\",
		"pulse":     "█▓▒░",
		"points":    "∙∙∙ ●∙∙ ∙●∙ ∙∙●",
		"meter":     "▱▱▱ ▰▱▱ ▰▰▱ ▰▰▰",
		"hamburger": "☱☲☴☲",
		"ellipsis":  ". .. ...",
	}
	typingAnimVal := fmt.Sprintf("%-10s  %s  ◀▶", typingAnim, typingAnimPreviews[typingAnim])
	writeField(12, "Typing Animation", typingAnimVal)

	// Apply scroll window: clip allMiddleLines to layout.middleLines starting at DisplayScrollOffset.
	offset := 0
	if s != nil {
		offset = s.DisplayScrollOffset
	}
	total := len(allMiddleLines)
	maxOffset := total - layout.middleLines
	if maxOffset < 0 {
		maxOffset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + layout.middleLines
	if end > total {
		end = total
	}
	window := allMiddleLines[offset:end]

	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	for _, line := range window {
		middle.writeLine(line)
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(helpStyle.Render("↑↓ navigate · Space / Enter toggle or cycle · Tab back to menu · Esc close"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// displayFieldLineStarts maps each Display field index to its first line in the middle section.
// Layout: fields 0-5 (3 lines each), divider+blank (2), fields 6-8 (3 lines each),
// divider+header+blank (3), fields 9-10 (3 lines each),
// divider+header+blank (3), fields 11-12 (3 lines each).
var displayFieldLineStarts = []int{0, 3, 6, 9, 12, 15, 20, 23, 26, 32, 35, 41, 44}

// updateDisplayScroll adjusts DisplayScrollOffset so the focused field is visible.
func (a *App) updateDisplayScroll(s *SettingsState) {
	contentHeight := a.height - 2
	layout := calculateSettingsLayout(100, contentHeight, 2, 0)
	if s.DisplayFocusField < 0 || s.DisplayFocusField >= len(displayFieldLineStarts) {
		return
	}
	fieldStart := displayFieldLineStarts[s.DisplayFocusField]
	fieldEnd := fieldStart + 3
	if fieldStart < s.DisplayScrollOffset {
		s.DisplayScrollOffset = fieldStart
	}
	if fieldEnd > s.DisplayScrollOffset+layout.middleLines {
		s.DisplayScrollOffset = fieldEnd - layout.middleLines
	}
	if s.DisplayScrollOffset < 0 {
		s.DisplayScrollOffset = 0
	}
}

// handleDisplayFieldActivate is called on Space/Enter for the Display category.
func (a *App) handleDisplayFieldActivate(s *SettingsState) {
	if a.uiConfig == nil {
		return
	}
	cfg := &a.uiConfig.Display
	switch s.DisplayFocusField {
	case 0: // Timestamp Format: cycle 24h → 12h → 24h
		if cfg.TimestampFormat == "" || cfg.TimestampFormat == "24h" {
			cfg.TimestampFormat = "12h"
		} else {
			cfg.TimestampFormat = "24h"
		}
	case 1: // Timestamp Style: cycle absolute → relative → absolute
		if cfg.TimestampStyle == "" || cfg.TimestampStyle == "absolute" {
			cfg.TimestampStyle = "relative"
		} else {
			cfg.TimestampStyle = "absolute"
		}
	case 2: // Message Density: cycle compact → normal → spacious → compact
		switch cfg.MessageDensity {
		case "compact":
			cfg.MessageDensity = "normal"
		case "spacious":
			cfg.MessageDensity = "compact"
		default: // "normal" or ""
			cfg.MessageDensity = "spacious"
		}
	case 3: // Show Avatars toggle
		cfg.ShowAvatars = !cfg.ShowAvatars
	case 4: // Date Separators toggle
		cfg.ShowDateSeps = !cfg.ShowDateSeps
	case 5: // Message Grouping Gap: cycle through presets
		presets := []int{1, 2, 5, 10, 15, 30}
		cur := cfg.GroupingGapMins
		if cur == 0 {
			cur = 5
		}
		next := presets[0]
		for i, v := range presets {
			if v == cur && i+1 < len(presets) {
				next = presets[i+1]
				break
			}
		}
		cfg.GroupingGapMins = next
	case 6: // Show Members Panel toggle
		a.uiConfig.ShowMembersList = !a.uiConfig.ShowMembersList
	case 7: // Show Server List toggle (instant via settings; animated via [ key)
		cfg.ServerListCollapsed = !cfg.ServerListCollapsed
		if cfg.ServerListCollapsed {
			a.serverListAnimWidth = 10
		} else {
			a.serverListAnimWidth = 22
		}
	case 8: // Collapse Members Panel toggle (instant via settings; animated via ] key)
		cfg.MembersListCollapsed = !cfg.MembersListCollapsed
		if cfg.MembersListCollapsed {
			a.membersAnimWidth = 10
		} else {
			a.membersAnimWidth = 30
		}
	case 9: // Voice Level Bar toggle
		cfg.MembersHideVUMeter = !cfg.MembersHideVUMeter
	case 10: // Connection Quality toggle
		cfg.MembersHideQuality = !cfg.MembersHideQuality
	case 11: // Panel Animations toggle
		cfg.DisablePanelAnimations = !cfg.DisablePanelAnimations
	case 12: // Typing Animation: cycle through styles
		curr := cfg.TypingAnimation
		if curr == "" {
			curr = "braille"
		}
		idx := 0
		for i, n := range typingAnimNames {
			if n == curr {
				idx = i
				break
			}
		}
		cfg.TypingAnimation = typingAnimNames[(idx+1)%len(typingAnimNames)]
	}
	a.saveDisplayConfig()
	a.updateChatContent()
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

// handleServerSoundPageKey handles key events on the per-server sound override sub-page.
func (a *App) handleServerSoundPageKey(msg tea.KeyMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		return nil
	}

	// Route to sound picker if open
	if s.ServerSoundPickerOpen {
		switch msg.String() {
		case "esc":
			s.ServerSoundPickerOpen = false
		case "up", "k":
			if s.ServerSoundPickerCursor > 0 {
				s.ServerSoundPickerCursor--
			}
		case "down", "j":
			if s.ServerSoundPickerCursor < len(SoundOptions)-1 {
				s.ServerSoundPickerCursor++
			}
		case "p":
			a.playSound(SoundOptions[s.ServerSoundPickerCursor].Name)
		case "enter", " ":
			chosen := SoundOptions[s.ServerSoundPickerCursor].Name
			a.applyServerSoundOverrideField(s, s.ServerSoundPickerTarget, chosen)
			s.ServerSoundPickerOpen = false
		}
		return nil
	}

	switch msg.String() {
	case "esc":
		s.ServerSoundPageOpen = false
		s.ServerSoundServerID = nil
	case "up", "k":
		if s.ServerSoundFocus > 0 {
			s.ServerSoundFocus--
		}
	case "down", "j":
		if s.ServerSoundFocus < 3 {
			s.ServerSoundFocus++
		}
	case "space", "enter":
		switch s.ServerSoundFocus {
		case 0: // Muted toggle
			a.applyServerSoundOverrideField(s, 0, "")
		case 1: // Mentions only toggle
			a.applyServerSoundOverrideField(s, 1, "")
		case 2: // Mention sound picker
			ov := a.getServerSoundOverride(s.ServerSoundServerID)
			s.ServerSoundPickerTarget = 2
			s.ServerSoundPickerCursor = FindSoundIndex(ov.MentionSound)
			s.ServerSoundPickerOpen = true
		case 3: // Message sound picker
			ov := a.getServerSoundOverride(s.ServerSoundServerID)
			s.ServerSoundPickerTarget = 3
			s.ServerSoundPickerCursor = FindSoundIndex(ov.MessageSound)
			s.ServerSoundPickerOpen = true
		}
	case "r", "R":
		// Reset: clear override entirely (revert to global defaults)
		a.setServerSoundOverride(s.ServerSoundServerID, nil)
	}
	return nil
}

// getServerSoundOverride returns the current override for a server, or a zero-value struct if none.
func (a *App) getServerSoundOverride(serverID *uuid.UUID) ServerSoundOverride {
	if serverID == nil {
		return ServerSoundOverride{}
	}
	for _, srv := range a.clientServers {
		if srv.ID == *serverID && srv.SoundOverride != nil {
			return *srv.SoundOverride
		}
	}
	return ServerSoundOverride{}
}

// setServerSoundOverride writes (or clears) the sound override for a server and persists it.
func (a *App) setServerSoundOverride(serverID *uuid.UUID, ov *ServerSoundOverride) {
	if serverID == nil {
		return
	}
	for _, srv := range a.clientServers {
		if srv.ID == *serverID {
			srv.SoundOverride = ov
			if err := a.configMgr.UpdateServer(srv); err != nil {
				a.statusMessage = fmt.Sprintf("Failed to save server sound settings: %v", err)
			}
			return
		}
	}
}

// applyServerSoundOverrideField modifies one field of the server's sound override.
// field: 0=muted toggle, 1=mentionsOnly toggle, 2=mentionSound string, 3=messageSound string.
func (a *App) applyServerSoundOverrideField(s *SettingsState, field int, value string) {
	if s.ServerSoundServerID == nil {
		return
	}
	ov := a.getServerSoundOverride(s.ServerSoundServerID)
	switch field {
	case 0:
		ov.SoundsMuted = !ov.SoundsMuted
	case 1:
		ov.MentionsOnly = !ov.MentionsOnly
	case 2:
		ov.MentionSound = value
	case 3:
		ov.MessageSound = value
	}
	a.setServerSoundOverride(s.ServerSoundServerID, &ov)
}

// renderServerSoundPage renders the per-server sound override settings sub-page.
func (a *App) renderServerSoundPage(width, height int) string {
	s := a.settingsState
	if s == nil {
		return ""
	}

	// Find the server name
	serverName := "Server"
	for _, srv := range a.clientServers {
		if s.ServerSoundServerID != nil && srv.ID == *s.ServerSoundServerID {
			serverName = srv.Name
			break
		}
	}

	ov := a.getServerSoundOverride(s.ServerSoundServerID)

	// If sound picker sub-page is open, render that instead
	if s.ServerSoundPickerOpen {
		layout := calculateSettingsLayout(width, height, 2, 0)
		title := "@Mention Sound — " + serverName
		subtitle := "Overrides global mention sound for this server"
		if s.ServerSoundPickerTarget == 3 {
			title = "Message Sound — " + serverName
			subtitle = "Overrides global message sound for this server"
		}
		top := newSectionBuilder(layout.topLines, layout.interiorWidth)
		top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).Render(title))
		top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render(subtitle))
		top.writeBlank()
		top.writeLine(a.renderSeparator(layout.interiorWidth))

		middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
		currentSound := ov.MentionSound
		if s.ServerSoundPickerTarget == 3 {
			currentSound = ov.MessageSound
		}
		for i, opt := range SoundOptions {
			isCursor := i == s.ServerSoundPickerCursor
			isCurrent := opt.Name == currentSound || (currentSound == "" && opt.Name == "None")
			marker := "  "
			style := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			if isCursor {
				marker = "▶ "
				style = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
					Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)
			} else if isCurrent {
				style = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green))
			}
			suffix := ""
			if isCurrent {
				suffix = " ✓"
			}
			middle.writeLine(style.Render(marker + opt.Name + suffix))
		}
		middle.pad()

		bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
		helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		bottom.writeLine(a.renderSeparator(layout.interiorWidth))
		bottom.writeLine(helpStyle.Render("↑↓ navigate · P preview · Enter select · Esc back"))
		bottom.pad()

		content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
		return lipgloss.NewStyle().Width(width).Height(height).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
			Padding(0, 1).Render(content)
	}

	// Main server sound page
	layout := calculateSettingsLayout(width, height, 2, 1)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	focused := s.FocusOnForm

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Sound Settings — " + serverName))
	top.writeLine(dimStyle.Render("Overrides global notification sounds for this server"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	renderField := func(fieldIdx int, label, value string) {
		isSelected := focused && s.ServerSoundFocus == fieldIdx
		lStyle := labelStyle
		vStyle := normalStyle
		marker := "  "
		if isSelected {
			lStyle = selectedStyle
			vStyle = selectedStyle
			marker = "▶ "
		}
		middle.writeLine(lStyle.Render(marker + label))
		middle.writeLine(vStyle.Render("    " + value))
		middle.writeBlank()
	}

	mutedVal := "[✓] Enabled"
	if ov.SoundsMuted {
		mutedVal = "[ ] Muted (all sounds silenced for this server)"
	}
	renderField(0, "Notification Sounds", mutedVal)

	mentionsOnlyVal := "[ ] Off  (sounds for all messages)"
	if ov.MentionsOnly {
		mentionsOnlyVal = "[✓] On   (sounds for @mentions only)"
	}
	renderField(1, "Mentions Only", mentionsOnlyVal)

	mentionSnd := ov.MentionSound
	if mentionSnd == "" {
		mentionSnd = "Global default (" + a.notifConfig.MentionSound + ")"
		if a.notifConfig.MentionSound == "" {
			mentionSnd = "Global default (None)"
		}
	}
	renderField(2, "@Mention Alert Sound", mentionSnd+" ▾")

	msgSnd := ov.MessageSound
	if msgSnd == "" {
		msgSnd = "Global default (" + a.notifConfig.MessageSound + ")"
		if a.notifConfig.MessageSound == "" {
			msgSnd = "Global default (None)"
		}
	}
	renderField(3, "Message Alert Sound", msgSnd+" ▾")

	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(helpStyle.Render("↑↓ navigate · Space / Enter toggle or open · R reset to global defaults · Esc back"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().Width(width).Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}
