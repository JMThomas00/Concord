package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// ═══════════════════════════════════════════════════════════════════════════
// SERVER SETTINGS PAGE LAYOUT GUIDE
// ═══════════════════════════════════════════════════════════════════════════
//
// When creating new Server Settings pages, follow the +2 Padding Pattern
// documented in internal/client/SETTINGS_PAGE_TEMPLATE.md
//
// Quick Reference:
//   - Count your top section lines, add +2 for padding
//   - Example: 5 lines (header+subtitle+stats+blank+sep) → pageTopExtra = 3
//   - Bottom: 2 help lines → pageBottomExtra = 1
//   - ALWAYS use .Padding(0, 1) on content pages (NOT on sidebars)
//
// See SETTINGS_PAGE_TEMPLATE.md for complete examples and patterns.
// ═══════════════════════════════════════════════════════════════════════════

// openServerManagement transitions the app into the Server Management view
func (a *App) openServerManagement(returnTo View, categoryIndex int) {
	if a.activeConn == nil {
		a.statusMessage = "No active server connection"
		return
	}

	categories := []string{"Channels", "Roles", "Members", "Messages"}

	// Load initial data for the selected category
	serverID := a.getActiveServerID()

	a.serverManagementState = &ServerManagementState{
		Categories:       categories,
		SelectedCategory: categoryIndex,
		FocusOnForm:      false,
		PreviousView:     returnTo,
	}

	// Load channels for Channels category
	if categoryIndex == 0 {
		a.loadChannelListForManagement(serverID)
	}

	// Load roles for Roles category
	if categoryIndex == 1 {
		a.loadRoleListForManagement(serverID)
	}

	// Load members for Members category
	if categoryIndex == 2 {
		a.loadMemberListForManagement()
		a.serverManagementState.FilterRole = "All"
		a.serverManagementState.FilterOnline = "all"
		a.serverManagementState.SortBy = "role"
	}

	// Load retention policy for Messages category
	if categoryIndex == 3 {
		a.loadRetentionPolicyForManagement(serverID)
	}

	a.view = ViewServerManagement
}

// loadChannelListForManagement loads channels for the Channels category
func (a *App) loadChannelListForManagement(serverID uuid.UUID) {
	if a.activeConn == nil {
		return
	}

	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()

	if channels, ok := a.activeConn.Channels[serverID]; ok {
		// Include text channels AND categories (not DMs)
		var channelList []*models.Channel
		for _, ch := range channels {
			if ch.Type == models.ChannelTypeText || ch.Type == models.ChannelTypeCategory {
				channelList = append(channelList, ch)
			}
		}
		a.serverManagementState.ChannelList = channelList
	}
}

// loadRoleListForManagement loads roles for the Roles category
func (a *App) loadRoleListForManagement(serverID uuid.UUID) {
	if a.activeConn == nil {
		return
	}

	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()

	if roles, ok := a.activeConn.Roles[serverID]; ok {
		// Sort by position descending (higher position = higher in list)
		roleList := make([]*models.Role, len(roles))
		copy(roleList, roles)
		for i := 0; i < len(roleList)-1; i++ {
			for j := i + 1; j < len(roleList); j++ {
				if roleList[j].Position > roleList[i].Position {
					roleList[i], roleList[j] = roleList[j], roleList[i]
				}
			}
		}
		a.serverManagementState.RoleList = roleList
	}
}

// loadMemberListForManagement loads members for the Members category
func (a *App) loadMemberListForManagement() {
	if a.activeConn == nil {
		return
	}

	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()

	a.serverManagementState.MemberList = a.activeConn.Members
}

// loadRetentionPolicyForManagement loads retention policy for Messages category
func (a *App) loadRetentionPolicyForManagement(serverID uuid.UUID) {
	// For now, set to nil - will be loaded from server when implemented
	a.serverManagementState.RetentionPolicy = nil
	a.serverManagementState.ChannelOverrides = nil
}

// handleServerManagementKey processes key events when in ViewServerManagement
func (a *App) handleServerManagementKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		a.view = ViewMain
		return nil
	}

	// Handle modal/form inputs first
	if s.ChannelFormOpen {
		return a.handleChannelFormKey(msg)
	}
	if s.RoleFormOpen {
		return a.handleRoleFormKey(msg)
	}
	if s.PermissionsEditorOpen {
		return a.handlePermissionsEditorKey(msg)
	}
	if s.DeleteConfirmOpen {
		return a.handleDeleteConfirmKey(msg)
	}
	if s.MoveDialogOpen {
		return a.handleMoveDialogKey(msg)
	}
	if s.FilterPanelOpen {
		return a.handleFilterPanelKey(msg)
	}
	if s.SearchInputOpen {
		return a.handleSearchInputKey(msg)
	}
	if s.RetentionFormState != nil {
		return a.handleRetentionFormKey(msg)
	}
	if s.PruneConfirmOpen {
		return a.handlePruneConfirmKey(msg)
	}

	switch msg.String() {
	case "esc":
		// Return to previous view
		returnTo := s.PreviousView
		a.serverManagementState = nil
		a.view = returnTo

	case "up", "k":
		if !s.FocusOnForm {
			// Navigate categories
			if s.SelectedCategory > 0 {
				s.SelectedCategory--
				a.loadCategoryData(s.SelectedCategory)
			}
		} else {
			// Navigate within category content
			a.navigateUpInCategory()
		}

	case "down", "j":
		if !s.FocusOnForm {
			// Navigate categories
			if s.SelectedCategory < len(s.Categories)-1 {
				s.SelectedCategory++
				a.loadCategoryData(s.SelectedCategory)
			}
		} else {
			// Navigate within category content
			a.navigateDownInCategory()
		}

	case "tab":
		// Toggle between category list and form content
		s.FocusOnForm = !s.FocusOnForm

	case "enter":
		if !s.FocusOnForm {
			// Enter into the selected category
			s.FocusOnForm = true
		}

	case "c", "C":
		// Create action (context-dependent)
		if s.FocusOnForm {
			a.handleCreateAction()
		}

	case "e", "E":
		// Edit action (context-dependent)
		if s.FocusOnForm {
			a.handleEditAction()
		}

	case "d", "D":
		// Delete action (context-dependent)
		if s.FocusOnForm {
			a.handleDeleteAction()
		}

	case "m", "M":
		// Move action (Channels only)
		if s.FocusOnForm && s.SelectedCategory == 0 {
			a.handleMoveChannelAction()
		}

	case "p", "P":
		// Permissions editor (Roles) or Prune (Messages)
		if s.FocusOnForm {
			if s.SelectedCategory == 1 {
				a.handlePermissionsAction()
			} else if s.SelectedCategory == 3 {
				a.handlePruneAction()
			}
		}

	case "shift+up":
		// Reorder up (Channels and Roles)
		if s.FocusOnForm && (s.SelectedCategory == 0 || s.SelectedCategory == 1) {
			a.handleReorderUp()
		}

	case "shift+down":
		// Reorder down (Channels and Roles)
		if s.FocusOnForm && (s.SelectedCategory == 0 || s.SelectedCategory == 1) {
			a.handleReorderDown()
		}

	case "shift+r", "shift+R":
		// Assign role (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleAssignRoleAction()
		}

	case "shift+k", "shift+K":
		// Kick member (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleKickMemberAction()
		}

	case "shift+b", "shift+B":
		// Ban member (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleBanMemberAction()
		}

	case "f", "F":
		// Open filters (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			s.FilterPanelOpen = true
			s.FilterPanelFocus = 0
		}

	case "s", "S":
		// Open search (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			s.SearchInputOpen = true
			s.SearchInputValue = ""
			s.SearchInputCursor = 0
		}

	case "n", "N":
		// Create channel override (Messages)
		if s.FocusOnForm && s.SelectedCategory == 3 {
			a.handleCreateChannelOverride()
		}
	}

	return nil
}

// loadCategoryData loads data when switching categories
func (a *App) loadCategoryData(categoryIndex int) {
	serverID := a.getActiveServerID()

	switch categoryIndex {
	case 0: // Channels
		a.loadChannelListForManagement(serverID)
		a.serverManagementState.SelectedChannel = 0
	case 1: // Roles
		a.loadRoleListForManagement(serverID)
		a.serverManagementState.SelectedRole = 0
	case 2: // Members
		a.loadMemberListForManagement()
		a.serverManagementState.SelectedMember = 0
	case 3: // Messages
		a.loadRetentionPolicyForManagement(serverID)
	}
}

// Navigation helpers
// buildChannelDisplayOrder creates a list of channel indices in display order
// (matching the hierarchical rendering order)
func (a *App) buildChannelDisplayOrder() []int {
	s := a.serverManagementState
	var displayOrder []int

	// Separate categories and channels
	var categories []*models.Channel
	channelsByCategory := make(map[uuid.UUID][]*models.Channel)
	var topLevelChannels []*models.Channel

	for _, ch := range s.ChannelList {
		if ch.Type == models.ChannelTypeCategory {
			categories = append(categories, ch)
		} else if ch.CategoryID == uuid.Nil {
			topLevelChannels = append(topLevelChannels, ch)
		} else {
			channelsByCategory[ch.CategoryID] = append(channelsByCategory[ch.CategoryID], ch)
		}
	}

	// Add top-level channels first
	for _, ch := range topLevelChannels {
		for i, listCh := range s.ChannelList {
			if listCh.ID == ch.ID {
				displayOrder = append(displayOrder, i)
				break
			}
		}
	}

	// Add categories and their channels
	for _, category := range categories {
		// Add category
		for i, listCh := range s.ChannelList {
			if listCh.ID == category.ID {
				displayOrder = append(displayOrder, i)
				break
			}
		}

		// Add channels in this category
		if channels, ok := channelsByCategory[category.ID]; ok {
			for _, ch := range channels {
				for i, listCh := range s.ChannelList {
					if listCh.ID == ch.ID {
						displayOrder = append(displayOrder, i)
						break
					}
				}
			}
		}
	}

	return displayOrder
}

func (a *App) navigateUpInCategory() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Channels
		// Build display order
		displayOrder := a.buildChannelDisplayOrder()
		if len(displayOrder) == 0 {
			return
		}

		// Find current position in display order
		currentDisplayPos := -1
		for i, idx := range displayOrder {
			if idx == s.SelectedChannel {
				currentDisplayPos = i
				break
			}
		}

		// Move up in display order
		if currentDisplayPos > 0 {
			s.SelectedChannel = displayOrder[currentDisplayPos-1]
		}

	case 1: // Roles
		if s.SelectedRole > 0 {
			s.SelectedRole--
		}
	case 2: // Members
		if s.SelectedMember > 0 {
			s.SelectedMember--
		}
	case 3: // Messages
		if s.SelectedOverride > 0 {
			s.SelectedOverride--
		}
	}
}

func (a *App) navigateDownInCategory() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Channels
		// Build display order
		displayOrder := a.buildChannelDisplayOrder()
		if len(displayOrder) == 0 {
			return
		}

		// Find current position in display order
		currentDisplayPos := -1
		for i, idx := range displayOrder {
			if idx == s.SelectedChannel {
				currentDisplayPos = i
				break
			}
		}

		// Move down in display order
		if currentDisplayPos >= 0 && currentDisplayPos < len(displayOrder)-1 {
			s.SelectedChannel = displayOrder[currentDisplayPos+1]
		}

	case 1: // Roles
		if s.SelectedRole < len(s.RoleList)-1 {
			s.SelectedRole++
		}
	case 2: // Members
		if s.SelectedMember < len(s.MemberList)-1 {
			s.SelectedMember++
		}
	case 3: // Messages
		if s.ChannelOverrides != nil && s.SelectedOverride < len(s.ChannelOverrides)-1 {
			s.SelectedOverride++
		}
	}
}

// Action handlers - will be implemented per category
func (a *App) handleCreateAction() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Create Channel
		// Initialize textinput
		nameInput := textinput.New()
		nameInput.Placeholder = "channel-name"
		nameInput.CharLimit = 50
		nameInput.Width = 40
		nameInput.Focus()

		// Detect parent category if creating within a selected group
		var parentCategoryID *uuid.UUID
		if s.SelectedChannel >= 0 && s.SelectedChannel < len(s.ChannelList) {
			selectedCh := s.ChannelList[s.SelectedChannel]
			if selectedCh.Type == models.ChannelTypeCategory {
				id := selectedCh.ID
				parentCategoryID = &id
			} else if selectedCh.CategoryID != uuid.Nil {
				id := selectedCh.CategoryID
				parentCategoryID = &id
			}
		}

		s.ChannelFormOpen = true
		s.ChannelFormState = &ChannelFormState{
			Mode:          "create",
			NameTextInput: nameInput,
			TypeIndex:     0,
			CategoryID:    parentCategoryID,
			FocusField:    0,
		}
	case 1: // Create Role
		s.RoleFormOpen = true
		s.RoleFormState = &RoleFormState{
			Mode:        "create",
			PresetIndex: 0,
			ColorIndex:  0,
			FocusField:  0,
		}
	}
}

func (a *App) handleEditAction() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Edit Channel
		if s.SelectedChannel >= 0 && s.SelectedChannel < len(s.ChannelList) {
			selectedCh := s.ChannelList[s.SelectedChannel]

			// Initialize textinput with current name
			nameInput := textinput.New()
			nameInput.SetValue(selectedCh.Name)
			nameInput.CharLimit = 50
			nameInput.Width = 40
			nameInput.Focus()

			// Determine type index
			typeIndex := 0
			if selectedCh.Type == models.ChannelTypeCategory {
				typeIndex = 1
			}

			// Get category ID
			var catID *uuid.UUID
			if selectedCh.CategoryID != uuid.Nil {
				id := selectedCh.CategoryID
				catID = &id
			}

			// Get channel ID
			chID := selectedCh.ID

			s.ChannelFormOpen = true
			s.ChannelFormState = &ChannelFormState{
				Mode:             "edit",
				EditingChannelID: &chID,
				NameTextInput:    nameInput,
				TypeIndex:        typeIndex,
				CategoryID:       catID,
				FocusField:       0,
			}
		}
	case 1: // Edit Role
		if s.SelectedRole >= 0 && s.SelectedRole < len(s.RoleList) {
			role := s.RoleList[s.SelectedRole]
			s.RoleFormOpen = true
			s.RoleFormState = &RoleFormState{
				Mode:          "edit",
				EditingRoleID: &role.ID,
				NameInput:     role.Name,
				ColorIndex:    0,
				PresetIndex:   3, // Custom
				FocusField:    0,
			}
		}
	case 3: // Edit server default retention policy
		s.RetentionFormState = &RetentionFormState{
			Mode:       "server",
			FocusField: 0,
		}
	}
}

func (a *App) handleDeleteAction() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Delete Channel
		if s.SelectedChannel >= 0 && s.SelectedChannel < len(s.ChannelList) {
			ch := s.ChannelList[s.SelectedChannel]
			s.DeleteConfirmOpen = true
			s.DeleteConfirmChannel = ch
		}
	case 1: // Delete Role
		if s.SelectedRole >= 0 && s.SelectedRole < len(s.RoleList) {
			role := s.RoleList[s.SelectedRole]
			// Don't allow deleting @everyone role
			if role.Name != "@everyone" && role.Name != "everyone" {
				s.DeleteConfirmOpen = true
				s.DeleteConfirmRole = role
			}
		}
	case 3: // Delete channel override
		if s.ChannelOverrides != nil && s.SelectedOverride >= 0 && s.SelectedOverride < len(s.ChannelOverrides) {
			// TODO: Delete channel override
		}
	}
}

func (a *App) handleMoveChannelAction() {
	s := a.serverManagementState

	if s.SelectedChannel < 0 || s.SelectedChannel >= len(s.ChannelList) {
		return
	}

	selectedChannel := s.ChannelList[s.SelectedChannel]

	// Build category list
	var categoryList []*models.Channel
	if a.channelTree != nil {
		for _, node := range a.channelTree.FlatList {
			if node.IsCategory && node.Channel.ID != selectedChannel.ID {
				categoryList = append(categoryList, node.Channel)
			}
		}
	}

	s.MoveDialogOpen = true
	s.MoveDialogState = &MoveDialogState{
		Channel:       selectedChannel,
		CategoryList:  categoryList,
		SelectedIndex: 0, // Start at "Top Level (no group)"
	}
}

func (a *App) handlePermissionsAction() {
	s := a.serverManagementState
	if s.SelectedRole >= 0 && s.SelectedRole < len(s.RoleList) {
		role := s.RoleList[s.SelectedRole]
		s.PermissionsEditorOpen = true
		s.PermissionsEditorRole = role
		s.PermModifiedBits = uint64(role.Permissions)
		s.PermSelectedIndex = 0
		s.PermScrollOffset = 0
	}
}

func (a *App) handleReorderUp() {
	// TODO: Implement reorder up
}

func (a *App) handleReorderDown() {
	// TODO: Implement reorder down
}

func (a *App) handleAssignRoleAction() {
	// TODO: Implement assign role
}

func (a *App) handleKickMemberAction() {
	// TODO: Implement kick member
}

func (a *App) handleBanMemberAction() {
	// TODO: Implement ban member
}

func (a *App) handlePruneAction() {
	s := a.serverManagementState
	s.PruneConfirmOpen = true
}

func (a *App) handleCreateChannelOverride() {
	s := a.serverManagementState
	s.RetentionFormState = &RetentionFormState{
		Mode:       "channel",
		FocusField: 0,
	}
}

// Placeholder key handlers for forms/dialogs
func (a *App) handleChannelFormKey(msg tea.KeyMsg) tea.Cmd {
	state := a.serverManagementState.ChannelFormState

	switch msg.String() {
	case "esc":
		a.serverManagementState.ChannelFormOpen = false
		a.serverManagementState.ChannelFormState = nil
		return nil

	case "tab":
		state.FocusField = (state.FocusField + 1) % 4
		// Update textinput focus
		if state.FocusField == 0 {
			state.NameTextInput.Focus()
		} else {
			state.NameTextInput.Blur()
		}
		return nil

	case "shift+tab":
		state.FocusField--
		if state.FocusField < 0 {
			state.FocusField = 3
		}
		if state.FocusField == 0 {
			state.NameTextInput.Focus()
		} else {
			state.NameTextInput.Blur()
		}
		return nil

	case "up", "down":
		if state.FocusField == 1 {
			state.TypeIndex = 1 - state.TypeIndex // Toggle 0<->1
		}
		return nil

	case "enter":
		if state.FocusField == 2 { // Submit
			return a.handleChannelFormSubmit()
		} else if state.FocusField == 3 { // Cancel
			a.serverManagementState.ChannelFormOpen = false
			a.serverManagementState.ChannelFormState = nil
		}
		return nil
	}

	// Forward to textinput when name field has focus
	if state.FocusField == 0 {
		var cmd tea.Cmd
		state.NameTextInput, cmd = state.NameTextInput.Update(msg)
		return cmd
	}

	return nil
}

func (a *App) handleChannelFormSubmit() tea.Cmd {
	state := a.serverManagementState.ChannelFormState

	// Validation
	name := strings.TrimSpace(state.NameTextInput.Value())
	if name == "" {
		state.ErrorMsg = "Channel name is required"
		return nil
	}
	if len(name) > 100 {
		state.ErrorMsg = "Name must be 1-100 characters"
		return nil
	}

	// Connection check
	if a.activeConn == nil || a.currentServer == nil {
		state.ErrorMsg = "Not connected to server"
		return nil
	}

	// Determine type
	var channelType models.ChannelType
	if state.TypeIndex == 0 {
		channelType = models.ChannelTypeText
	} else {
		channelType = models.ChannelTypeCategory
	}

	serverID := a.currentServer.ID
	var req interface{}
	var opCode protocol.OpCode

	if state.Mode == "create" {
		req = &protocol.ChannelCreateRequest{
			ServerID:   serverID,
			Name:       name,
			Type:       channelType,
			CategoryID: state.CategoryID,
		}
		opCode = protocol.OpChannelCreate
	} else {
		req = &protocol.ChannelUpdateRequest{
			ServerID:  serverID,
			ChannelID: *state.EditingChannelID,
			Name:      &name,
		}
		opCode = protocol.OpChannelUpdate
	}

	// Build and send protocol message
	msg, err := protocol.NewMessage(opCode, req)
	if err != nil {
		state.ErrorMsg = fmt.Sprintf("Failed to build request: %v", err)
		return nil
	}

	if err := a.activeConn.Connection.Send(msg); err != nil {
		state.ErrorMsg = fmt.Sprintf("Failed to send: %v", err)
		return nil
	}

	// Close form on success
	a.serverManagementState.ChannelFormOpen = false
	a.serverManagementState.ChannelFormState = nil
	a.statusMessage = "Request sent..."

	return nil
}

func (a *App) handleRoleFormKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement role form key handling
	if msg.String() == "esc" {
		a.serverManagementState.RoleFormOpen = false
		a.serverManagementState.RoleFormState = nil
	}
	return nil
}

func (a *App) handlePermissionsEditorKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement permissions editor key handling
	if msg.String() == "esc" {
		a.serverManagementState.PermissionsEditorOpen = false
		a.serverManagementState.PermissionsEditorRole = nil
	}
	return nil
}

func (a *App) handleDeleteConfirmKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc", "n", "N":
		a.serverManagementState.DeleteConfirmOpen = false
		a.serverManagementState.DeleteConfirmChannel = nil
		a.serverManagementState.DeleteConfirmRole = nil
		return nil

	case "y", "Y", "enter":
		return a.handleDeleteConfirmed()
	}
	return nil
}

func (a *App) handleDeleteConfirmed() tea.Cmd {
	s := a.serverManagementState

	if a.activeConn == nil || a.currentServer == nil {
		return nil
	}

	if s.DeleteConfirmChannel != nil {
		req := &protocol.ChannelDeleteRequest{
			ServerID:  a.currentServer.ID,
			ChannelID: s.DeleteConfirmChannel.ID,
		}

		msg, err := protocol.NewMessage(protocol.OpChannelDelete, req)
		if err != nil {
			a.statusMessage = fmt.Sprintf("Failed: %v", err)
			return nil
		}

		if err := a.activeConn.Connection.Send(msg); err != nil {
			a.statusMessage = fmt.Sprintf("Failed: %v", err)
			return nil
		}

		a.statusMessage = fmt.Sprintf("Deleting channel #%s...", s.DeleteConfirmChannel.Name)
	}

	s.DeleteConfirmOpen = false
	s.DeleteConfirmChannel = nil

	return nil
}

func (a *App) handleMoveDialogKey(msg tea.KeyMsg) tea.Cmd {
	state := a.serverManagementState.MoveDialogState

	switch msg.String() {
	case "esc":
		a.serverManagementState.MoveDialogOpen = false
		a.serverManagementState.MoveDialogState = nil
		return nil

	case "up", "k":
		if state.SelectedIndex > 0 {
			state.SelectedIndex--
		}
		return nil

	case "down", "j":
		maxIndex := len(state.CategoryList) // +1 for "Top Level" is implicit in rendering
		if state.SelectedIndex < maxIndex {
			state.SelectedIndex++
		}
		return nil

	case "enter":
		return a.handleMoveDialogSubmit()
	}

	return nil
}

func (a *App) handleMoveDialogSubmit() tea.Cmd {
	state := a.serverManagementState.MoveDialogState

	if a.activeConn == nil || a.currentServer == nil {
		return nil
	}

	// Determine new category ID
	var newCategoryID *uuid.UUID
	if state.SelectedIndex == 0 {
		// Moving to top level - set CategoryID to uuid.Nil
		nilUUID := uuid.Nil
		newCategoryID = &nilUUID
	} else {
		// Moving to a category
		categoryIdx := state.SelectedIndex - 1
		if categoryIdx < len(state.CategoryList) {
			id := state.CategoryList[categoryIdx].ID
			newCategoryID = &id
		}
	}

	// Build update request
	req := &protocol.ChannelUpdateRequest{
		ServerID:   a.currentServer.ID,
		ChannelID:  state.Channel.ID,
		CategoryID: newCategoryID,
	}

	msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req)
	if err != nil {
		a.statusMessage = fmt.Sprintf("Failed: %v", err)
		return nil
	}

	if err := a.activeConn.Connection.Send(msg); err != nil {
		a.statusMessage = fmt.Sprintf("Failed: %v", err)
		return nil
	}

	a.serverManagementState.MoveDialogOpen = false
	a.serverManagementState.MoveDialogState = nil
	a.statusMessage = "Moving channel..."

	return nil
}

func (a *App) handleFilterPanelKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement filter panel key handling
	if msg.String() == "esc" {
		a.serverManagementState.FilterPanelOpen = false
	}
	return nil
}

func (a *App) handleSearchInputKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement search input key handling
	if msg.String() == "esc" {
		a.serverManagementState.SearchInputOpen = false
	}
	return nil
}

func (a *App) handleRetentionFormKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement retention form key handling
	if msg.String() == "esc" {
		a.serverManagementState.RetentionFormState = nil
	}
	return nil
}

func (a *App) handlePruneConfirmKey(msg tea.KeyMsg) tea.Cmd {
	// TODO: Implement prune confirmation key handling
	if msg.String() == "esc" || msg.String() == "n" {
		a.serverManagementState.PruneConfirmOpen = false
	}
	return nil
}

// renderServerManagementView renders the Server Management view
func (a *App) renderServerManagementView() string {
	s := a.serverManagementState
	if s == nil {
		return ""
	}

	totalWidth := a.width
	totalHeight := a.height
	if totalWidth < 80 {
		totalWidth = 80
	}
	if totalHeight < 20 {
		totalHeight = 20
	}

	// Split: left categories (~24 chars) | right content (rest)
	catWidth := 24
	contentWidth := totalWidth - catWidth - 1

	// ── Left: Category list ────────────────────────────────────────
	catPanel := a.renderCategorySidebar(catWidth, totalHeight-2, s)

	// ── Right: Content panel ───────────────────────────────────────
	var contentPanel string
	switch s.SelectedCategory {
	case 0: // Channels
		contentPanel = a.renderChannelsCategory(contentWidth, totalHeight-2, s)
	case 1: // Roles
		contentPanel = a.renderRolesCategory(contentWidth, totalHeight-2, s)
	case 2: // Members
		contentPanel = a.renderMembersCategory(contentWidth, totalHeight-2, s)
	case 3: // Messages
		contentPanel = a.renderMessagesCategory(contentWidth, totalHeight-2, s)
	}

	// ── Assemble ───────────────────────────────────────────────────
	content := lipgloss.JoinHorizontal(lipgloss.Top, catPanel, contentPanel)

	// Title bar
	titleBar := lipgloss.NewStyle().
		Width(totalWidth).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Render("  Server Settings  •  Esc: Back")

	baseView := lipgloss.JoinVertical(lipgloss.Left, titleBar, content)

	// Show delete confirmation dialog if active
	if s.DeleteConfirmOpen {
		return a.renderDeleteConfirmDialog()
	}

	return baseView
}

// renderCategorySidebar renders the category list sidebar
func (a *App) renderCategorySidebar(width, height int, s *ServerManagementState) string {
	var buf strings.Builder

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(width - 2)
	buf.WriteString(headerStyle.Render("SERVER SETTINGS"))
	buf.WriteString("\n\n")

	for i, category := range s.Categories {
		var line string
		selected := i == s.SelectedCategory

		prefix := "  "
		if selected {
			prefix = "▶ "
		}

		if selected && !s.FocusOnForm {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(width - 2).
				Render(prefix + category)
		} else if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(width - 2).
				Render(prefix + category)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 2).
				Render(prefix + category)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Render(buf.String())
}

// renderChannelsCategory renders the Channels management category
func (a *App) renderChannelsCategory(width, height int, s *ServerManagementState) string {
	// Check if a form/dialog is open and render it instead
	if s.ChannelFormOpen && s.ChannelFormState != nil {
		return a.renderChannelFormPage(width, height, s)
	}
	if s.MoveDialogOpen && s.MoveDialogState != nil {
		return a.renderMoveChannelPage(width, height, s)
	}

	layout := calculateSettingsLayout(width, height, 3, 1) // 3 = stats + 2 padding lines, 1 = extra help line

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Channel Management"))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Manage text, voice channels, and categories"))

	// Stats
	categoryCount := 0
	for _, ch := range s.ChannelList {
		if ch.Type == models.ChannelTypeCategory {
			categoryCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statsStyle.Render(fmt.Sprintf("%d channels · %d categories", len(s.ChannelList), categoryCount)))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Build display list (flattened hierarchical view for rendering)
	type displayItem struct {
		channel *models.Channel
		indent  int
		listIdx int // Index in s.ChannelList
	}
	var displayList []displayItem

	// Organize by category
	var categories []*models.Channel
	channelsByCategory := make(map[uuid.UUID][]*models.Channel)
	var topLevelChannels []*models.Channel

	for _, ch := range s.ChannelList {
		if ch.Type == models.ChannelTypeCategory {
			categories = append(categories, ch)
		} else if ch.CategoryID == uuid.Nil {
			topLevelChannels = append(topLevelChannels, ch)
		} else {
			channelsByCategory[ch.CategoryID] = append(channelsByCategory[ch.CategoryID], ch)
		}
	}

	// Build display list: top-level channels first
	for _, ch := range topLevelChannels {
		listIdx := -1
		for j, listCh := range s.ChannelList {
			if listCh.ID == ch.ID {
				listIdx = j
				break
			}
		}
		displayList = append(displayList, displayItem{channel: ch, indent: 0, listIdx: listIdx})
	}

	// Then categories with their children
	for _, category := range categories {
		catIdx := -1
		for j, listCh := range s.ChannelList {
			if listCh.ID == category.ID {
				catIdx = j
				break
			}
		}
		displayList = append(displayList, displayItem{channel: category, indent: 0, listIdx: catIdx})

		// Add channels in this category (indented)
		if channels, ok := channelsByCategory[category.ID]; ok {
			for _, ch := range channels {
				chIdx := -1
				for j, listCh := range s.ChannelList {
					if listCh.ID == ch.ID {
						chIdx = j
						break
					}
				}
				displayList = append(displayList, displayItem{channel: ch, indent: 1, listIdx: chIdx})
			}
		}
	}

	// Calculate scrollable area
	maxVisible := layout.middleLines - 2
	if maxVisible < 3 {
		maxVisible = 3
	}

	visibleStart := 0
	visibleEnd := len(displayList)

	// Find selected item in display list
	selectedDisplayIdx := -1
	for i, item := range displayList {
		if item.listIdx == s.SelectedChannel {
			selectedDisplayIdx = i
			break
		}
	}

	// Calculate viewport with selected item centered
	if len(displayList) > maxVisible && selectedDisplayIdx >= 0 {
		halfVisible := maxVisible / 2
		visibleStart = selectedDisplayIdx - halfVisible
		visibleEnd = selectedDisplayIdx + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(displayList) {
			visibleEnd = len(displayList)
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

	// Render visible items
	for i := visibleStart; i < visibleEnd; i++ {
		item := displayList[i]
		ch := item.channel
		selected := s.FocusOnForm && item.listIdx == s.SelectedChannel

		var prefix string
		if item.indent == 0 {
			prefix = "  "
			if selected {
				prefix = "▶ "
			}
		} else {
			prefix = "    "
			if selected {
				prefix = "  ▶ "
			}
		}

		var channelName string
		if ch.Type == models.ChannelTypeCategory {
			channelName = fmt.Sprintf("▼ %s", ch.Name)
		} else {
			channelName = fmt.Sprintf("# %s", ch.Name)
		}

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + channelName)
		} else {
			line = prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(channelName)
		}
		middle.writeLine(line)
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(displayList) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(displayList)-visibleEnd)))
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Shift+↑↓ reorder · Esc close"))
	bottom.writeLine(helpStyle.Render("Actions: C create · E edit · M move · D delete"))

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

// renderRolesCategory renders the Roles management category
func (a *App) renderRolesCategory(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 3, 1) // 3 = stats + 2 padding lines, 1 = extra help line

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Role Management"))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Configure server roles and permissions"))

	// Stats
	totalMembers := len(a.serverManagementState.MemberList)
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statsStyle.Render(fmt.Sprintf("%d roles · %d total members", len(s.RoleList), totalMembers)))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Calculate scrollable area
	maxVisible := layout.middleLines - 2
	if maxVisible < 3 {
		maxVisible = 3
	}

	visibleStart := 0
	visibleEnd := len(s.RoleList)

	// Calculate viewport with selected item centered
	if len(s.RoleList) > maxVisible && s.SelectedRole >= 0 {
		halfVisible := maxVisible / 2
		visibleStart = s.SelectedRole - halfVisible
		visibleEnd = s.SelectedRole + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(s.RoleList) {
			visibleEnd = len(s.RoleList)
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

	// Render visible roles
	for i := visibleStart; i < visibleEnd; i++ {
		role := s.RoleList[i]
		selected := s.FocusOnForm && i == s.SelectedRole

		prefix := "  "
		if selected {
			prefix = "▶ "
		}

		// Count members with this role
		memberCount := 0
		for _, member := range a.serverManagementState.MemberList {
			if member.HighestRole != nil && member.HighestRole.ID == role.ID {
				memberCount++
			}
			// Also count @everyone
			if role.Name == "@everyone" || role.Name == "everyone" {
				memberCount = totalMembers
				break
			}
		}

		roleLine := fmt.Sprintf("%s (%d member", role.Name, memberCount)
		if memberCount != 1 {
			roleLine += "s)"
		} else {
			roleLine += ")"
		}

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + roleLine)
		} else {
			line = prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(roleLine)
		}
		middle.writeLine(line)
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(s.RoleList) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(s.RoleList)-visibleEnd)))
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Shift+↑↓ reorder · Esc close"))
	bottom.writeLine(helpStyle.Render("Actions: C create · E edit · P permissions · D delete"))

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

// renderMembersCategory renders the Members management category
func (a *App) renderMembersCategory(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 4, 1) // 4 = stats + filter + 2 padding lines, 1 = extra help line

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Member Management"))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("View members, assign roles, and moderate users"))

	// Stats
	onlineCount := 0
	for _, member := range s.MemberList {
		if member.User != nil && member.User.Status == models.StatusOnline {
			onlineCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statsStyle.Render(fmt.Sprintf("%d members · %d online", len(s.MemberList), onlineCount)))

	// Filter bar
	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	filterText := fmt.Sprintf("Filters: Role: %s | Status: %s | Sort: %s",
		s.FilterRole, strings.Title(s.FilterOnline), s.SortBy)
	top.writeLine(filterStyle.Render(filterText))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Calculate scrollable area
	maxVisible := layout.middleLines - 2
	if maxVisible < 3 {
		maxVisible = 3
	}

	visibleStart := 0
	visibleEnd := len(s.MemberList)

	// Calculate viewport with selected item centered
	if len(s.MemberList) > maxVisible && s.SelectedMember >= 0 {
		halfVisible := maxVisible / 2
		visibleStart = s.SelectedMember - halfVisible
		visibleEnd = s.SelectedMember + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(s.MemberList) {
			visibleEnd = len(s.MemberList)
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

	// Render visible members
	for i := visibleStart; i < visibleEnd; i++ {
		member := s.MemberList[i]
		if member.User == nil {
			continue
		}

		selected := s.FocusOnForm && i == s.SelectedMember

		prefix := "  "
		if selected {
			prefix = "▶ "
		}

		// Status indicator
		statusDot := "○"
		if member.User.Status == models.StatusOnline {
			statusDot = "●"
		}

		// Role name
		roleName := "@everyone"
		if member.HighestRole != nil {
			roleName = member.HighestRole.Name
		}

		// Join date
		joinDate := "2026-02-22"
		if member.Member != nil && !member.Member.JoinedAt.IsZero() {
			joinDate = member.Member.JoinedAt.Format("2006-01-02")
		}

		// Format: username  status  role  joined
		memberLine := fmt.Sprintf("%-20s %s %-15s %s",
			member.User.Username, statusDot, roleName, joinDate)

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + memberLine)
		} else {
			line = prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(memberLine)
		}
		middle.writeLine(line)
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(s.MemberList) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(s.MemberList)-visibleEnd)))
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ navigate · Esc close"))
	bottom.writeLine(helpStyle.Render("Actions: Shift+R assign role · Shift+K kick · Shift+B ban · F filters · S search"))

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

// renderMessagesCategory renders the Messages/Retention management category
func (a *App) renderMessagesCategory(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 2, 1) // 2 = 2 padding lines, 1 = extra help line

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Message Retention Settings"))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Configure message retention policies and channel overrides"))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Server Default Policy section
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true)
	middle.writeLine(sectionStyle.Render("Server Default Policy"))

	policyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	if s.RetentionPolicy == nil {
		middle.writeLine(policyStyle.Render("  No retention policy configured. Messages kept indefinitely."))
	} else {
		middle.writeLine(policyStyle.Render(fmt.Sprintf("  Time-based: %d days", s.RetentionPolicy.TimeRetentionDays)))
		middle.writeLine(policyStyle.Render(fmt.Sprintf("  Count-based: %d messages max", s.RetentionPolicy.MaxMessageCount)))
	}
	middle.writeBlank()

	// Channel Overrides section
	middle.writeLine(sectionStyle.Render("Channel Overrides"))

	if s.ChannelOverrides == nil || len(s.ChannelOverrides) == 0 {
		middle.writeLine(policyStyle.Render("  No channel-specific overrides configured."))
	} else {
		for i, override := range s.ChannelOverrides {
			selected := s.FocusOnForm && i == s.SelectedOverride
			overrideText := fmt.Sprintf("#%s: %d days, %d max",
				override.ChannelID.String()[:8],
				override.TimeRetentionDays,
				override.MaxMessageCount)

			if selected {
				line := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Background)).
					Background(lipgloss.Color(a.theme.Colors.Cyan)).
					Bold(true).
					Width(layout.interiorWidth).
					Render("▶ " + overrideText)
				middle.writeLine(line)
			} else {
				middle.writeLine(policyStyle.Render("  " + overrideText))
			}
		}
	}
	middle.writeBlank()

	// Actions section
	middle.writeLine(sectionStyle.Render("Actions"))
	actionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(actionStyle.Render("  E - Edit server default policy"))
	middle.writeLine(actionStyle.Render("  N - Create channel override"))
	middle.writeLine(actionStyle.Render("  D - Delete selected override"))
	middle.writeLine(actionStyle.Render("  P - Prune messages now (manual cleanup)"))

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select override · Esc close"))
	bottom.writeLine(helpStyle.Render("Actions: E edit policy · N new override · D delete · P prune"))

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

// renderChannelFormPage renders the channel create/edit form as a full page
func (a *App) renderChannelFormPage(width, height int, s *ServerManagementState) string {
	state := s.ChannelFormState
	if state == nil {
		return ""
	}

	// Use same layout calculation as channel list
	layout := calculateSettingsLayout(width, height, 2, 0) // 2 = 2 padding lines, 0 = bottom is correct

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	title := "Create Channel / Channel Group"
	if state.Mode == "edit" {
		if state.TypeIndex == 0 {
			title = "Edit Channel"
		} else {
			title = "Edit Channel Group"
		}
	}
	top.writeLine(headerStyle.Render(title))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	subtitle := "Create a new text channel or channel group"
	if state.Mode == "edit" {
		subtitle = "Edit channel settings"
	}
	top.writeLine(subtitleStyle.Render(subtitle))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Error message if present
	if state.ErrorMsg != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Bold(true)
		middle.writeLine(errorStyle.Render("⚠ " + state.ErrorMsg))
		middle.writeBlank()
	}

	// Name field
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	middle.writeLine(labelStyle.Render("▸ Name:"))

	// Render textinput with focus indicator
	inputView := state.NameTextInput.View()
	if state.FocusField == 0 {
		inputStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan))
		middle.writeLine(inputStyle.Render("  " + inputView))
	} else {
		middle.writeLine("  " + inputView)
	}
	middle.writeBlank()

	// Type selection
	middle.writeLine(labelStyle.Render("▸ Type:"))

	// Text Channel option
	textChannelPrefix := "  ( ) "
	if state.TypeIndex == 0 {
		textChannelPrefix = "  (●) "
	}
	textChannelLine := textChannelPrefix + "Text Channel"
	if state.FocusField == 1 {
		middle.writeLine(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render(textChannelLine))
	} else {
		middle.writeLine(textChannelLine)
	}

	// Channel Group option
	channelGroupPrefix := "  ( ) "
	if state.TypeIndex == 1 {
		channelGroupPrefix = "  (●) "
	}
	channelGroupLine := channelGroupPrefix + "Channel Group"
	if state.FocusField == 1 {
		middle.writeLine(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render(channelGroupLine))
	} else {
		middle.writeLine(channelGroupLine)
	}
	middle.writeBlank()

	// Show parent group if applicable
	if state.CategoryID != nil && a.activeConn != nil {
		a.activeConn.mu.RLock()
		if a.currentServer != nil {
			if channels, ok := a.activeConn.Channels[a.currentServer.ID]; ok {
				for _, ch := range channels {
					if ch.ID == *state.CategoryID {
						parentStyle := lipgloss.NewStyle().
							Foreground(lipgloss.Color(a.theme.Colors.Comment)).
							Italic(true)
						middle.writeLine(parentStyle.Render(fmt.Sprintf("Parent group: %s", ch.Name)))
						middle.writeBlank()
						break
					}
				}
			}
		}
		a.activeConn.mu.RUnlock()
	}

	// Buttons
	createLabel := "Create"
	if state.Mode == "edit" {
		createLabel = "Save"
	}

	var createButton, cancelButton string
	if state.FocusField == 2 {
		createButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Green)).
			Bold(true).
			Padding(0, 2).
			Render(createLabel)
	} else {
		createButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Green)).
			Render("[" + createLabel + "]")
	}

	if state.FocusField == 3 {
		cancelButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Red)).
			Bold(true).
			Padding(0, 2).
			Render("Cancel")
	} else {
		cancelButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Render("[Cancel]")
	}

	middle.writeLine("  " + createButton + "  " + cancelButton)

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Tab: Navigate · ↑↓: Select type · Enter: Submit · Esc: Cancel"))

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

// renderMoveChannelPage renders the move channel form as a full page
func (a *App) renderMoveChannelPage(width, height int, s *ServerManagementState) string {
	state := s.MoveDialogState
	if state == nil {
		return ""
	}

	// Use same layout calculation as channel list
	layout := calculateSettingsLayout(width, height, 2, 0) // 2 = 2 padding lines, 0 = bottom is correct

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render(fmt.Sprintf("Move Channel: #%s", state.Channel.Name)))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Select a new parent group or move to top level"))
	top.writeBlank()

	// Top section separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Instructions
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)
	middle.writeLine(instructionStyle.Render("Select destination:"))
	middle.writeBlank()

	// Top Level option (index 0)
	topLevelText := "  Top Level (no group)"
	if state.SelectedIndex == 0 {
		middle.writeLine(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Cyan)).
			Bold(true).
			Width(layout.interiorWidth).
			Render("▶ Top Level (no group)"))
	} else {
		middle.writeLine(lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Render(topLevelText))
	}

	// Category list
	for i, category := range state.CategoryList {
		categoryText := fmt.Sprintf("  ▼ %s", category.Name)
		listIndex := i + 1 // +1 because 0 is "Top Level"

		if state.SelectedIndex == listIndex {
			middle.writeLine(lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(fmt.Sprintf("▶ ▼ %s", category.Name)))
		} else {
			middle.writeLine(lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(categoryText))
		}
	}

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Bottom section separator
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Navigation help
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("↑↓: Navigate · Enter: Confirm · Esc: Cancel"))

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

// renderChannelFormDialog renders the channel create/edit form dialog
func (a *App) renderChannelFormDialog() string {
	state := a.serverManagementState.ChannelFormState
	if state == nil {
		return ""
	}

	dialogWidth := 70

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	title := "Create Channel / Channel Group"
	if state.Mode == "edit" {
		if state.TypeIndex == 0 {
			title = "Edit Channel"
		} else {
			title = "Edit Channel Group"
		}
	}
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")

	// Error message if present
	if state.ErrorMsg != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Width(dialogWidth - 4).
			Align(lipgloss.Center)
		content.WriteString(errorStyle.Render(state.ErrorMsg))
		content.WriteString("\n\n")
	}

	// Name field
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	content.WriteString(labelStyle.Render("Name:"))
	content.WriteString("\n")

	// Render textinput with focus indicator
	inputView := state.NameTextInput.View()
	if state.FocusField == 0 {
		inputView = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(a.theme.Colors.Cyan)).
			Padding(0, 1).
			Render(inputView)
	} else {
		inputView = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
			Padding(0, 1).
			Render(inputView)
	}
	content.WriteString(inputView)
	content.WriteString("\n\n")

	// Type radio buttons
	content.WriteString(labelStyle.Render("Type:"))
	content.WriteString("\n")

	typeTextStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	if state.FocusField == 1 {
		typeTextStyle = typeTextStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	}

	radioText := "( ) Text Channel"
	if state.TypeIndex == 0 {
		radioText = "(•) Text Channel"
	}
	content.WriteString(typeTextStyle.Render("  " + radioText))
	content.WriteString("\n")

	radioCat := "( ) Channel Group"
	if state.TypeIndex == 1 {
		radioCat = "(•) Channel Group"
	}
	content.WriteString(typeTextStyle.Render("  " + radioCat))
	content.WriteString("\n\n")

	// Parent group indicator (if creating within a category)
	if state.CategoryID != nil && state.Mode == "create" {
		// Find category name
		var categoryName string
		if a.channelTree != nil {
			for _, node := range a.channelTree.FlatList {
				if node.IsCategory && node.Channel.ID == *state.CategoryID {
					categoryName = node.Channel.Name
					break
				}
			}
		}

		if categoryName != "" {
			parentStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Italic(true)
			content.WriteString(parentStyle.Render(fmt.Sprintf("Parent group: %s", strings.ToUpper(categoryName))))
			content.WriteString("\n\n")
		}
	}

	// Buttons
	buttonRowStyle := lipgloss.NewStyle().
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	createStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Cyan)).
		Padding(0, 2)
	cancelStyle := createStyle.Copy()

	if state.FocusField == 2 {
		createStyle = createStyle.
			Background(lipgloss.Color(a.theme.Colors.Cyan)).
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Bold(true)
	}
	if state.FocusField == 3 {
		cancelStyle = cancelStyle.
			Background(lipgloss.Color(a.theme.Colors.Red)).
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Bold(true)
	}

	buttonText := "[Create]"
	if state.Mode == "edit" {
		buttonText = "[Save]"
	}

	buttons := lipgloss.JoinHorizontal(
		lipgloss.Center,
		createStyle.Render(buttonText),
		"  ",
		cancelStyle.Render("[Cancel]"),
	)
	content.WriteString(buttonRowStyle.Render(buttons))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Tab] Next  [↑↓] Toggle Type  [Enter] Confirm  [Esc] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Cyan)).
		Padding(1, 2).
		Width(dialogWidth)

	dialog := dialogStyle.Render(content.String())

	// Center on screen (overlay on top of server management view)
	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)
}

// renderMoveDialog renders the move channel dialog
func (a *App) renderMoveDialog() string {
	state := a.serverManagementState.MoveDialogState
	if state == nil {
		return ""
	}

	dialogWidth := 60

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	title := fmt.Sprintf("Move Channel: #%s", state.Channel.Name)
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")

	// Instructions
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(descStyle.Render("Select destination:"))
	content.WriteString("\n\n")

	// List of destinations
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(dialogWidth - 6)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 6)

	// Option 0: Top Level
	topLevelText := "  Top Level (no group)"
	if state.SelectedIndex == 0 {
		content.WriteString(selectedStyle.Render(topLevelText))
	} else {
		content.WriteString(normalStyle.Render(topLevelText))
	}
	content.WriteString("\n")

	// Categories
	for i, category := range state.CategoryList {
		itemIndex := i + 1 // +1 because 0 is "Top Level"
		itemText := fmt.Sprintf("  %s", strings.ToUpper(category.Name))

		if state.SelectedIndex == itemIndex {
			content.WriteString(selectedStyle.Render(itemText))
		} else {
			content.WriteString(normalStyle.Render(itemText))
		}
		content.WriteString("\n")
	}

	content.WriteString("\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[↑↓] Navigate  [Enter] Confirm  [Esc] Cancel"))

	// Wrap in dialog
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(1, 2).
		Width(dialogWidth)

	dialog := dialogStyle.Render(content.String())

	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)
}

// renderDeleteConfirmDialog renders the delete confirmation dialog
func (a *App) renderDeleteConfirmDialog() string {
	s := a.serverManagementState

	dialogWidth := 50

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("⚠ Confirm Deletion"))
	content.WriteString("\n\n")

	// Warning message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	var warningText string
	if s.DeleteConfirmChannel != nil {
		warningText = fmt.Sprintf("Delete channel #%s?\n\nThis action cannot be undone.", s.DeleteConfirmChannel.Name)
	} else if s.DeleteConfirmRole != nil {
		warningText = fmt.Sprintf("Delete role %s?\n\nThis action cannot be undone.", s.DeleteConfirmRole.Name)
	}

	content.WriteString(msgStyle.Render(warningText))
	content.WriteString("\n\n")

	// Buttons
	buttonRowStyle := lipgloss.NewStyle().
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	confirmBtn := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Background(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Padding(0, 2).
		Render("[Yes, Delete]")

	cancelBtn := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 2).
		Render("[No, Cancel]")

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
	content.WriteString(buttonRowStyle.Render(buttons))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Y] Confirm  [N/Esc] Cancel"))

	// Wrap in dialog
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Red)).
		Padding(1, 2).
		Width(dialogWidth)

	dialog := dialogStyle.Render(content.String())

	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)
}
