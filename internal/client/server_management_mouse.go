package client

import (
	"fmt"
	"sort"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
)

// roleAssignAvailableRoles returns the active server's roles in the exact
// order renderMembersRoleAssignPage/handleRoleAssignKey both already sort
// them in (highest position first) -- factored out here so the mouse
// resolver doesn't re-duplicate that sort a third time.
func (a *App) roleAssignAvailableRoles() []*models.Role {
	var availableRoles []*models.Role
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			availableRoles = roles
		}
		a.activeConn.mu.RUnlock()
	}
	sort.Slice(availableRoles, func(i, j int) bool {
		return availableRoles[i].Position > availableRoles[j].Position
	})
	return availableRoles
}

// filterPanelOptionCount returns the total number of flat focus-index slots
// in the Members category's filter panel (1 "All" + non-default roles + 3
// status options + 4 sort options), matching the exact count
// handleFilterPanelKey computes for its own up/down bounds-check.
func (a *App) filterPanelOptionCount() int {
	roleCount := 1
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			for _, role := range roles {
				if !role.IsDefault {
					roleCount++
				}
			}
		}
		a.activeConn.mu.RUnlock()
	}
	return roleCount + 3 + 4
}

// handleServerManagementMouse handles mouse input over the Server
// Management view (Ctrl+B, admin-only) -- Area 4 of "Concord - Mouse
// Support Plan" in the Obsidian vault. Mirrors handleSettingsMouse's shape:
// resolve which row/field was clicked, set whatever cursor/focus field the
// keyboard switch in server_management_view.go already reads, then reuse
// the existing handler via a synthesized tea.KeyMsg rather than
// reimplementing any of its branches. Dispatch priority matches
// handleServerManagementKey's own modal-first ordering.
func (a *App) handleServerManagementMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}

	switch {
	case s.ChannelFormOpen:
		return a.handleChannelFormMouse(msg)
	case s.RoleFormOpen:
		return a.handleRoleFormMouse(msg)
	case s.PermissionsEditorOpen:
		return a.handlePermissionsEditorMouse(msg)
	case s.OverwriteEditorOpen:
		return a.handleOverwriteEditorMouse(msg)
	case s.OverwriteTargetPicker:
		return a.handleOverwriteTargetPickerMouse(msg)
	case s.DeleteConfirmOpen:
		return a.handleSrvMgmtDeleteConfirmMouse(msg)
	case s.MoveDialogOpen:
		return a.handleMoveDialogMouse(msg)
	case s.RoleAssignOpen:
		return a.handleRoleAssignMouse(msg)
	case s.KickConfirmOpen:
		return a.handleKickConfirmMouse(msg)
	case s.BanConfirmOpen:
		return a.handleBanConfirmMouse(msg)
	case s.UnmuteConfirmOpen:
		return a.handleUnmuteConfirmMouse(msg)
	case s.MuteDurationOpen:
		return a.handleMuteDurationMouse(msg)
	case s.FilterPanelOpen:
		return a.handleFilterPanelMouse(msg)
	case s.SearchInputOpen:
		// Already the sole focus target when open (opened via keyboard 's'),
		// no other field competes for it -- nothing for a click to do.
		return nil
	case s.RemoveExemptPickerOpen:
		return a.handleRemoveExemptMouse(msg)
	case s.OverrideChannelPickerOpen:
		return a.handleChannelPickerMouse(msg)
	case s.RetentionFormState != nil:
		return a.handleRetentionFormMouse(msg)
	case s.PruneConfirmOpen:
		return a.handlePruneConfirmMouse(msg)
	case s.PluginConfigState != nil:
		// Plugins category's config sub-page -- explicitly deferred, never
		// investigated (see "Concord - Mouse Support Plan", Area 4.6).
		return nil
	}

	// Category sidebar -- clicking a row jumps straight into that category.
	for i := range s.Categories {
		if zoneInBounds(fmt.Sprintf("srvmgmt-cat-row:%d", i), msg) {
			s.SelectedCategory = i
			s.FocusOnForm = true
			return nil
		}
	}

	switch s.SelectedCategory {
	case 0:
		return a.handleChannelsCategoryMouse(msg)
	case 1:
		return a.handleRolesCategoryMouse(msg)
	case 2:
		return a.handleMembersCategoryMouse(msg)
	case 3:
		return a.handleMessagesCategoryMouse(msg)
	case 4:
		return a.handlePluginsCategoryMouse(msg)
	}
	return nil
}

// handleChannelsCategoryMouse handles clicks on the Channels category's
// hierarchical channel list -- structurally identical to the main channel
// list already shipped in the first pass, direct port of the same pattern.
func (a *App) handleChannelsCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.ChannelList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-channel-row:%d", i), msg) {
			s.SelectedChannel = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handleRolesCategoryMouse handles clicks on the Roles category's list.
func (a *App) handleRolesCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.RoleList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-role-row:%d", i), msg) {
			s.SelectedRole = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handleMembersCategoryMouse handles clicks on the Members category's
// table. This is a plain single-line-per-member list (confirmed via direct
// read of renderMembersCategory, not the multi-line-block shape the main
// panel's member rows have), so it needs the same simple select-row
// treatment as Channels/Roles -- the five moderation sub-UIs it opens
// (role-assign, kick/ban/mute confirm, filter, search) are each wired
// separately since they're independent keyboard-triggered overlays, not
// reachable through this row click itself.
func (a *App) handleMembersCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.MemberList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-member-row:%d", i), msg) {
			s.SelectedMember = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handleMessagesCategoryMouse handles clicks on the Messages category's
// on-page exempt-channel list (the retention form/pickers/prune-confirm
// sub-pages are wired separately, matching how they fully replace this
// category's content the same way ChannelFormOpen does for Channels).
func (a *App) handleMessagesCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.ChannelOverrides {
		if zoneInBounds(fmt.Sprintf("srvmgmt-override-row:%d", i), msg) {
			s.SelectedOverride = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handlePluginsCategoryMouse handles clicks on the Plugins category's list.
// Only the row-select behavior every sibling category shares is wired here
// -- opening a plugin's config page (Enter) and toggling it enabled ('T')
// stay keyboard-only, since the config page itself is explicitly deferred
// pending its own investigation (see the Plan's Area 4.6).
func (a *App) handlePluginsCategoryMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.PluginList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-plugin-row:%d", i), msg) {
			s.SelectedPlugin = i
			s.FocusOnForm = true
			return nil
		}
	}
	return nil
}

// handlePermissionsEditorMouse handles clicks on a role's permission list --
// a click sets the cursor then synthesizes Space, reusing
// handlePermissionsEditorKey's existing toggle branch unmodified.
func (a *App) handlePermissionsEditorMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	permList := getPermissionList()
	for i := range permList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-perm-row:%d", i), msg) {
			s.PermSelectedIndex = i
			return a.handlePermissionsEditorKey(tea.KeyMsg{Type: tea.KeySpace})
		}
	}
	return nil
}

// handleOverwriteTargetPickerMouse handles clicks on step 1 of the channel
// permission-overwrite flow (pick a role or member) -- a click selects and
// synthesizes Enter, reusing the existing "open the editor for this target"
// path.
func (a *App) handleOverwriteTargetPickerMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	targets := a.overwriteTargets()
	for i := range targets {
		if zoneInBounds(fmt.Sprintf("srvmgmt-overwrite-target-row:%d", i), msg) {
			s.OverwriteTargetIndex = i
			return a.handleOverwriteTargetPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleOverwriteEditorMouse handles clicks on step 2's Inherit/Allow/Deny
// list -- a click sets the cursor and synthesizes Space, cycling that row's
// state exactly like pressing Space would.
func (a *App) handleOverwriteEditorMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	permList := getOverwriteablePermissionList()
	for i := range permList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-overwrite-editor-row:%d", i), msg) {
			s.OverwriteSelectedIndex = i
			return a.handleOverwriteEditorKey(tea.KeyMsg{Type: tea.KeySpace})
		}
	}
	return nil
}

// handleRoleAssignMouse handles clicks on the Members category's role
// assignment checklist -- a click on a togglable (non-@everyone) row
// synthesizes Space, reusing the existing per-role toggle unmodified.
func (a *App) handleRoleAssignMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	availableRoles := a.roleAssignAvailableRoles()
	for i, role := range availableRoles {
		if !zoneInBounds(fmt.Sprintf("srvmgmt-roleassign-row:%d", i), msg) {
			continue
		}
		s.RoleAssignFocus = i
		if role.IsDefault {
			return nil
		}
		return a.handleRoleAssignKey(tea.KeyMsg{Type: tea.KeySpace})
	}
	return nil
}

// handleFilterPanelMouse handles clicks on the Members category's filter
// panel -- every option (role/status/sort) shares one flat focus index, so
// a click sets that index and synthesizes Enter, reusing the existing
// apply-on-select branch unmodified.
func (a *App) handleFilterPanelMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	totalOptions := a.filterPanelOptionCount()
	for i := 0; i < totalOptions; i++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-filter-opt:%d", i), msg) {
			s.FilterPanelFocus = i
			return a.handleFilterPanelKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleKickConfirmMouse, handleBanConfirmMouse, handleUnmuteConfirmMouse,
// and handleSrvMgmtDeleteConfirmMouse handle clicks on the Yes/Cancel
// buttons of their respective confirm dialogs -- all four share the same
// Tab-cycled-button-then-Enter shape, so each click sets the focused button
// index and synthesizes Enter, reusing the existing confirm/cancel branch.
func (a *App) handleKickConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	if zoneInBounds("kick-confirm-yes", msg) {
		s.KickConfirmFocusedBtn = 0
		return a.handleKickConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("kick-confirm-cancel", msg) {
		s.KickConfirmFocusedBtn = 1
		return a.handleKickConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

func (a *App) handleBanConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	if zoneInBounds("ban-confirm-yes", msg) {
		s.BanConfirmFocusedBtn = 0
		return a.handleBanConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("ban-confirm-cancel", msg) {
		s.BanConfirmFocusedBtn = 1
		return a.handleBanConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

func (a *App) handleUnmuteConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	if zoneInBounds("unmute-confirm-yes", msg) {
		s.UnmuteConfirmFocusedBtn = 0
		return a.handleUnmuteConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("unmute-confirm-cancel", msg) {
		s.UnmuteConfirmFocusedBtn = 1
		return a.handleUnmuteConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

func (a *App) handleSrvMgmtDeleteConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	if zoneInBounds("srvmgmt-delete-confirm-yes", msg) {
		s.DeleteConfirmFocusedButton = 0
		return a.handleDeleteConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-delete-confirm-cancel", msg) {
		s.DeleteConfirmFocusedButton = 1
		return a.handleDeleteConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	return nil
}

// handleMuteDurationMouse handles clicks on the mute-duration page. Unlike
// every other confirm dialog in this file, Enter here is a single global
// "apply the mute now" action with no dedicated Yes/Confirm button in the
// render -- so a click only moves focus (mirroring what arrow-key
// navigation does), never auto-submits a mute. The custom-duration and
// reason fields (rows 4-5) are click-to-focus-only, same as any other text
// field.
func (a *App) handleMuteDurationMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := 0; i <= 5; i++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-mute-duration-row:%d", i), msg) {
			s.MuteDurationFocus = i
			return nil
		}
	}
	return nil
}

// handleRemoveExemptMouse and handleChannelPickerMouse handle clicks on the
// Messages category's two single-purpose picker pages -- each row is that
// page's entire reason for existing (pick a channel to exempt, or pick one
// to remove the exemption from), so a click both selects and synthesizes
// Enter to act immediately, mirroring the Theme category's click-selects-
// and-saves template.
func (a *App) handleRemoveExemptMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.ChannelOverrides {
		if zoneInBounds(fmt.Sprintf("srvmgmt-remove-exempt-row:%d", i), msg) {
			s.RemoveExemptSelected = i
			return a.handleRemoveExemptKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

func (a *App) handleChannelPickerMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	for i := range s.OverrideChannelList {
		if zoneInBounds(fmt.Sprintf("srvmgmt-exempt-channel-row:%d", i), msg) {
			s.OverrideChannelSelected = i
			return a.handleChannelPickerKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleRetentionFormMouse handles clicks on the server default retention
// policy form -- the 3 numeric fields are click-to-focus-only (typing
// happens via the keyboard path, same as any other text field), while
// Save/Cancel synthesize the same "s"/Esc keys their labels advertise.
func (a *App) handleRetentionFormMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	form := s.RetentionFormState
	if zoneInBounds("srvmgmt-retention-save", msg) {
		return a.handleRetentionFormKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("s")})
	}
	if zoneInBounds("srvmgmt-retention-cancel", msg) {
		return a.handleRetentionFormKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	for field := 0; field <= 2; field++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-retention-field:%d", field), msg) {
			form.FocusField = field
			return nil
		}
	}
	return nil
}

// handlePruneConfirmMouse handles clicks on the manual-prune confirmation
// page's Yes/Cancel buttons -- both synthesize the exact key their own
// label advertises.
func (a *App) handlePruneConfirmMouse(msg tea.MouseMsg) tea.Cmd {
	if zoneInBounds("srvmgmt-prune-confirm-yes", msg) {
		return a.handlePruneConfirmKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-prune-confirm-cancel", msg) {
		return a.handlePruneConfirmKey(tea.KeyMsg{Type: tea.KeyEsc})
	}
	return nil
}

// handleMoveDialogMouse handles clicks on the move-channel destination
// list -- a click selects and synthesizes Enter, same single-purpose-list
// template as the two Messages pickers above.
func (a *App) handleMoveDialogMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	state := s.MoveDialogState
	if state == nil {
		return nil
	}
	for i := 0; i <= len(state.CategoryList); i++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-move-row:%d", i), msg) {
			state.SelectedIndex = i
			return a.handleMoveDialogKey(tea.KeyMsg{Type: tea.KeyEnter})
		}
	}
	return nil
}

// handleChannelFormMouse handles clicks on the channel create/edit form:
// the Name field and (when the selected type is voice) the Max Users field
// are click-to-focus-only; the type radio group sets both focus and the
// selected type directly (mirroring what left/right arrow navigation does,
// including re-deriving a plugin kind's fields via setPluginKind); Submit/
// Cancel synthesize Enter. Plugin-declared create_fields (only present when
// a plugin channel kind is selected) are not wired -- the Plugins category
// itself is explicitly deferred pending investigation, see the Plan's Area
// 4.6, and this is the one place that surface leaks into the Channels
// category.
func (a *App) handleChannelFormMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	state := s.ChannelFormState
	if state == nil {
		return nil
	}
	layout := computeChannelFormLayout(state)

	if zoneInBounds("srvmgmt-channelform-submit", msg) {
		a.setChannelFormFocus(state, layout, layout.submitField)
		return a.handleChannelFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-channelform-cancel", msg) {
		a.setChannelFormFocus(state, layout, layout.cancelField)
		return a.handleChannelFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-channelform-name", msg) {
		a.setChannelFormFocus(state, layout, 0)
		return nil
	}
	if layout.isVoice && zoneInBounds("srvmgmt-channelform-maxusers", msg) {
		a.setChannelFormFocus(state, layout, layout.maxUsersField)
		return nil
	}

	pluginTypeLocked := state.Mode == "edit" && state.OriginalType == models.ChannelTypePlugin
	if !pluginTypeLocked {
		pluginKinds := a.pluginKindOptions()
		total := 3 + len(pluginKinds)
		for idx := 0; idx < total; idx++ {
			if !zoneInBounds(fmt.Sprintf("srvmgmt-channelform-type:%d", idx), msg) {
				continue
			}
			a.setChannelFormFocus(state, layout, 1)
			state.TypeIndex = idx
			if idx >= 3 {
				setPluginKind(state, pluginKinds[idx-3])
			} else {
				state.PluginFields = nil
				state.PluginTextInputs = nil
				state.PluginValues = nil
			}
			return nil
		}
	}
	return nil
}

// setChannelFormFocus applies the same blur-all-then-focus-one pattern
// handleChannelFormKey's local focusField closure uses for Tab/Shift+Tab,
// as a directly-callable, exported-within-package variant since a click
// names an exact target field rather than stepping relatively.
func (a *App) setChannelFormFocus(state *ChannelFormState, layout channelFormFieldLayout, field int) {
	state.FocusField = field
	state.NameTextInput.Blur()
	state.MaxUsersInput.Blur()
	for i := range state.PluginTextInputs {
		state.PluginTextInputs[i].Blur()
	}
	switch {
	case field == 0:
		state.NameTextInput.Focus()
	case layout.isVoice && field == layout.maxUsersField:
		state.MaxUsersInput.Focus()
	case layout.isPlugin && field >= layout.pluginStart && field < layout.pluginStart+len(state.PluginFields):
		idx := field - layout.pluginStart
		ft := state.PluginFields[idx].Type
		if ft == "text" || ft == "number" {
			state.PluginTextInputs[idx].Focus()
		}
	}
}

// handleRoleFormMouse handles clicks on the role create/edit form's 8
// fields: Name and Display Order are click-to-focus-only; the Permission
// Preset and Color groups set both focus and their selected index directly
// (mirroring what up/down and left/right do); the two checkboxes
// synthesize Space; Submit/Cancel synthesize Enter.
func (a *App) handleRoleFormMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.serverManagementState
	state := s.RoleFormState
	if state == nil {
		return nil
	}

	if zoneInBounds("srvmgmt-roleform-submit", msg) {
		state.FocusField = 6
		return a.handleRoleFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-roleform-cancel", msg) {
		state.FocusField = 7
		return a.handleRoleFormKey(tea.KeyMsg{Type: tea.KeyEnter})
	}
	if zoneInBounds("srvmgmt-roleform-name", msg) {
		state.FocusField = 0
		return nil
	}
	if zoneInBounds("srvmgmt-roleform-order", msg) {
		state.FocusField = 5
		return nil
	}
	if zoneInBounds("srvmgmt-roleform-hoisted", msg) {
		state.FocusField = 3
		return a.handleRoleFormKey(tea.KeyMsg{Type: tea.KeySpace})
	}
	if zoneInBounds("srvmgmt-roleform-mentionable", msg) {
		state.FocusField = 4
		return a.handleRoleFormKey(tea.KeyMsg{Type: tea.KeySpace})
	}
	for i := 0; i < 4; i++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-roleform-preset:%d", i), msg) {
			state.FocusField = 1
			state.PresetIndex = i
			return nil
		}
	}
	for i := 0; i < 5; i++ {
		if zoneInBounds(fmt.Sprintf("srvmgmt-roleform-color:%d", i), msg) {
			state.FocusField = 2
			state.ColorIndex = i
			return nil
		}
	}
	return nil
}
