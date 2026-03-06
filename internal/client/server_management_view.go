package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

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
		// Filter out categories and DM channels - show only text channels
		var channelList []*models.Channel
		for _, ch := range channels {
			if ch.Type == models.ChannelTypeText {
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
func (a *App) navigateUpInCategory() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Channels
		if s.SelectedChannel > 0 {
			s.SelectedChannel--
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
		if s.SelectedChannel < len(s.ChannelList)-1 {
			s.SelectedChannel++
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
		s.ChannelFormOpen = true
		s.ChannelFormState = &ChannelFormState{
			Mode:       "create",
			TypeIndex:  0,
			FocusField: 0,
		}
	case 1: // Create Role
		s.RoleFormOpen = true
		s.RoleFormState = &RoleFormState{
			Mode:       "create",
			PresetIndex: 0,
			ColorIndex: 0,
			FocusField: 0,
		}
	}
}

func (a *App) handleEditAction() {
	s := a.serverManagementState
	switch s.SelectedCategory {
	case 0: // Edit Channel
		if s.SelectedChannel >= 0 && s.SelectedChannel < len(s.ChannelList) {
			ch := s.ChannelList[s.SelectedChannel]
			s.ChannelFormOpen = true
			s.ChannelFormState = &ChannelFormState{
				Mode:             "edit",
				EditingChannelID: &ch.ID,
				NameInput:        ch.Name,
				TypeIndex:        0, // Text channel
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
	// TODO: Implement move channel dialog
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
	// TODO: Implement channel form key handling
	if msg.String() == "esc" {
		a.serverManagementState.ChannelFormOpen = false
		a.serverManagementState.ChannelFormState = nil
	}
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
	// TODO: Implement delete confirmation key handling
	if msg.String() == "esc" || msg.String() == "n" {
		a.serverManagementState.DeleteConfirmOpen = false
		a.serverManagementState.DeleteConfirmChannel = nil
		a.serverManagementState.DeleteConfirmRole = nil
	}
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

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
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
	var buf strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Channel Management"))
	buf.WriteString("\n")

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(descStyle.Render("Manage text, voice channels, and categories"))
	buf.WriteString("\n")

	// Stats
	categoryCount := 0
	for _, ch := range s.ChannelList {
		if ch.Type == models.ChannelTypeCategory {
			categoryCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(statsStyle.Render(fmt.Sprintf("%d channels · %d categories", len(s.ChannelList), categoryCount)))
	buf.WriteString("\n\n")

	// Separator
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	// Channel list
	for i, ch := range s.ChannelList {
		selected := s.FocusOnForm && i == s.SelectedChannel

		prefix := "  "
		if selected {
			prefix = "⚑ "
		}

		channelName := fmt.Sprintf("# %s", ch.Name)

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Cyan)).
				Bold(true).
				Width(width - 4).
				Render(prefix + channelName)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 4).
				Render(prefix + channelName)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	// Footer help
	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(helpStyle.Render("Navigation: ↑↓ select · Shift+↑↓ reorder · Esc close"))
	buf.WriteString("\n")
	buf.WriteString(helpStyle.Render("Actions: C create · E edit · M move · D delete"))

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(buf.String())
}

// renderRolesCategory renders the Roles management category
func (a *App) renderRolesCategory(width, height int, s *ServerManagementState) string {
	var buf strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Role Management"))
	buf.WriteString("\n")

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(descStyle.Render("Configure server roles and permissions"))
	buf.WriteString("\n")

	// Stats - count total members across all roles
	totalMembers := len(a.serverManagementState.MemberList)
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(statsStyle.Render(fmt.Sprintf("%d roles · %d total members", len(s.RoleList), totalMembers)))
	buf.WriteString("\n\n")

	// Separator
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	// Role list
	for i, role := range s.RoleList {
		selected := s.FocusOnForm && i == s.SelectedRole

		prefix := "  "
		if selected {
			prefix = "⚑ "
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
				Width(width - 4).
				Render(prefix + roleLine)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 4).
				Render(prefix + roleLine)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	// Footer help
	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(helpStyle.Render("Navigation: ↑↓ select · Shift+↑↓ reorder · Esc close"))
	buf.WriteString("\n")
	buf.WriteString(helpStyle.Render("Actions: C create · E edit · P permissions · D delete"))

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(buf.String())
}

// renderMembersCategory renders the Members management category
func (a *App) renderMembersCategory(width, height int, s *ServerManagementState) string {
	var buf strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Member Management"))
	buf.WriteString("\n")

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(descStyle.Render("View members, assign roles, and moderate users"))
	buf.WriteString("\n")

	// Stats - count online members
	onlineCount := 0
	for _, member := range s.MemberList {
		if member.User != nil && member.User.Status == models.StatusOnline {
			onlineCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(statsStyle.Render(fmt.Sprintf("%d members · %d online", len(s.MemberList), onlineCount)))
	buf.WriteString("\n\n")

	// Filter bar
	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	filterText := fmt.Sprintf("Filters: Role: %s | Status: %s | Sort: %s",
		s.FilterRole, strings.Title(s.FilterOnline), s.SortBy)
	buf.WriteString(filterStyle.Render(filterText))
	buf.WriteString("\n")

	// Separator
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n")

	// Member list (table format)
	for i, member := range s.MemberList {
		if member.User == nil {
			continue
		}

		selected := s.FocusOnForm && i == s.SelectedMember

		prefix := "  "
		if selected {
			prefix = "⚑ "
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

		// Join date (placeholder - would need actual data)
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
				Width(width - 4).
				Render(prefix + memberLine)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(width - 4).
				Render(prefix + memberLine)
		}
		buf.WriteString(line)
		buf.WriteString("\n")
	}

	// Footer help
	buf.WriteString("\n")
	buf.WriteString(strings.Repeat("─", width-4))
	buf.WriteString("\n\n")

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	buf.WriteString(helpStyle.Render("Navigation: ↑↓ navigate · Esc close"))
	buf.WriteString("\n")
	buf.WriteString(helpStyle.Render("Actions: Shift+R assign role · Shift+K kick · Shift+B ban · F filters · S search"))

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(buf.String())
}

// renderMessagesCategory renders the Messages/Retention management category
func (a *App) renderMessagesCategory(width, height int, s *ServerManagementState) string {
	var buf strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	buf.WriteString(headerStyle.Render("Message Retention Settings"))
	buf.WriteString("\n\n")

	// Server Default Policy section
	sectionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true)
	buf.WriteString(sectionStyle.Render("Server Default Policy"))
	buf.WriteString("\n")

	policyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(2)

	if s.RetentionPolicy == nil {
		buf.WriteString(policyStyle.Render("No retention policy configured. Messages will be kept indefinitely."))
	} else {
		// Show configured policy
		buf.WriteString(policyStyle.Render(fmt.Sprintf("Time-based: %d days", s.RetentionPolicy.TimeRetentionDays)))
		buf.WriteString("\n")
		buf.WriteString(policyStyle.Render(fmt.Sprintf("Count-based: %d messages max", s.RetentionPolicy.MaxMessageCount)))
	}
	buf.WriteString("\n\n")

	// Channel Overrides section
	buf.WriteString(sectionStyle.Render("Channel Overrides"))
	buf.WriteString("\n")

	if s.ChannelOverrides == nil || len(s.ChannelOverrides) == 0 {
		buf.WriteString(policyStyle.Render("No channel-specific overrides configured."))
	} else {
		for i, override := range s.ChannelOverrides {
			selected := s.FocusOnForm && i == s.SelectedOverride
			overrideText := fmt.Sprintf("#%s: %d days, %d max",
				override.ChannelID.String()[:8],
				override.TimeRetentionDays,
				override.MaxMessageCount)

			if selected {
				buf.WriteString(lipgloss.NewStyle().
					Background(lipgloss.Color(a.theme.Colors.Selection)).
					Render("  " + overrideText))
			} else {
				buf.WriteString(policyStyle.Render(overrideText))
			}
			buf.WriteString("\n")
		}
	}
	buf.WriteString("\n")

	// Actions section
	buf.WriteString(sectionStyle.Render("Actions"))
	buf.WriteString("\n")

	actionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		PaddingLeft(2)
	buf.WriteString(actionStyle.Render("E - Edit server default policy"))
	buf.WriteString("\n")
	buf.WriteString(actionStyle.Render("N - Create channel override"))
	buf.WriteString("\n")
	buf.WriteString(actionStyle.Render("D - Delete selected override"))
	buf.WriteString("\n")
	buf.WriteString(actionStyle.Render("P - Prune messages now (manual cleanup)"))

	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(buf.String())
}
