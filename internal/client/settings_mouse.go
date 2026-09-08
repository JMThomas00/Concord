package client

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"
)

// handleSettingsMouse handles mouse input over the Settings view (Ctrl+S).
// Kept in its own file, separate from mouse.go, since settings_view.go's
// ~2000 lines of category-specific logic will grow this dispatcher
// considerably as more categories get click support -- see
// "Concord - Mouse Support Plan" in the Obsidian vault for the full
// category-by-category rollout.
func (a *App) handleSettingsMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState
	if s == nil {
		return nil
	}

	// A Help & Guide scrollbar drag in progress must keep tracking Motion
	// events (and end cleanly on Release) even though every other resolver
	// below only ever acts on a Press -- see helpScrollDragging's doc
	// comment in app.go. This has to run before the Press-only gate right
	// below, or a drag's Motion/Release events would just be dropped.
	if a.helpScrollDragging {
		switch msg.Action {
		case tea.MouseActionMotion:
			if msg.Button == tea.MouseButtonLeft {
				a.updateHelpScrollFromRow(msg.Y)
			}
			return nil
		case tea.MouseActionRelease:
			a.helpScrollDragging = false
			return nil
		default:
			a.helpScrollDragging = false
		}
	}

	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	// The delete-server confirmation dialog fully replaces the category
	// layout when open (renderSettingsView returns it directly instead of
	// the normal sidebar+content render) -- check it first, same priority
	// order the renderer itself uses.
	if a.deleteConfirmServerID != nil {
		return a.handleDeleteServerConfirmMouse(msg)
	}

	// Category sidebar -- clicking a row jumps straight into that category's
	// content, equivalent to what plain Enter from the sidebar does today
	// (settings_view.go's "enter" case just flips FocusOnForm, it doesn't
	// target a specific field within the category).
	for i := range s.Categories {
		if zoneInBounds(fmt.Sprintf("settings-cat-row:%d", i), msg) {
			s.SelectedCategory = i
			s.FocusOnForm = true
			return nil
		}
	}

	switch s.SelectedCategory {
	case settingsCatTheme:
		return a.handleThemeCategoryMouse(msg)
	case settingsCatNotifications:
		return a.handleNotificationsCategoryMouse(msg)
	case settingsCatDisplay:
		return a.handleDisplayCategoryMouse(msg)
	case settingsCatAudio:
		return a.handleAudioCategoryMouse(msg)
	case settingsCatManageServers:
		return a.handleManageServersCategoryMouse(msg)
	default:
		// Help & Guide is always the last category (see the helpIdx
		// convention in renderSettingsView) and has no fixed index constant
		// -- it's whatever's left once the named categories above don't
		// match. The scrollbar column is checked first since it's a strict
		// subset of the panel's own area (a click inside it should start a
		// drag, not just focus the pane).
		if z := zone.Get("help-scrollbar"); z != nil && z.InBounds(msg) {
			a.helpScrollDragging = true
			s.FocusOnForm = true
			a.updateHelpScrollFromRow(msg.Y)
			return nil
		}
		if zoneInBounds("settings-help-panel", msg) {
			s.FocusOnForm = true
		}
	}

	return nil
}

// updateHelpScrollFromRow maps a mouse row (msg.Y, absolute screen
// coordinate) to a Help & Guide scroll offset, using the "help-scrollbar"
// zone's own recorded bounds to convert to a row relative to the track --
// the same math serves both a single click-to-jump and every Motion event
// of an in-progress drag. Deliberately computed from the raw Y delta
// rather than z.Pos/z.InBounds so a drag that strays outside the narrow
// 2-char-wide column (clamped below) still keeps tracking the cursor,
// exactly like a real scrollbar.
func (a *App) updateHelpScrollFromRow(absY int) {
	s := a.settingsState
	if s == nil {
		return
	}
	z := zone.Get("help-scrollbar")
	if z == nil {
		return
	}
	trackHeight := z.EndY - z.StartY + 1
	if trackHeight <= 0 {
		return
	}
	totalLines := len(s.HelpRenderedLines)
	maxOffset := totalLines - trackHeight
	if maxOffset <= 0 {
		s.HelpScrollOffset = 0
		return
	}

	relY := absY - z.StartY
	if relY < 0 {
		relY = 0
	}
	if relY > trackHeight-1 {
		relY = trackHeight - 1
	}

	offset := 0
	if trackHeight > 1 {
		offset = relY * maxOffset / (trackHeight - 1)
	}
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	s.HelpScrollOffset = offset
}

// handleNotificationsCategoryMouse handles clicks on the Notifications
// category: the main field list (6 fields, each a zone-marked 2-line block
// -- see writeZoneMarkedLines in settings_view.go) and the @mention/message
// sound picker sub-page. The mute-manager picker (NotifMutePickerOpen) is
// not yet wired -- it has its own internal tab bar (Servers/Channels) that
// needs its own look, deferred the same way the member-list/Hub-Browser
// gaps were in the first pass rather than guessed at.
func (a *App) handleNotificationsCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState

	if s.NotifSoundPickerOpen {
		for i := range SoundOptions {
			if zoneInBounds(fmt.Sprintf("notif-sound-row:%d", i), msg) {
				s.NotifSoundCursor = i
				return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
			}
		}
		return nil
	}

	for field := 0; field <= 5; field++ {
		if zoneInBounds(fmt.Sprintf("notif-field:%d", field), msg) {
			s.NotifFocusField = field
			return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleDeleteServerConfirmMouse handles clicks on the Yes/Cancel buttons
// of the delete-server confirmation dialog, synthesizing the same keys the
// keyboard path already uses ("y"/"Y" confirms, Esc cancels) rather than
// duplicating handleSettingsKey's delete/removal logic.
func (a *App) handleDeleteServerConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	if zoneInBounds("delete-server-confirm-yes", msg) {
		return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	}
	if zoneInBounds("delete-server-confirm-cancel", msg) {
		return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return nil
}

// handleDisplayCategoryMouse handles clicks on the Display category's 13
// fields (zone-marked 2-line blocks, "display-field:%d" -- see writeField
// inside renderDisplayContent). No extra scroll-offset math is needed: the
// zone bounds already reflect real post-scroll screen position.
func (a *App) handleDisplayCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState
	for field := 0; field <= 12; field++ {
		if zoneInBounds(fmt.Sprintf("display-field:%d", field), msg) {
			s.DisplayFocusField = field
			return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleAudioCategoryMouse handles clicks on the Audio category's 11 fields
// (zone-marked as "audio-field:%d" -- both writeField's 2-line blocks and
// writeToggle's 1-line blocks share this ID scheme) and the inline device
// picker's rows. Clicking a slider field (Input Gain/Output Volume/VAD
// Threshold) only enters slider mode, same as Enter does -- adjustment
// itself stays keyboard-only (←/→), no click-to-drag.
func (a *App) handleAudioCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState

	if s.AudioPickerOpen {
		for i := range s.AudioPickerDevices {
			if zoneInBounds(fmt.Sprintf("audio-device-row:%d", i), msg) {
				s.AudioPickerCursor = i
				return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
			}
		}
		return nil
	}

	for field := 0; field <= 10; field++ {
		if !zoneInBounds(fmt.Sprintf("audio-field:%d", field), msg) {
			continue
		}
		// Clicking a different field while a slider is active must exit
		// slider mode first, same as arrow-key navigation already does
		// (settings_view.go's up/down handlers: "exit slider before
		// moving") -- otherwise AudioSliderActive would stay true against
		// whatever field the click just moved focus to.
		if s.AudioSliderActive && s.AudioFocusField != field {
			s.AudioSliderActive = false
		}
		s.AudioFocusField = field
		return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

// handleManageServersCategoryMouse handles clicks on the Manage Servers
// category. When the Add/Edit form is open it takes over the whole content
// pane (mirroring renderManageServersContent's own render priority), same
// as the delete-confirm dialog does at the view level.
func (a *App) handleManageServersCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState
	if s.ServerFormOpen {
		return a.handleServerFormMouse(msg)
	}

	// Server list -- clicking a row selects it (same as arrow-key
	// navigation), no default activate step since selecting a server here
	// has no single obvious action the way picking a theme does.
	for i := range a.clientServers {
		if zoneInBounds(fmt.Sprintf("settings-server-row:%d", i), msg) {
			s.SelectedServer = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handleServerFormMouse handles clicks on the Add/Edit Server form's 4
// fields (Name/Address/Port/TLS toggle) and its Save/Cancel buttons.
// Clicking a text field just focuses it -- the same click-to-focus
// precedent the main chat input's "chat-input" zone already establishes --
// while clicking the TLS row toggles it directly (mirroring what Space
// does) and Save/Cancel synthesize Enter/Esc into handleSettingsKey's
// existing form-submit/cancel path rather than duplicating it.
func (a *App) handleServerFormMouse(msg tea.MouseMsg) tea.Cmd {
	if zoneInBounds("server-form-save", msg) {
		return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("server-form-cancel", msg) {
		return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	for field := 0; field <= 3; field++ {
		if !zoneInBounds(fmt.Sprintf("server-form-field:%d", field), msg) {
			continue
		}
		a.setAddServerFocus(field)
		if field == 3 {
			a.addServerUseTLS = !a.addServerUseTLS
		}
		return nil
	}
	return nil
}

// setAddServerFocus moves focus to a specific Add/Edit Server form field by
// index, applying the same blur-all-then-focus-one pattern
// cycleAddServerFocus (add_server_view.go) already uses -- that function
// only supports relative +1/-1 stepping, so a click (which names an exact
// target field) needs this direct-set variant instead.
func (a *App) setAddServerFocus(field int) {
	a.addServerFocus = field
	a.addServerName.Blur()
	a.addServerAddress.Blur()
	a.addServerPort.Blur()
	switch field {
	case 0:
		a.addServerName.Focus()
	case 1:
		a.addServerAddress.Focus()
	case 2:
		a.addServerPort.Focus()
	}
}

// handleThemeCategoryMouse handles clicks on the Theme category's theme
// list -- the template every other category's list-based content will
// copy. A click sets the selection and live-previews the theme (mirroring
// what arrow-key navigation already does), then synthesizes the same Enter
// keypress that saves and returns to the category list, reusing
// handleSettingsKey's existing save path (applyAndSaveTheme + status
// message + FocusOnForm = false) rather than duplicating it.
func (a *App) handleThemeCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.settingsState
	for i, name := range s.AvailableThemes {
		if zoneInBounds(fmt.Sprintf("theme-row:%d", i), msg) {
			s.SelectedTheme = i
			a.previewTheme(name)
			return a.handleSettingsKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}
