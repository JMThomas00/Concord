package client

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
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

// openServerManagement transitions the app into the Server Management view with a slide-from-right animation.
func (a *App) openServerManagement(returnTo View, categoryIndex int) tea.Cmd {
	if a.activeConn == nil {
		a.statusMessage = "No active server connection"
		return nil
	}

	categories := []string{"Channels", "Roles", "Members", "Messages", "Plugins", "About"}

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

	// Load installed plugin list for Plugins category
	if categoryIndex == 4 {
		a.loadPluginListForManagement(serverID)
	}

	a.view = ViewServerManagement
	if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
		return nil
	}
	a.srvMgmtAnimFrame = 0
	a.srvMgmtAnimClosing = false
	a.srvMgmtAnimating = true
	return srvMgmtPanelAnimTick()
}

// loadChannelListForManagement loads channels for the Channels category
func (a *App) loadChannelListForManagement(serverID uuid.UUID) {
	if a.activeConn == nil {
		return
	}

	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()

	if channels, ok := a.activeConn.Channels[serverID]; ok {
		// Include text, voice, plugin channels, AND categories (not DMs)
		var channelList []*models.Channel
		for _, ch := range channels {
			if ch.Type == models.ChannelTypeText || ch.Type == models.ChannelTypeVoice || ch.Type == models.ChannelTypeCategory || ch.Type == models.ChannelTypePlugin {
				channelList = append(channelList, ch)
			}
		}

		// Sort by SortOrder ascending
		for i := 0; i < len(channelList)-1; i++ {
			for j := i + 1; j < len(channelList); j++ {
				if channelList[j].SortOrder < channelList[i].SortOrder {
					channelList[i], channelList[j] = channelList[j], channelList[i]
				}
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
		// Sort alphabetically by name
		roleList := make([]*models.Role, len(roles))
		copy(roleList, roles)
		sort.Slice(roleList, func(i, j int) bool {
			return roleList[i].Name < roleList[j].Name
		})
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
	a.applyMemberFilters()
}

// applyMemberFilters applies current filters and sorting to the member list
func (a *App) applyMemberFilters() {
	s := a.serverManagementState
	if s == nil || a.activeConn == nil {
		return
	}

	a.activeConn.mu.RLock()
	allMembers := a.activeConn.Members
	a.activeConn.mu.RUnlock()

	// Filter by role
	var filtered []*MemberDisplay
	for _, member := range allMembers {
		// Role filter
		if s.FilterRole != "All" && s.FilterRole != "" {
			if member.HighestRole == nil || member.HighestRole.Name != s.FilterRole {
				continue
			}
		}

		// Status filter
		if s.FilterOnline != "all" && member.User != nil {
			if s.FilterOnline == "online" && member.User.Status != models.StatusOnline {
				continue
			}
			if s.FilterOnline == "offline" && member.User.Status == models.StatusOnline {
				continue
			}
		}

		// Search filter
		if s.SearchQuery != "" && member.User != nil {
			if !strings.Contains(strings.ToLower(member.User.Username), strings.ToLower(s.SearchQuery)) {
				continue
			}
		}

		filtered = append(filtered, member)
	}

	// Sort
	switch s.SortBy {
	case "name":
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].User == nil || filtered[j].User == nil {
				return false
			}
			return strings.ToLower(filtered[i].User.Username) < strings.ToLower(filtered[j].User.Username)
		})

	case "joined":
		sort.Slice(filtered, func(i, j int) bool {
			if filtered[i].Member == nil || filtered[j].Member == nil {
				return false
			}
			return filtered[i].Member.JoinedAt.Before(filtered[j].Member.JoinedAt)
		})

	case "role":
		sort.Slice(filtered, func(i, j int) bool {
			// Sort by role position (higher position = higher in list)
			posI := 0
			posJ := 0
			if filtered[i].HighestRole != nil {
				posI = filtered[i].HighestRole.Position
			}
			if filtered[j].HighestRole != nil {
				posJ = filtered[j].HighestRole.Position
			}
			return posI > posJ
		})

	case "kicks":
		sort.Slice(filtered, func(i, j int) bool {
			return filtered[i].KickCount > filtered[j].KickCount
		})

	default:
		// Default to role sorting
		sort.Slice(filtered, func(i, j int) bool {
			posI := 0
			posJ := 0
			if filtered[i].HighestRole != nil {
				posI = filtered[i].HighestRole.Position
			}
			if filtered[j].HighestRole != nil {
				posJ = filtered[j].HighestRole.Position
			}
			return posI > posJ
		})
	}

	s.MemberList = filtered

	// Reset selection if out of bounds
	if s.SelectedMember >= len(s.MemberList) {
		s.SelectedMember = len(s.MemberList) - 1
	}
	if s.SelectedMember < 0 && len(s.MemberList) > 0 {
		s.SelectedMember = 0
	}
}

// loadRetentionPolicyForManagement sends OpGetRetentionPolicy to load the server default + all overrides
func (a *App) loadRetentionPolicyForManagement(serverID uuid.UUID) {
	if a.activeConn == nil {
		return
	}
	req := &protocol.GetRetentionPolicyRequest{
		ServerID: serverID,
	}
	msg, err := protocol.NewMessage(protocol.OpGetRetentionPolicy, req)
	if err == nil {
		_ = a.activeConn.Connection.Send(msg)
	}
}

// loadPluginListForManagement requests the installed plugin list + config
// for the Plugins category (Settings > Plugins). The response arrives async
// as EventPluginConfigUpdate — see app.go's dispatch handling.
func (a *App) loadPluginListForManagement(serverID uuid.UUID) {
	if a.activeConn == nil {
		return
	}
	req := &protocol.PluginConfigGetRequest{ServerID: serverID}
	msg, err := protocol.NewMessage(protocol.OpPluginConfigGet, req)
	if err == nil {
		_ = a.activeConn.Connection.Send(msg)
	}
}

// Permission list item for rendering
type permissionItem struct {
	Name     string
	Bit      models.Permission
	Category string
}

// getOverwriteablePermissionList returns the permissions a channel
// overwrite editor should expose. Deliberately a small subset of
// getPermissionList(), not all of it: PermissionCalculator.ComputeOverwrites
// is correct for every bit, but server-side enforcement (hasChannelPermission
// in internal/server/handlers.go) only actually checks PermissionSendMessages
// today — showing togglable overwrites for permissions nothing enforces
// would be a UI promise Concord can't keep. Add an entry here the same
// session real enforcement lands for that permission, not before.
func getOverwriteablePermissionList() []permissionItem {
	return []permissionItem{
		{Name: "Send Messages", Bit: models.PermissionSendMessages, Category: "Text Channels"},
	}
}

// getPermissionList returns all permissions organized by category
func getPermissionList() []permissionItem {
	return []permissionItem{
		// General Permissions
		{Name: "View Channels", Bit: models.PermissionViewChannels, Category: "General"},
		{Name: "Manage Channels", Bit: models.PermissionManageChannels, Category: "General"},
		{Name: "Manage Roles", Bit: models.PermissionManageRoles, Category: "General"},
		{Name: "Manage Server", Bit: models.PermissionManageServer, Category: "General"},
		{Name: "Create Invite", Bit: models.PermissionCreateInvite, Category: "General"},
		{Name: "Kick Members", Bit: models.PermissionKickMembers, Category: "General"},
		{Name: "Ban Members", Bit: models.PermissionBanMembers, Category: "General"},
		{Name: "Change Nickname", Bit: models.PermissionChangeNickname, Category: "General"},
		{Name: "Manage Titles", Bit: models.PermissionManageNicknames, Category: "General"},

		// Text Channel Permissions
		{Name: "Send Messages", Bit: models.PermissionSendMessages, Category: "Text Channels"},
		{Name: "Send Messages in Threads", Bit: models.PermissionSendMessagesThreads, Category: "Text Channels"},
		{Name: "Create Threads", Bit: models.PermissionCreateThreads, Category: "Text Channels"},
		{Name: "Embed Links", Bit: models.PermissionEmbedLinks, Category: "Text Channels"},
		{Name: "Attach Files", Bit: models.PermissionAttachFiles, Category: "Text Channels"},
		{Name: "Add Reactions", Bit: models.PermissionAddReactions, Category: "Text Channels"},
		{Name: "Use External Emoji", Bit: models.PermissionUseExternalEmoji, Category: "Text Channels"},
		{Name: "Mention Everyone", Bit: models.PermissionMentionEveryone, Category: "Text Channels"},
		{Name: "Manage Messages", Bit: models.PermissionManageMessages, Category: "Text Channels"},
		{Name: "Read Message History", Bit: models.PermissionReadMessageHistory, Category: "Text Channels"},
		{Name: "Pin Messages", Bit: models.PermissionPinMessages, Category: "Text Channels"},

		// Voice Channel Permissions
		{Name: "Connect", Bit: models.PermissionConnect, Category: "Voice Channels"},
		{Name: "Speak", Bit: models.PermissionSpeak, Category: "Voice Channels"},
		{Name: "Mute Members", Bit: models.PermissionMuteMembers, Category: "Voice Channels"},
		{Name: "Deafen Members", Bit: models.PermissionDeafenMembers, Category: "Voice Channels"},
		{Name: "Move Members", Bit: models.PermissionMoveMembers, Category: "Voice Channels"},
		{Name: "Use Voice Activity", Bit: models.PermissionUseVAD, Category: "Voice Channels"},

		// Administrator (Special)
		{Name: "Administrator (All Permissions)", Bit: models.PermissionAdministrator, Category: "Special"},
	}
}

// getPermissionDescription returns the description and command examples for a permission
func getPermissionDescription(permName string) (description string, commands string) {
	descriptions := map[string]struct {
		desc string
		cmds string
	}{
		// General Permissions
		"View Channels": {
			desc: "See channels and categories in the server",
			cmds: "Implicit - all members have this by default",
		},
		"Manage Channels": {
			desc: "Create, edit, delete, and reorder channels and categories",
			cmds: "/create-channel <name> [category], /delete-channel <name>, /rename-channel <old> <new>, /move-channel <channel> <category>",
		},
		"Manage Roles": {
			desc: "Create, edit, delete roles and assign or remove roles from members",
			cmds: "/roles, /create-role <name> [preset], /role assign|remove @user <role>",
		},
		"Manage Server": {
			desc: "Change server name, settings, retention policies (Not Implemented)",
			cmds: "Server settings UI - future feature",
		},
		"Create Invite": {
			desc: "Generate invite links for others to join the server (Not Implemented)",
			cmds: "/invite create - future feature",
		},
		"Kick Members": {
			desc: "Remove members from the server (they can rejoin)",
			cmds: "/kick @user [reason]",
		},
		"Ban Members": {
			desc: "Permanently ban members from the server",
			cmds: "/ban @user [reason], /unban @user",
		},
		"Change Nickname": {
			desc: "Change your own title in this server (Not Implemented)",
			cmds: "/title <new title> - future feature",
		},
		"Manage Titles": {
			desc: "Change other members' titles",
			cmds: "/title @user <new title>",
		},

		// Text Channel Permissions
		"Send Messages": {
			desc: "Send text messages in channels",
			cmds: "Implicit - all members can send messages",
		},
		"Send Messages in Threads": {
			desc: "Reply to threads (Not Implemented)",
			cmds: "Thread system - future feature",
		},
		"Create Threads": {
			desc: "Create discussion threads from messages (Not Implemented)",
			cmds: "Thread system - future feature",
		},
		"Embed Links": {
			desc: "Post URLs that auto-embed (images, videos, etc.)",
			cmds: "Implicit - URL rendering is automatic (OSC 8 hyperlinks)",
		},
		"Attach Files": {
			desc: "Upload files/images to messages (Not Implemented)",
			cmds: "File upload system - future feature",
		},
		"Add Reactions": {
			desc: "React to messages with emoji (Not Implemented)",
			cmds: "Reaction system - future feature",
		},
		"Use External Emoji": {
			desc: "Use emoji from other servers (Not Implemented)",
			cmds: "Custom emoji system - future feature",
		},
		"Mention Everyone": {
			desc: "Use @everyone and @here to ping all members",
			cmds: "@everyone, @here in messages",
		},
		"Manage Messages": {
			desc: "Delete any message, pin/unpin messages",
			cmds: "/delete-message <id>, /pin <message-id>, /unpin <message-id>",
		},
		"Read Message History": {
			desc: "View past messages in channels (scrollback)",
			cmds: "Implicit - message history is always accessible",
		},
		"Pin Messages": {
			desc: "Pin important messages to channel header",
			cmds: "/pin <message-id>, /unpin <message-id>",
		},

		// Voice Channel Permissions
		"Connect": {
			desc: "Join voice channels (Not Implemented)",
			cmds: "Voice system - future feature",
		},
		"Speak": {
			desc: "Transmit audio in voice channels (Not Implemented)",
			cmds: "Voice system - future feature",
		},
		"Mute Members": {
			desc: "Server-mute members (text mute only; voice mute not implemented)",
			cmds: "/mute @user [duration] [reason]",
		},
		"Deafen Members": {
			desc: "Server-deafen members (they can't hear) (Not Implemented)",
			cmds: "Voice system - future feature",
		},
		"Move Members": {
			desc: "Move members between voice channels (Not Implemented)",
			cmds: "Voice system - future feature",
		},
		"Use Voice Activity": {
			desc: "Use voice activity detection instead of push-to-talk (Not Implemented)",
			cmds: "Voice system - future feature",
		},

		// Special
		"Administrator (All Permissions)": {
			desc: "Grants all permissions automatically (including future ones)",
			cmds: "Server owner always has this; role assignment via /role assign @user Admin",
		},
	}

	if info, exists := descriptions[permName]; exists {
		return info.desc, info.cmds
	}
	return "No description available", ""
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
	if s.OverwriteEditorOpen {
		return a.handleOverwriteEditorKey(msg)
	}
	if s.OverwriteTargetPicker {
		return a.handleOverwriteTargetPickerKey(msg)
	}
	if s.DeleteConfirmOpen {
		return a.handleDeleteConfirmKey(msg)
	}
	if s.MoveDialogOpen {
		return a.handleMoveDialogKey(msg)
	}
	if s.RoleAssignOpen {
		return a.handleRoleAssignKey(msg)
	}
	if s.KickConfirmOpen {
		return a.handleKickConfirmKey(msg)
	}
	if s.BanConfirmOpen {
		return a.handleBanConfirmKey(msg)
	}
	if s.UnmuteConfirmOpen {
		return a.handleUnmuteConfirmKey(msg)
	}
	if s.MuteDurationOpen {
		return a.handleMuteDurationKey(msg)
	}
	if s.FilterPanelOpen {
		return a.handleFilterPanelKey(msg)
	}
	if s.SearchInputOpen {
		return a.handleSearchInputKey(msg)
	}
	if s.RemoveExemptPickerOpen {
		return a.handleRemoveExemptKey(msg)
	}
	if s.OverrideChannelPickerOpen {
		return a.handleChannelPickerKey(msg)
	}
	if s.RetentionFormState != nil {
		return a.handleRetentionFormKey(msg)
	}
	if s.PruneConfirmOpen {
		return a.handlePruneConfirmKey(msg)
	}
	if s.PluginConfigState != nil {
		return a.handlePluginConfigKey(msg)
	}

	switch msg.String() {
	case "esc":
		// Return to previous view (with slide-out animation if enabled)
		if a.uiConfig != nil && a.uiConfig.Display.DisablePanelAnimations {
			returnTo := s.PreviousView
			a.serverManagementState = nil
			a.view = returnTo
			return nil
		}
		a.srvMgmtAnimClosing = true
		a.srvMgmtAnimating = true
		return srvMgmtPanelAnimTick()

	// IMPORTANT: Shift+up/down must be BEFORE regular up/down to take priority
	case "shift+up":
		// Reorder up (Channels only) - only when focused on content
		if s.FocusOnForm && s.SelectedCategory == 0 {
			a.handleReorderUp()
		}

	case "shift+down":
		// Reorder down (Channels only) - only when focused on content
		if s.FocusOnForm && s.SelectedCategory == 0 {
			a.handleReorderDown()
		}

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
		} else if s.SelectedCategory == 4 {
			a.handleOpenPluginConfigAction()
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

	case "m":
		// Move action (Channels only) - lowercase m
		if s.FocusOnForm && s.SelectedCategory == 0 {
			a.handleMoveChannelAction()
		}

	case "p", "P":
		// Permission overwrites (Channels), permissions editor (Roles), or Prune (Messages)
		if s.FocusOnForm {
			if s.SelectedCategory == 0 {
				a.handleChannelOverwritesAction()
			} else if s.SelectedCategory == 1 {
				a.handlePermissionsAction()
			} else if s.SelectedCategory == 3 {
				a.handlePruneAction()
			}
		}

	case "R":
		// Assign role (Members) - Shift+R
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleAssignRoleAction()
		}

	case "K":
		// Kick member (Members) - Shift+K
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleKickMemberAction()
		}

	case "B":
		// Ban/Unban member toggle (Members) - Shift+B
		if s.FocusOnForm && s.SelectedCategory == 2 {
			a.handleBanMemberAction()
		}

	case "f", "F":
		// Open filters (Members)
		if s.FocusOnForm && s.SelectedCategory == 2 {
			s.FilterPanelOpen = true
			s.FilterPanelFocus = 0
		}

	case "M":
		// Mute/Unmute toggle (Members) - Shift+M
		if s.FocusOnForm && s.SelectedCategory == 2 {
			// Check if selected member is muted
			if len(s.MemberList) > 0 && s.SelectedMember >= 0 && s.SelectedMember < len(s.MemberList) {
				member := s.MemberList[s.SelectedMember]
				if member != nil && member.IsMuted {
					a.handleUnmuteMemberAction()
				} else {
					a.handleMuteMemberAction()
				}
			}
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

	case "t", "T":
		// Toggle enabled/disabled (Plugins)
		if s.FocusOnForm && s.SelectedCategory == 4 {
			a.handleTogglePluginAction()
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
	case 4: // Plugins
		a.loadPluginListForManagement(serverID)
		a.serverManagementState.SelectedPlugin = 0
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
	case 4: // Plugins
		if s.SelectedPlugin > 0 {
			s.SelectedPlugin--
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
	case 4: // Plugins
		if s.SelectedPlugin < len(s.PluginList)-1 {
			s.SelectedPlugin++
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

		maxUsersInput := textinput.New()
		maxUsersInput.Placeholder = "0"
		maxUsersInput.CharLimit = 5
		maxUsersInput.Width = 10

		s.ChannelFormOpen = true
		s.ChannelFormState = &ChannelFormState{
			Mode:          "create",
			NameTextInput: nameInput,
			TypeIndex:     0,
			MaxUsersInput: maxUsersInput,
			CategoryID:    parentCategoryID,
			FocusField:    0,
		}
	case 1: // Create Role
		s.RoleFormOpen = true
		s.RoleFormState = &RoleFormState{
			Mode:          "create",
			PresetIndex:   0,
			ColorIndex:    0,
			IsHoisted:     true,  // Default to showing separately in members list
			IsMentionable: true,  // Default to allowing @mentions
			FocusField:    0,
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

			// Determine type index: 0=Text, 1=Voice, 2=Category. Plugin
			// channels keep TypeIndex 0 internally but are never editable to
			// a different type (see OriginalType below) — this number is
			// otherwise unused for them.
			typeIndex := 0
			switch selectedCh.Type {
			case models.ChannelTypeVoice:
				typeIndex = 1
			case models.ChannelTypeCategory:
				typeIndex = 2
			}

			pluginLabel := ""
			var pluginID, pluginKind string
			var pluginFields []protocol.PluginField
			var pluginTextInputs []textinput.Model
			var pluginValues []string
			if selectedCh.Type == models.ChannelTypePlugin {
				pluginLabel = a.channelTypeLabel(selectedCh)
				if info, ok := a.pluginChannelKind(selectedCh); ok {
					pluginID = info.PluginID
					pluginKind = info.Kind
					pluginFields = info.CreateFields
					pluginTextInputs, pluginValues = buildFieldEditors(info.CreateFields, selectedCh.PluginConfig)
				}
			}

			// MaxUsers input
			maxUsersInput := textinput.New()
			maxUsersInput.SetValue(fmt.Sprintf("%d", selectedCh.MaxUsers))
			maxUsersInput.CharLimit = 5
			maxUsersInput.Width = 10

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
				Mode:               "edit",
				EditingChannelID:   &chID,
				NameTextInput:      nameInput,
				TypeIndex:          typeIndex,
				MaxUsersInput:      maxUsersInput,
				CategoryID:         catID,
				FocusField:         0,
				OriginalType:       selectedCh.Type,
				PluginDisplayLabel: pluginLabel,
				PluginID:           pluginID,
				PluginKind:         pluginKind,
				PluginFields:       pluginFields,
				PluginTextInputs:   pluginTextInputs,
				PluginValues:       pluginValues,
			}
		}
	case 1: // Edit Role
		if s.SelectedRole >= 0 && s.SelectedRole < len(s.RoleList) {
			role := s.RoleList[s.SelectedRole]

			// Map color RGB value back to index
			colorIndex := 0 // Default to Gold
			colorValues := map[int]int{
				0xFFD700: 0, // Gold
				0x5865F2: 1, // Blue
				0xED4245: 2, // Red
				0x57F287: 3, // Green
				0x9B59B6: 4, // Purple
			}
			if idx, ok := colorValues[role.Color]; ok {
				colorIndex = idx
			}

			// Detect preset based on permissions (or default to Custom)
			presetIndex := 3 // Custom
			if role.Permissions == models.PermissionsText {
				presetIndex = 0 // Members
			} else if role.Permissions == models.PermissionsModerator {
				presetIndex = 1 // Moderator
			} else if role.Permissions == models.PermissionsAdmin {
				presetIndex = 2 // Admin
			}

			s.RoleFormOpen = true
			s.RoleFormState = &RoleFormState{
				Mode:               "edit",
				EditingRoleID:      &role.ID,
				NameInput:          role.Name,
				NameCursor:         len(role.Name),
				ColorIndex:         colorIndex,
				PresetIndex:        presetIndex,
				IsHoisted:          role.IsHoisted,
				IsMentionable:      role.IsMentionable,
				DisplayOrder:       fmt.Sprintf("%d", role.DisplayOrder),
				DisplayOrderCursor: len(fmt.Sprintf("%d", role.DisplayOrder)),
				FocusField:         0,
			}
		}
	case 3: // Edit retention policy — pre-fill with current values
		// A focused row in the Exempt Channels list edits that channel's own
		// override instead of the server default -- otherwise E always fell
		// through to the server-default policy even with a specific channel
		// highlighted, with no way to set a custom per-channel value at all
		// (only full exemption via N).
		if len(s.ChannelOverrides) > 0 && s.SelectedOverride >= 0 && s.SelectedOverride < len(s.ChannelOverrides) {
			override := s.ChannelOverrides[s.SelectedOverride]
			form := &RetentionFormState{
				Mode:       "channel",
				ChannelID:  override.ChannelID,
				FocusField: 0,
			}
			if override.TimeRetentionDays != nil {
				form.TimeRetentionDays = strconv.Itoa(*override.TimeRetentionDays)
			}
			if override.SystemTimeRetentionDays != nil {
				form.SystemTimeRetentionDays = strconv.Itoa(*override.SystemTimeRetentionDays)
			}
			if override.MaxMessageCount != nil {
				form.MaxMessageCount = strconv.Itoa(*override.MaxMessageCount)
			}
			s.RetentionFormState = form
			return
		}

		form := &RetentionFormState{
			Mode:       "server",
			FocusField: 0,
		}
		if s.RetentionPolicy != nil {
			if s.RetentionPolicy.TimeRetentionDays != nil {
				form.TimeRetentionDays = strconv.Itoa(*s.RetentionPolicy.TimeRetentionDays)
			}
			if s.RetentionPolicy.SystemTimeRetentionDays != nil {
				form.SystemTimeRetentionDays = strconv.Itoa(*s.RetentionPolicy.SystemTimeRetentionDays)
			}
			if s.RetentionPolicy.MaxMessageCount != nil {
				form.MaxMessageCount = strconv.Itoa(*s.RetentionPolicy.MaxMessageCount)
			}
		}
		s.RetentionFormState = form
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
			s.DeleteConfirmFocusedButton = 0 // Start with "Yes, Delete" focused
		}
	case 1: // Delete Role
		if s.SelectedRole >= 0 && s.SelectedRole < len(s.RoleList) {
			role := s.RoleList[s.SelectedRole]
			// Don't allow deleting @everyone role
			if role.Name != "@everyone" && role.Name != "everyone" {
				s.DeleteConfirmOpen = true
				s.DeleteConfirmRole = role
				s.DeleteConfirmFocusedButton = 0 // Start with "Yes, Delete" focused
			}
		}
	case 3: // Open the remove-exempt picker
		if len(s.ChannelOverrides) == 0 {
			a.statusMessage = "No exempt channels to remove"
			return
		}
		s.RemoveExemptPickerOpen = true
		s.RemoveExemptSelected = s.SelectedOverride // pre-select the currently highlighted row
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

// handleChannelOverwritesAction opens the permission-overwrite target picker
// for the currently-selected channel in the Channels category. Categories
// are skipped — Concord's overwrite system has no cascade-to-children
// concept (ComputeOverwrites reads one channel's own PermissionOverwrites
// only), so an overwrite on a category wouldn't do anything.
func (a *App) handleChannelOverwritesAction() {
	s := a.serverManagementState
	if s.SelectedChannel < 0 || s.SelectedChannel >= len(s.ChannelList) {
		return
	}
	channel := s.ChannelList[s.SelectedChannel]
	if channel.Type == models.ChannelTypeCategory {
		a.statusMessage = "Categories don't have their own permissions — set overwrites on individual channels"
		return
	}

	serverID := a.getActiveServerID()
	a.loadRoleListForManagement(serverID)
	a.loadMemberListForManagement()

	s.OverwriteChannel = channel
	s.OverwriteTargetPicker = true
	s.OverwriteTargetIndex = 0
}

// overwriteTargetItem is one row in the role/member target picker.
type overwriteTargetItem struct {
	ID           uuid.UUID
	Name         string
	Type         string // "role" or "member"
	HasOverwrite bool
}

// overwriteTargets lists every role, then every member, for the channel
// currently being edited, flagging which ones already have a stored
// overwrite so the picker can show that at a glance.
func (a *App) overwriteTargets() []overwriteTargetItem {
	s := a.serverManagementState
	if s.OverwriteChannel == nil {
		return nil
	}

	existing := make(map[uuid.UUID]bool, len(s.OverwriteChannel.PermissionOverwrites))
	for _, ow := range s.OverwriteChannel.PermissionOverwrites {
		existing[ow.ID] = true
	}

	items := make([]overwriteTargetItem, 0, len(s.RoleList)+len(s.MemberList))
	for _, role := range s.RoleList {
		name := role.Name
		if role.IsDefault {
			name = "@everyone"
		}
		items = append(items, overwriteTargetItem{ID: role.ID, Name: name, Type: "role", HasOverwrite: existing[role.ID]})
	}
	for _, member := range s.MemberList {
		if member == nil || member.User == nil {
			continue
		}
		items = append(items, overwriteTargetItem{ID: member.User.ID, Name: member.User.Username, Type: "member", HasOverwrite: existing[member.User.ID]})
	}
	return items
}

// findChannelOverwrite returns the existing overwrite for a target on the
// channel currently being edited, if any.
func (a *App) findChannelOverwrite(targetID uuid.UUID) (models.PermissionOverwrite, bool) {
	s := a.serverManagementState
	if s.OverwriteChannel == nil {
		return models.PermissionOverwrite{}, false
	}
	for _, ow := range s.OverwriteChannel.PermissionOverwrites {
		if ow.ID == targetID {
			return ow, true
		}
	}
	return models.PermissionOverwrite{}, false
}

func (a *App) handleReorderUp() {
	s := a.serverManagementState

	// Handle Channels category (index 0)
	if s.SelectedCategory == 0 {
		if s.SelectedChannel < 0 || s.SelectedChannel >= len(s.ChannelList) {
			return
		}

		currentChannel := s.ChannelList[s.SelectedChannel]

		// Find siblings (channels at same level: same CategoryID) with their indices
		type sibling struct {
			channel *models.Channel
			index   int
		}
		var siblings []sibling
		for i, ch := range s.ChannelList {
			// Categories swap with categories; channels within same category swap together
			if ch.Type == models.ChannelTypeCategory && currentChannel.Type == models.ChannelTypeCategory {
				siblings = append(siblings, sibling{ch, i})
			} else if ch.Type != models.ChannelTypeCategory && currentChannel.Type != models.ChannelTypeCategory {
				// Both are regular channels - check if they share the same category
				if ch.CategoryID == currentChannel.CategoryID {
					siblings = append(siblings, sibling{ch, i})
				}
			}
		}

		if len(siblings) < 2 {
			return // Nothing to swap with
		}

		// Find current position in siblings
		currentIdx := -1
		for i, sib := range siblings {
			if sib.channel.ID == currentChannel.ID {
				currentIdx = i
				break
			}
		}

		if currentIdx <= 0 {
			return // Already at top
		}

		// Normalize sibling SortOrder to distinct values (i*10) before swapping,
		// so equal-SortOrder channels (all new channels default to 0) still sort correctly.
		for i, sib := range siblings {
			sib.channel.SortOrder = i * 10
		}

		// Get target (previous sibling)
		targetSib := siblings[currentIdx-1]
		targetChannel := targetSib.channel

		// Swap SortOrder values
		currentChannel.SortOrder, targetChannel.SortOrder = targetChannel.SortOrder, currentChannel.SortOrder

		// Send protocol messages to persist changes
		serverID := a.getActiveServerID()
		if a.activeConn != nil && serverID != uuid.Nil {
			req1 := &protocol.ChannelUpdateRequest{
				ServerID:  serverID,
				ChannelID: currentChannel.ID,
				SortOrder: &currentChannel.SortOrder,
			}
			if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req1); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			req2 := &protocol.ChannelUpdateRequest{
				ServerID:  serverID,
				ChannelID: targetChannel.ID,
				SortOrder: &targetChannel.SortOrder,
			}
			if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req2); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			// Reload the channel list to reflect the new order
			a.loadChannelListForManagement(serverID)
			// Rebuild channel tree so main view shows updated order
			a.loadChannelTree()
			// Update selection to find where the moved channel ended up
			for i, ch := range s.ChannelList {
				if ch.ID == currentChannel.ID {
					s.SelectedChannel = i
					break
				}
			}
		}
		return
	}

	// Roles category (index 1) does not support reordering - roles are sorted alphabetically
}

func (a *App) handleReorderDown() {
	s := a.serverManagementState

	// Handle Channels category (index 0)
	if s.SelectedCategory == 0 {
		if s.SelectedChannel < 0 || s.SelectedChannel >= len(s.ChannelList) {
			return
		}

		currentChannel := s.ChannelList[s.SelectedChannel]

		// Find siblings (channels at same level: same CategoryID) with their indices
		type sibling struct {
			channel *models.Channel
			index   int
		}
		var siblings []sibling
		for i, ch := range s.ChannelList {
			// Categories swap with categories; channels within same category swap together
			if ch.Type == models.ChannelTypeCategory && currentChannel.Type == models.ChannelTypeCategory {
				siblings = append(siblings, sibling{ch, i})
			} else if ch.Type != models.ChannelTypeCategory && currentChannel.Type != models.ChannelTypeCategory {
				// Both are regular channels - check if they share the same category
				if ch.CategoryID == currentChannel.CategoryID {
					siblings = append(siblings, sibling{ch, i})
				}
			}
		}

		if len(siblings) < 2 {
			return // Nothing to swap with
		}

		// Find current position in siblings
		currentIdx := -1
		for i, sib := range siblings {
			if sib.channel.ID == currentChannel.ID {
				currentIdx = i
				break
			}
		}

		if currentIdx < 0 || currentIdx >= len(siblings)-1 {
			return // Already at bottom
		}

		// Normalize sibling SortOrder to distinct values (i*10) before swapping,
		// so equal-SortOrder channels (all new channels default to 0) still sort correctly.
		for i, sib := range siblings {
			sib.channel.SortOrder = i * 10
		}

		// Get target (next sibling)
		targetSib := siblings[currentIdx+1]
		targetChannel := targetSib.channel

		// Swap SortOrder values
		currentChannel.SortOrder, targetChannel.SortOrder = targetChannel.SortOrder, currentChannel.SortOrder

		// Send protocol messages to persist changes
		serverID := a.getActiveServerID()
		if a.activeConn != nil && serverID != uuid.Nil {
			req1 := &protocol.ChannelUpdateRequest{
				ServerID:  serverID,
				ChannelID: currentChannel.ID,
				SortOrder: &currentChannel.SortOrder,
			}
			if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req1); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			req2 := &protocol.ChannelUpdateRequest{
				ServerID:  serverID,
				ChannelID: targetChannel.ID,
				SortOrder: &targetChannel.SortOrder,
			}
			if msg, err := protocol.NewMessage(protocol.OpChannelUpdate, req2); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			// Reload the channel list to reflect the new order
			a.loadChannelListForManagement(serverID)
			// Rebuild channel tree so main view shows updated order
			a.loadChannelTree()
			// Update selection to find where the moved channel ended up
			for i, ch := range s.ChannelList {
				if ch.ID == currentChannel.ID {
					s.SelectedChannel = i
					break
				}
			}
		}
		return
	}

	// Handle Roles category (index 1)
	if s.SelectedCategory == 1 {
		if s.SelectedRole < 0 || s.SelectedRole >= len(s.RoleList) {
			return
		}
		if len(s.RoleList) < 2 {
			return // Nothing to swap with
		}
		if s.SelectedRole >= len(s.RoleList)-1 {
			return // Already at bottom
		}

		currentRole := s.RoleList[s.SelectedRole]
		targetRole := s.RoleList[s.SelectedRole+1]

		// Don't allow moving @everyone role itself (but allow other roles to swap past it)
		if currentRole.IsDefault {
			return
		}

		// Normalize DisplayOrder to distinct values (i*10) before swapping
		for i, role := range s.RoleList {
			role.DisplayOrder = i * 10
		}

		// Swap DisplayOrder values
		currentRole.DisplayOrder, targetRole.DisplayOrder = targetRole.DisplayOrder, currentRole.DisplayOrder

		// Update selection (moved down by 1)
		s.SelectedRole++

		// Send protocol messages to persist changes
		serverID := a.getActiveServerID()
		channelID := uuid.Nil
		if a.currentChannel != nil {
			channelID = a.currentChannel.ID
		}
		if a.activeConn != nil && serverID != uuid.Nil {
			req1 := &protocol.UpdateRoleRequest{
				ServerID:      serverID,
				ChannelID:     channelID,
				RoleID:        currentRole.ID,
				Name:          currentRole.Name,
				Permissions:   uint64(currentRole.Permissions),
				Color:         currentRole.Color,
				DisplayOrder:  &currentRole.DisplayOrder,
				IsHoisted:     currentRole.IsHoisted,
				IsMentionable: currentRole.IsMentionable,
			}
			if msg, err := protocol.NewMessage(protocol.OpUpdateRole, req1); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			req2 := &protocol.UpdateRoleRequest{
				ServerID:      serverID,
				ChannelID:     channelID,
				RoleID:        targetRole.ID,
				Name:          targetRole.Name,
				Permissions:   uint64(targetRole.Permissions),
				Color:         targetRole.Color,
				DisplayOrder:  &targetRole.DisplayOrder,
				IsHoisted:     targetRole.IsHoisted,
				IsMentionable: targetRole.IsMentionable,
			}
			if msg, err := protocol.NewMessage(protocol.OpUpdateRole, req2); err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}

			// Reload the role list to reflect the new order
			a.loadRoleListForManagement(serverID)
			// Update selection to find where the moved role ended up
			for i, role := range s.RoleList {
				if role.ID == currentRole.ID {
					s.SelectedRole = i
					break
				}
			}
		}
		return
	}
}

func (a *App) handleAssignRoleAction() {
	s := a.serverManagementState
	if s == nil || len(s.MemberList) == 0 || s.SelectedMember < 0 || s.SelectedMember >= len(s.MemberList) {
		return
	}

	member := s.MemberList[s.SelectedMember]
	if member == nil || member.Member == nil {
		return
	}

	// Initialize role assignment state
	s.RoleAssignOpen = true
	s.RoleAssignMember = member
	s.RoleAssignSelections = make(map[uuid.UUID]bool)
	s.RoleAssignFocus = 0

	// Pre-populate with member's current roles
	if member.Member.RoleIDs != nil {
		for _, roleID := range member.Member.RoleIDs {
			s.RoleAssignSelections[roleID] = true
		}
	}
}

func (a *App) handleKickMemberAction() {
	s := a.serverManagementState
	if s == nil || len(s.MemberList) == 0 || s.SelectedMember < 0 || s.SelectedMember >= len(s.MemberList) {
		return
	}

	member := s.MemberList[s.SelectedMember]
	if member == nil || member.User == nil {
		return
	}

	s.KickConfirmOpen = true
	s.KickConfirmMember = member
	s.KickConfirmFocusedBtn = 0
}

func (a *App) handleBanMemberAction() {
	s := a.serverManagementState
	if s == nil || len(s.MemberList) == 0 || s.SelectedMember < 0 || s.SelectedMember >= len(s.MemberList) {
		return
	}

	member := s.MemberList[s.SelectedMember]
	if member == nil || member.User == nil {
		return
	}

	s.BanConfirmOpen = true
	s.BanConfirmMember = member
	s.BanConfirmFocusedBtn = 0
}

func (a *App) handleMuteMemberAction() {
	s := a.serverManagementState
	if s == nil || len(s.MemberList) == 0 || s.SelectedMember < 0 || s.SelectedMember >= len(s.MemberList) {
		return
	}

	member := s.MemberList[s.SelectedMember]
	if member == nil || member.User == nil {
		return
	}

	s.MuteDurationOpen = true
	s.MuteDurationMember = member
	s.MuteDurationFocus = 0
	s.MuteDurationCustom = ""
	s.MuteDurationReason = ""
}

func (a *App) handleUnmuteMemberAction() {
	s := a.serverManagementState
	if s == nil || len(s.MemberList) == 0 || s.SelectedMember < 0 || s.SelectedMember >= len(s.MemberList) {
		return
	}

	member := s.MemberList[s.SelectedMember]
	if member == nil || member.User == nil {
		return
	}

	s.UnmuteConfirmOpen = true
	s.UnmuteConfirmMember = member
	s.UnmuteConfirmFocusedBtn = 0
}

func (a *App) handlePruneAction() {
	s := a.serverManagementState
	s.PruneConfirmOpen = true
}

func (a *App) handleCreateChannelOverride() {
	s := a.serverManagementState
	if a.activeConn == nil {
		return
	}
	serverID := a.getActiveServerID()

	a.activeConn.mu.RLock()
	channels, ok := a.activeConn.Channels[serverID]
	a.activeConn.mu.RUnlock()
	if !ok {
		return
	}

	// Build set of already-exempt channel IDs
	exemptIDs := make(map[uuid.UUID]bool)
	for _, ov := range s.ChannelOverrides {
		if ov.ChannelID != nil {
			exemptIDs[*ov.ChannelID] = true
		}
	}

	// Collect text channels not already exempt
	var textChannels []*models.Channel
	for _, ch := range channels {
		if ch.Type == models.ChannelTypeText && !exemptIDs[ch.ID] {
			textChannels = append(textChannels, ch)
		}
	}
	if len(textChannels) == 0 {
		a.statusMessage = "All text channels are already exempt"
		return
	}
	sort.Slice(textChannels, func(i, j int) bool {
		return textChannels[i].Name < textChannels[j].Name
	})

	s.OverrideChannelPickerOpen = true
	s.OverrideChannelList = textChannels
	s.OverrideChannelSelected = 0
}

// Placeholder key handlers for forms/dialogs
func (a *App) handleChannelFormKey(msg tea.KeyMsg) tea.Cmd {
	state := a.serverManagementState.ChannelFormState
	layout := computeChannelFormLayout(state)

	blurAll := func() {
		state.NameTextInput.Blur()
		state.MaxUsersInput.Blur()
		for i := range state.PluginTextInputs {
			state.PluginTextInputs[i].Blur()
		}
	}
	focusField := func(field int) {
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

	switch msg.String() {
	case "esc":
		a.serverManagementState.ChannelFormOpen = false
		a.serverManagementState.ChannelFormState = nil
		return nil

	case "tab":
		state.FocusField = (state.FocusField + 1) % (layout.cancelField + 1)
		blurAll()
		focusField(state.FocusField)
		return nil

	case "shift+tab":
		state.FocusField--
		if state.FocusField < 0 {
			state.FocusField = layout.cancelField
		}
		blurAll()
		focusField(state.FocusField)
		return nil

	case "up", "down", "left", "right":
		dir := 1
		if msg.String() == "up" || msg.String() == "left" {
			dir = -1
		}
		pluginTypeLocked := state.Mode == "edit" && state.OriginalType == models.ChannelTypePlugin
		if state.FocusField == 1 && !pluginTypeLocked {
			// Cycle through built-in types (Text/Voice/Category) then every
			// plugin-provided kind advertised at READY.
			pluginKinds := a.pluginKindOptions()
			total := 3 + len(pluginKinds)
			state.TypeIndex = (state.TypeIndex + dir + total) % total
			if state.TypeIndex >= 3 {
				setPluginKind(state, pluginKinds[state.TypeIndex-3])
			} else {
				state.PluginFields = nil
				state.PluginTextInputs = nil
				state.PluginValues = nil
			}
		} else if layout.isPlugin && state.FocusField >= layout.pluginStart && state.FocusField < layout.pluginStart+len(state.PluginFields) {
			idx := state.FocusField - layout.pluginStart
			field := state.PluginFields[idx]
			if field.Type != "text" && field.Type != "number" {
				state.PluginValues[idx] = cyclePluginFieldValue(field, state.PluginValues[idx], dir, a.textChannelNames())
			}
		}
		return nil

	case "enter":
		if state.FocusField == layout.submitField {
			return a.handleChannelFormSubmit()
		} else if state.FocusField == layout.cancelField {
			a.serverManagementState.ChannelFormOpen = false
			a.serverManagementState.ChannelFormState = nil
		}
		return nil
	}

	// Forward keystrokes to the focused text input, if any.
	if state.FocusField == 0 {
		var cmd tea.Cmd
		state.NameTextInput, cmd = state.NameTextInput.Update(msg)
		return cmd
	}
	if layout.isVoice && state.FocusField == layout.maxUsersField {
		var cmd tea.Cmd
		state.MaxUsersInput, cmd = state.MaxUsersInput.Update(msg)
		return cmd
	}
	if layout.isPlugin && state.FocusField >= layout.pluginStart && state.FocusField < layout.pluginStart+len(state.PluginFields) {
		idx := state.FocusField - layout.pluginStart
		ft := state.PluginFields[idx].Type
		if ft == "text" || ft == "number" {
			var cmd tea.Cmd
			state.PluginTextInputs[idx], cmd = state.PluginTextInputs[idx].Update(msg)
			return cmd
		}
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

	pluginTypeLocked := state.Mode == "edit" && state.OriginalType == models.ChannelTypePlugin

	// Determine type: 0=Text, 1=Voice, 2=Category, 3+=plugin kind
	var channelType models.ChannelType
	switch {
	case pluginTypeLocked:
		channelType = models.ChannelTypePlugin
	case state.TypeIndex == 1:
		channelType = models.ChannelTypeVoice
	case state.TypeIndex == 2:
		channelType = models.ChannelTypeCategory
	case state.TypeIndex >= 3:
		channelType = models.ChannelTypePlugin
	default:
		channelType = models.ChannelTypeText
	}

	// Client-side required-field check for plugin channels, mirroring the
	// server's own validation so the error surfaces immediately. Applies to
	// edit mode too now that a plugin channel's create_fields are editable.
	if channelType == models.ChannelTypePlugin {
		for _, f := range state.PluginFields {
			if f.Required && pluginConfigValues(state)[f.Key] == "" {
				state.ErrorMsg = fmt.Sprintf("%s is required", f.Label)
				return nil
			}
		}
	}

	// Parse MaxUsers (voice channels only)
	maxUsers := 0
	if channelType == models.ChannelTypeVoice {
		if v, err := strconv.Atoi(strings.TrimSpace(state.MaxUsersInput.Value())); err == nil && v >= 0 {
			maxUsers = v
		}
	}

	serverID := a.currentServer.ID
	var req interface{}
	var opCode protocol.OpCode

	if state.Mode == "create" {
		// Categories are always top-level (no parent) — nesting a category
		// inside a category is rejected server-side. Text, voice, and
		// plugin channels can all have a parent group; the server applies
		// CategoryID uniformly to any non-category channel type.
		var categoryID *uuid.UUID
		if channelType != models.ChannelTypeCategory {
			categoryID = state.CategoryID
		}

		createReq := &protocol.ChannelCreateRequest{
			ServerID:   serverID,
			Name:       name,
			Type:       channelType,
			CategoryID: categoryID,
			MaxUsers:   maxUsers,
		}
		if channelType == models.ChannelTypePlugin {
			createReq.PluginID = state.PluginID
			createReq.PluginChannelKind = state.PluginKind
			createReq.PluginConfig = pluginConfigValues(state)
		}
		req = createReq
		opCode = protocol.OpChannelCreate
	} else {
		updateReq := &protocol.ChannelUpdateRequest{
			ServerID:  serverID,
			ChannelID: *state.EditingChannelID,
			Name:      &name,
			MaxUsers:  &maxUsers,
		}
		// A plugin channel's type/kind can't be changed via this form — the
		// server rejects Type: ChannelTypePlugin on update, so leave it nil
		// (meaning "unchanged") rather than risk silently converting it.
		if !pluginTypeLocked {
			updateReq.Type = &channelType
		} else {
			updateReq.PluginConfig = pluginConfigValues(state)
		}
		req = updateReq
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
	state := a.serverManagementState.RoleFormState
	if state == nil {
		return nil
	}

	switch msg.String() {
	case "esc":
		a.serverManagementState.RoleFormOpen = false
		a.serverManagementState.RoleFormState = nil
		return nil

	case "tab":
		state.FocusField = (state.FocusField + 1) % 8 // 8 fields total (0-7)
		return nil

	case "shift+tab":
		state.FocusField--
		if state.FocusField < 0 {
			state.FocusField = 7
		}
		return nil

	case "up":
		if state.FocusField == 1 {
			// Permission Preset navigation
			state.PresetIndex--
			if state.PresetIndex < 0 {
				state.PresetIndex = 3
			}
		}
		return nil

	case "down":
		if state.FocusField == 1 {
			// Permission Preset navigation
			state.PresetIndex = (state.PresetIndex + 1) % 4
		}
		return nil

	case " ", "space":
		// Toggle checkboxes
		if state.FocusField == 3 {
			state.IsHoisted = !state.IsHoisted
		} else if state.FocusField == 4 {
			state.IsMentionable = !state.IsMentionable
		}
		return nil

	case "enter":
		if state.FocusField == 6 { // Submit button
			return a.handleRoleFormSubmit()
		} else if state.FocusField == 7 { // Cancel button
			a.serverManagementState.RoleFormOpen = false
			a.serverManagementState.RoleFormState = nil
		}
		return nil

	case "backspace":
		if state.FocusField == 0 && state.NameCursor > 0 {
			// Delete character in name field
			state.NameInput = state.NameInput[:state.NameCursor-1] + state.NameInput[state.NameCursor:]
			state.NameCursor--
		} else if state.FocusField == 5 && state.DisplayOrderCursor > 0 {
			// Delete character in display order field
			state.DisplayOrder = state.DisplayOrder[:state.DisplayOrderCursor-1] + state.DisplayOrder[state.DisplayOrderCursor:]
			state.DisplayOrderCursor--
		}
		return nil

	case "left":
		if state.FocusField == 0 && state.NameCursor > 0 {
			state.NameCursor--
		} else if state.FocusField == 2 {
			// Color navigation
			state.ColorIndex--
			if state.ColorIndex < 0 {
				state.ColorIndex = 4
			}
		} else if state.FocusField == 5 && state.DisplayOrderCursor > 0 {
			state.DisplayOrderCursor--
		}
		return nil

	case "right":
		if state.FocusField == 0 && state.NameCursor < len(state.NameInput) {
			state.NameCursor++
		} else if state.FocusField == 2 {
			// Color navigation
			state.ColorIndex = (state.ColorIndex + 1) % 5
		} else if state.FocusField == 5 && state.DisplayOrderCursor < len(state.DisplayOrder) {
			state.DisplayOrderCursor++
		}
		return nil
	}

	// Handle text input for name field
	if state.FocusField == 0 {
		key := msg.String()
		if len(key) == 1 && len(state.NameInput) < 50 {
			state.NameInput = state.NameInput[:state.NameCursor] + key + state.NameInput[state.NameCursor:]
			state.NameCursor++
		}
	}

	// Handle text input for display order field (numeric only)
	if state.FocusField == 5 {
		key := msg.String()
		if len(key) == 1 && key >= "0" && key <= "9" && len(state.DisplayOrder) < 5 {
			state.DisplayOrder = state.DisplayOrder[:state.DisplayOrderCursor] + key + state.DisplayOrder[state.DisplayOrderCursor:]
			state.DisplayOrderCursor++
		}
	}

	return nil
}

func (a *App) handlePermissionsEditorKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState

	// Get total permission count
	permList := getPermissionList()

	switch msg.String() {
	case "esc":
		// Cancel - discard changes
		s.PermissionsEditorOpen = false
		s.PermissionsEditorRole = nil
		s.PermModifiedBits = 0
		s.PermSelectedIndex = 0
		s.PermScrollOffset = 0
		return nil

	case "enter":
		// Save changes
		if s.PermissionsEditorRole == nil {
			return nil
		}

		serverID := a.getActiveServerID()
		channelID := uuid.Nil
		if a.currentChannel != nil {
			channelID = a.currentChannel.ID
		}

		role := s.PermissionsEditorRole
		req := &protocol.UpdateRoleRequest{
			ServerID:      serverID,
			ChannelID:     channelID,
			RoleID:        role.ID,
			Name:          role.Name,
			Permissions:   s.PermModifiedBits,
			Color:         role.Color,
			DisplayOrder:  &role.DisplayOrder,
			IsHoisted:     role.IsHoisted,
			IsMentionable: role.IsMentionable,
		}

		if msg, err := protocol.NewMessage(protocol.OpUpdateRole, req); err == nil {
			if a.activeConn != nil {
				_ = a.activeConn.Connection.Send(msg)
			}
		}

		// Close editor
		s.PermissionsEditorOpen = false
		s.PermissionsEditorRole = nil
		s.PermModifiedBits = 0
		s.PermSelectedIndex = 0
		s.PermScrollOffset = 0
		return nil

	case "up", "k":
		if s.PermSelectedIndex > 0 {
			s.PermSelectedIndex--
		}
		return nil

	case "down", "j":
		if s.PermSelectedIndex < len(permList)-1 {
			s.PermSelectedIndex++
		}
		return nil

	case " ", "space":
		// Toggle selected permission
		if s.PermSelectedIndex >= 0 && s.PermSelectedIndex < len(permList) {
			perm := permList[s.PermSelectedIndex]
			s.PermModifiedBits ^= uint64(perm.Bit) // XOR to toggle
		}
		return nil

	case "a", "A":
		// Enable all permissions
		s.PermModifiedBits = 0
		for _, perm := range permList {
			s.PermModifiedBits |= uint64(perm.Bit)
		}
		return nil

	case "n", "N":
		// Disable all permissions
		s.PermModifiedBits = 0
		return nil
	}

	return nil
}

// handleOverwriteTargetPickerKey processes the role/member picker opened by
// handleChannelOverwritesAction — step 1 of editing a channel's permission
// overwrites.
func (a *App) handleOverwriteTargetPickerKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	targets := a.overwriteTargets()

	switch msg.String() {
	case "esc":
		s.OverwriteTargetPicker = false
		s.OverwriteChannel = nil
		s.OverwriteTargetIndex = 0
		return nil

	case "up", "k":
		if s.OverwriteTargetIndex > 0 {
			s.OverwriteTargetIndex--
		}
		return nil

	case "down", "j":
		if s.OverwriteTargetIndex < len(targets)-1 {
			s.OverwriteTargetIndex++
		}
		return nil

	case "enter":
		if s.OverwriteTargetIndex < 0 || s.OverwriteTargetIndex >= len(targets) {
			return nil
		}
		target := targets[s.OverwriteTargetIndex]
		s.OverwriteTargetID = target.ID
		s.OverwriteTargetType = target.Type
		s.OverwriteTargetName = target.Name
		s.OverwriteAllowBits = 0
		s.OverwriteDenyBits = 0
		if existing, ok := a.findChannelOverwrite(target.ID); ok {
			s.OverwriteAllowBits = uint64(existing.Allow)
			s.OverwriteDenyBits = uint64(existing.Deny)
		}
		s.OverwriteSelectedIndex = 0
		s.OverwriteTargetPicker = false
		s.OverwriteEditorOpen = true
		return nil
	}

	return nil
}

// cycleOverwriteState advances one permission bit through
// Inherit -> Allow -> Deny -> Inherit across a pair of Allow/Deny bitfields —
// mirrors models.PermissionOverwrite's shape, where a bit lives in neither,
// Allow, or Deny, never both.
func cycleOverwriteState(allowBits, denyBits *uint64, bit uint64) {
	switch {
	case *allowBits&bit != 0:
		*allowBits &^= bit
		*denyBits |= bit
	case *denyBits&bit != 0:
		*denyBits &^= bit
	default:
		*allowBits |= bit
	}
}

// handleOverwriteEditorKey processes the Inherit/Allow/Deny editor opened
// after picking a target — step 2. Enter and Esc both return to the target
// picker (not a full close) so managing several targets on the same channel
// doesn't mean re-entering through the Channels list each time; Esc on the
// picker itself is what fully closes the flow.
func (a *App) handleOverwriteEditorKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	permList := getOverwriteablePermissionList()

	switch msg.String() {
	case "esc":
		s.OverwriteEditorOpen = false
		s.OverwriteTargetPicker = true
		return nil

	case "enter":
		if s.OverwriteChannel == nil {
			return nil
		}
		req := &protocol.UpdateChannelOverwriteRequest{
			ServerID:   a.getActiveServerID(),
			ChannelID:  s.OverwriteChannel.ID,
			TargetID:   s.OverwriteTargetID,
			TargetType: s.OverwriteTargetType,
			Allow:      int64(s.OverwriteAllowBits),
			Deny:       int64(s.OverwriteDenyBits),
			// Every bit back to Inherit means there's nothing left to store —
			// delete the row instead of upserting an all-zero no-op.
			Delete: s.OverwriteAllowBits == 0 && s.OverwriteDenyBits == 0,
		}
		if wireMsg, err := protocol.NewMessage(protocol.OpUpdateChannelOverwrite, req); err == nil {
			if a.activeConn != nil {
				_ = a.activeConn.Connection.Send(wireMsg)
			}
		}
		a.statusMessage = fmt.Sprintf("Updated permissions for %s", s.OverwriteTargetName)

		// Apply the same edit locally so the target picker's "(overwrite set)"
		// marker is correct immediately, instead of waiting on the server's
		// response to refresh s.OverwriteChannel.
		overwrites := s.OverwriteChannel.PermissionOverwrites
		for i, ow := range overwrites {
			if ow.ID == s.OverwriteTargetID {
				overwrites = append(overwrites[:i], overwrites[i+1:]...)
				break
			}
		}
		if !req.Delete {
			overwrites = append(overwrites, models.PermissionOverwrite{
				ID:    s.OverwriteTargetID,
				Type:  s.OverwriteTargetType,
				Allow: int64(s.OverwriteAllowBits),
				Deny:  int64(s.OverwriteDenyBits),
			})
		}
		s.OverwriteChannel.PermissionOverwrites = overwrites

		s.OverwriteEditorOpen = false
		s.OverwriteTargetPicker = true
		return nil

	case "up", "k":
		if s.OverwriteSelectedIndex > 0 {
			s.OverwriteSelectedIndex--
		}
		return nil

	case "down", "j":
		if s.OverwriteSelectedIndex < len(permList)-1 {
			s.OverwriteSelectedIndex++
		}
		return nil

	case " ", "space":
		if s.OverwriteSelectedIndex >= 0 && s.OverwriteSelectedIndex < len(permList) {
			bit := uint64(permList[s.OverwriteSelectedIndex].Bit)
			cycleOverwriteState(&s.OverwriteAllowBits, &s.OverwriteDenyBits, bit)
		}
		return nil
	}

	return nil
}

func (a *App) handleDeleteConfirmKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState

	switch msg.String() {
	case "tab", "right":
		// Next button
		s.DeleteConfirmFocusedButton = (s.DeleteConfirmFocusedButton + 1) % 2
		return nil

	case "shift+tab", "left":
		// Previous button
		s.DeleteConfirmFocusedButton--
		if s.DeleteConfirmFocusedButton < 0 {
			s.DeleteConfirmFocusedButton = 1
		}
		return nil

	case "enter":
		// Confirm focused button
		if s.DeleteConfirmFocusedButton == 0 {
			// Yes, Delete
			return a.handleDeleteConfirmed()
		} else {
			// No, Cancel
			s.DeleteConfirmOpen = false
			s.DeleteConfirmChannel = nil
			s.DeleteConfirmRole = nil
			s.DeleteConfirmFocusedButton = 0 // Reset to default
			return nil
		}

	case "y", "Y":
		// Quick confirm
		return a.handleDeleteConfirmed()

	case "n", "N", "esc":
		// Quick cancel
		s.DeleteConfirmOpen = false
		s.DeleteConfirmChannel = nil
		s.DeleteConfirmRole = nil
		s.DeleteConfirmFocusedButton = 0 // Reset to default
		return nil
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
	} else if s.DeleteConfirmRole != nil {
		// Handle role deletion
		// Get current channel for the command context
		var channelID uuid.UUID
		if a.currentChannel != nil {
			channelID = a.currentChannel.ID
		}

		req := &protocol.DeleteRoleRequest{
			ServerID:  a.currentServer.ID,
			ChannelID: channelID,
			RoleID:    s.DeleteConfirmRole.ID,
		}

		msg, err := protocol.NewMessage(protocol.OpDeleteRole, req)
		if err != nil {
			a.statusMessage = fmt.Sprintf("Failed: %v", err)
			return nil
		}

		if err := a.activeConn.Connection.Send(msg); err != nil {
			a.statusMessage = fmt.Sprintf("Failed: %v", err)
			return nil
		}

		a.statusMessage = fmt.Sprintf("Deleting role %s...", s.DeleteConfirmRole.Name)
		// Role list will be updated automatically when EventRoleDelete is received
	}

	s.DeleteConfirmOpen = false
	s.DeleteConfirmChannel = nil
	s.DeleteConfirmRole = nil
	s.DeleteConfirmFocusedButton = 0 // Reset to default

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
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	// Get available roles to calculate option count
	var availableRoles []*models.Role
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			for _, role := range roles {
				if !role.IsDefault {
					availableRoles = append(availableRoles, role)
				}
			}
		}
		a.activeConn.mu.RUnlock()
	}

	// Calculate total options: 1 (All) + roles + 3 (statuses) + 4 (sort options)
	roleCount := 1 + len(availableRoles)
	statusCount := 3
	sortCount := 4
	totalOptions := roleCount + statusCount + sortCount

	switch msg.String() {
	case "up", "k":
		if s.FilterPanelFocus > 0 {
			s.FilterPanelFocus--
		}

	case "down", "j":
		if s.FilterPanelFocus < totalOptions-1 {
			s.FilterPanelFocus++
		}

	case "enter", " ":
		// Apply filter based on focused option
		if s.FilterPanelFocus == 0 {
			// "All" role option
			s.FilterRole = "All"
		} else if s.FilterPanelFocus < roleCount {
			// Individual role
			if s.FilterPanelFocus-1 < len(availableRoles) {
				s.FilterRole = availableRoles[s.FilterPanelFocus-1].Name
			}
		} else if s.FilterPanelFocus < roleCount+statusCount {
			// Status filter
			statusIdx := s.FilterPanelFocus - roleCount
			statuses := []string{"all", "online", "offline"}
			if statusIdx < len(statuses) {
				s.FilterOnline = statuses[statusIdx]
			}
		} else {
			// Sort option
			sortIdx := s.FilterPanelFocus - roleCount - statusCount
			sortOptions := []string{"name", "joined", "role", "kicks"}
			if sortIdx < len(sortOptions) {
				s.SortBy = sortOptions[sortIdx]
			}
		}
		// Re-filter and re-sort members
		a.applyMemberFilters()

	case "esc":
		s.FilterPanelOpen = false
	}

	return nil
}

func (a *App) handleSearchInputKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "esc":
		// Clear search and close
		s.SearchInputOpen = false
		s.SearchQuery = ""
		a.applyMemberFilters()

	case "enter":
		// Apply search and close input
		s.SearchQuery = s.SearchInputValue
		s.SearchInputOpen = false
		a.applyMemberFilters()

	case "backspace":
		if len(s.SearchInputValue) > 0 {
			s.SearchInputValue = s.SearchInputValue[:len(s.SearchInputValue)-1]
			if s.SearchInputCursor > 0 {
				s.SearchInputCursor--
			}
		}

	case "left":
		if s.SearchInputCursor > 0 {
			s.SearchInputCursor--
		}

	case "right":
		if s.SearchInputCursor < len(s.SearchInputValue) {
			s.SearchInputCursor++
		}

	default:
		// Handle regular character input
		if len(msg.String()) == 1 {
			s.SearchInputValue += msg.String()
			s.SearchInputCursor = len(s.SearchInputValue)
		}
	}

	return nil
}

func (a *App) handleRoleAssignKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	// Get available roles
	var availableRoles []*models.Role
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			availableRoles = roles
		}
		a.activeConn.mu.RUnlock()
	}

	// Sort roles by position (descending)
	sort.Slice(availableRoles, func(i, j int) bool {
		return availableRoles[i].Position > availableRoles[j].Position
	})

	switch msg.String() {
	case "up", "k":
		if s.RoleAssignFocus > 0 {
			s.RoleAssignFocus--
		}

	case "down", "j":
		if s.RoleAssignFocus < len(availableRoles)-1 {
			s.RoleAssignFocus++
		}

	case " ": // Space to toggle
		if s.RoleAssignFocus >= 0 && s.RoleAssignFocus < len(availableRoles) {
			role := availableRoles[s.RoleAssignFocus]
			if !role.IsDefault { // Can't toggle @everyone
				s.RoleAssignSelections[role.ID] = !s.RoleAssignSelections[role.ID]
			}
		}

	case "enter":
		// Save changes: compare current selections with member's roles
		if s.RoleAssignMember == nil || s.RoleAssignMember.Member == nil {
			s.RoleAssignOpen = false
			return nil
		}

		// Get current role IDs
		currentRoles := make(map[uuid.UUID]bool)
		for _, roleID := range s.RoleAssignMember.Member.RoleIDs {
			currentRoles[roleID] = true
		}

		// Find roles to add and remove
		var toAdd []uuid.UUID
		var toRemove []uuid.UUID

		for _, role := range availableRoles {
			if role.IsDefault {
				continue // Skip @everyone
			}

			isSelected := s.RoleAssignSelections[role.ID]
			hadRole := currentRoles[role.ID]

			if isSelected && !hadRole {
				toAdd = append(toAdd, role.ID)
			} else if !isSelected && hadRole {
				toRemove = append(toRemove, role.ID)
			}
		}

		// Send role assign/remove messages
		serverID := a.getActiveServerID()

		// Build role name map
		roleNames := make(map[uuid.UUID]string)
		for _, role := range availableRoles {
			roleNames[role.ID] = role.Name
		}

		for _, roleID := range toAdd {
			roleName, ok := roleNames[roleID]
			if !ok {
				continue
			}
			req := &protocol.RoleAssignRequest{
				ServerID: serverID,
				UserID:   s.RoleAssignMember.User.ID,
				RoleName: roleName,
			}
			msg, err := protocol.NewMessage(protocol.OpRoleAssign, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}
		}

		for _, roleID := range toRemove {
			roleName, ok := roleNames[roleID]
			if !ok {
				continue
			}
			req := &protocol.RoleRemoveRequest{
				ServerID: serverID,
				UserID:   s.RoleAssignMember.User.ID,
				RoleName: roleName,
			}
			msg, err := protocol.NewMessage(protocol.OpRoleRemove, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
			}
		}

		if len(toAdd) > 0 || len(toRemove) > 0 {
			a.statusMessage = fmt.Sprintf("Updated roles for %s", s.RoleAssignMember.User.Username)
		}

		s.RoleAssignOpen = false

	case "esc":
		s.RoleAssignOpen = false
	}

	return nil
}

func (a *App) handleKickConfirmKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "tab", "right":
		s.KickConfirmFocusedBtn = (s.KickConfirmFocusedBtn + 1) % 2
	case "shift+tab", "left":
		s.KickConfirmFocusedBtn--
		if s.KickConfirmFocusedBtn < 0 {
			s.KickConfirmFocusedBtn = 1
		}
	case "enter":
		if s.KickConfirmFocusedBtn == 0 && s.KickConfirmMember != nil {
			// Confirm kick
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			req := &protocol.KickMemberRequest{
				ServerID:  serverID,
				ChannelID: channelID, // Send to the channel the user was in
				UserID:    s.KickConfirmMember.User.ID,
			}
			msg, err := protocol.NewMessage(protocol.OpKickMember, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
				a.statusMessage = fmt.Sprintf("Kicked %s from server", s.KickConfirmMember.User.Username)
			}
		}
		s.KickConfirmOpen = false
	case "y", "Y":
		// Quick confirm
		if s.KickConfirmMember != nil {
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			req := &protocol.KickMemberRequest{
				ServerID:  serverID,
				ChannelID: channelID, // Send to the channel the user was in
				UserID:    s.KickConfirmMember.User.ID,
			}
			msg, err := protocol.NewMessage(protocol.OpKickMember, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
				a.statusMessage = fmt.Sprintf("Kicked %s from server", s.KickConfirmMember.User.Username)
			}
		}
		s.KickConfirmOpen = false
	case "n", "N", "esc":
		s.KickConfirmOpen = false
	}
	return nil
}

func (a *App) handleBanConfirmKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "tab", "right":
		s.BanConfirmFocusedBtn = (s.BanConfirmFocusedBtn + 1) % 2
	case "shift+tab", "left":
		s.BanConfirmFocusedBtn--
		if s.BanConfirmFocusedBtn < 0 {
			s.BanConfirmFocusedBtn = 1
		}
	case "enter":
		if s.BanConfirmFocusedBtn == 0 && s.BanConfirmMember != nil {
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			if s.BanConfirmMember.IsBanned {
				// Unban
				req := &protocol.UnbanMemberRequest{
					ServerID:  serverID,
					ChannelID: channelID,
					Username:  s.BanConfirmMember.User.Username,
				}
				msg, err := protocol.NewMessage(protocol.OpUnbanMember, req)
				if err == nil {
					_ = a.activeConn.Connection.Send(msg)
					a.statusMessage = fmt.Sprintf("Unbanned %s", s.BanConfirmMember.User.Username)
				}
			} else {
				// Ban
				req := &protocol.BanMemberRequest{
					ServerID:  serverID,
					ChannelID: channelID,
					UserID:    s.BanConfirmMember.User.ID,
					Reason:    "Banned from member management",
				}
				msg, err := protocol.NewMessage(protocol.OpBanMember, req)
				if err == nil {
					_ = a.activeConn.Connection.Send(msg)
					a.statusMessage = fmt.Sprintf("Banned %s from server", s.BanConfirmMember.User.Username)
				}
			}
		}
		s.BanConfirmOpen = false
	case "y", "Y":
		// Quick confirm
		if s.BanConfirmMember != nil {
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			if s.BanConfirmMember.IsBanned {
				req := &protocol.UnbanMemberRequest{
					ServerID:  serverID,
					ChannelID: channelID,
					Username:  s.BanConfirmMember.User.Username,
				}
				msg, err := protocol.NewMessage(protocol.OpUnbanMember, req)
				if err == nil {
					_ = a.activeConn.Connection.Send(msg)
					a.statusMessage = fmt.Sprintf("Unbanned %s", s.BanConfirmMember.User.Username)
				}
			} else {
				req := &protocol.BanMemberRequest{
					ServerID:  serverID,
					ChannelID: channelID,
					UserID:    s.BanConfirmMember.User.ID,
					Reason:    "Banned from member management",
				}
				msg, err := protocol.NewMessage(protocol.OpBanMember, req)
				if err == nil {
					_ = a.activeConn.Connection.Send(msg)
					a.statusMessage = fmt.Sprintf("Banned %s from server", s.BanConfirmMember.User.Username)
				}
			}
		}
		s.BanConfirmOpen = false
	case "n", "N", "esc":
		s.BanConfirmOpen = false
	}
	return nil
}

func (a *App) handleUnmuteConfirmKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "tab", "right":
		s.UnmuteConfirmFocusedBtn = (s.UnmuteConfirmFocusedBtn + 1) % 2
	case "shift+tab", "left":
		s.UnmuteConfirmFocusedBtn--
		if s.UnmuteConfirmFocusedBtn < 0 {
			s.UnmuteConfirmFocusedBtn = 1
		}
	case "enter":
		if s.UnmuteConfirmFocusedBtn == 0 && s.UnmuteConfirmMember != nil {
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			req := &protocol.MuteMemberRequest{
				ServerID:  serverID,
				ChannelID: channelID,
				UserID:    s.UnmuteConfirmMember.User.ID,
				Mute:      false, // Unmute
			}
			msg, err := protocol.NewMessage(protocol.OpMuteMember, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
				a.statusMessage = fmt.Sprintf("Unmuted %s", s.UnmuteConfirmMember.User.Username)
			}
		}
		s.UnmuteConfirmOpen = false
	case "y", "Y":
		// Quick confirm
		if s.UnmuteConfirmMember != nil {
			serverID := a.getActiveServerID()
			channelID := uuid.Nil
			if a.currentChannel != nil {
				channelID = a.currentChannel.ID
			}
			req := &protocol.MuteMemberRequest{
				ServerID:  serverID,
				ChannelID: channelID,
				UserID:    s.UnmuteConfirmMember.User.ID,
				Mute:      false, // Unmute
			}
			msg, err := protocol.NewMessage(protocol.OpMuteMember, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(msg)
				a.statusMessage = fmt.Sprintf("Unmuted %s", s.UnmuteConfirmMember.User.Username)
			}
		}
		s.UnmuteConfirmOpen = false
	case "n", "N", "esc":
		s.UnmuteConfirmOpen = false
	}
	return nil
}

func (a *App) handleMuteDurationKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	if s == nil {
		return nil
	}

	// Mute duration page has: 4 radio buttons (1h, 24h, 7d, permanent) + custom field + reason field = 6 fields total
	totalFields := 6

	switch msg.String() {
	case "up":
		// Arrow keys always work for navigation
		if s.MuteDurationFocus > 0 {
			s.MuteDurationFocus--
		}
	case "k":
		// Vim keys don't work when focus is on text input fields
		if s.MuteDurationFocus != 4 && s.MuteDurationFocus != 5 {
			if s.MuteDurationFocus > 0 {
				s.MuteDurationFocus--
			}
		}
	case "down":
		// Arrow keys always work for navigation
		if s.MuteDurationFocus < totalFields-1 {
			s.MuteDurationFocus++
		}
	case "j":
		// Vim keys don't work when focus is on text input fields
		if s.MuteDurationFocus != 4 && s.MuteDurationFocus != 5 {
			if s.MuteDurationFocus < totalFields-1 {
				s.MuteDurationFocus++
			}
		}
	case "enter":
		// Apply mute
		if s.MuteDurationMember == nil {
			s.MuteDurationOpen = false
			return nil
		}

		serverID := a.getActiveServerID()
		channelID := uuid.Nil
		if a.currentChannel != nil {
			channelID = a.currentChannel.ID
		}
		var durationMinutes int

		// Check if custom duration is filled first (regardless of focus)
		if s.MuteDurationCustom != "" {
			durationMinutes = parseDuration(s.MuteDurationCustom)
			if durationMinutes == 0 {
				a.statusMessage = "Invalid duration format. Use format like: 30m, 5h, 2d"
				return nil
			}
		} else {
			// Use preset duration based on focus
			switch s.MuteDurationFocus {
			case 0: // 1 hour
				durationMinutes = 60
			case 1: // 24 hours
				durationMinutes = 60 * 24
			case 2: // 7 days
				durationMinutes = 60 * 24 * 7
			case 3: // Permanent
				durationMinutes = 0
			default:
				// If focused on reason field and no custom duration, treat as permanent
				durationMinutes = 0
			}
		}

		req := &protocol.MuteMemberRequest{
			ServerID:  serverID,
			ChannelID: channelID,
			UserID:    s.MuteDurationMember.User.ID,
			Mute:      true,
			Duration:  durationMinutes,
		}
		msg, err := protocol.NewMessage(protocol.OpMuteMember, req)
		if err == nil {
			_ = a.activeConn.Connection.Send(msg)
			if durationMinutes == 0 {
				a.statusMessage = fmt.Sprintf("Permanently muted %s", s.MuteDurationMember.User.Username)
			} else {
				a.statusMessage = fmt.Sprintf("Muted %s for %d minutes", s.MuteDurationMember.User.Username, durationMinutes)
			}
		}
		s.MuteDurationOpen = false

	case "esc":
		s.MuteDurationOpen = false

	default:
		// Handle text input for custom duration and reason fields
		if s.MuteDurationFocus == 4 {
			// Custom duration field
			if msg.String() == "backspace" {
				if len(s.MuteDurationCustom) > 0 {
					s.MuteDurationCustom = s.MuteDurationCustom[:len(s.MuteDurationCustom)-1]
				}
			} else if len(msg.String()) == 1 {
				s.MuteDurationCustom += msg.String()
			}
		} else if s.MuteDurationFocus == 5 {
			// Reason field
			if msg.String() == "backspace" {
				if len(s.MuteDurationReason) > 0 {
					s.MuteDurationReason = s.MuteDurationReason[:len(s.MuteDurationReason)-1]
				}
			} else if len(msg.String()) == 1 {
				s.MuteDurationReason += msg.String()
			}
		}
	}
	return nil
}

// parseDuration parses duration strings like "30m", "5h", "2d" into minutes
func parseDuration(dur string) int {
	if len(dur) < 2 {
		return 0
	}

	// Extract number and unit
	numStr := dur[:len(dur)-1]
	unit := dur[len(dur)-1:]

	num := 0
	for _, c := range numStr {
		if c >= '0' && c <= '9' {
			num = num*10 + int(c-'0')
		} else {
			return 0 // Invalid number
		}
	}

	switch unit {
	case "m":
		return num
	case "h":
		return num * 60
	case "d":
		return num * 60 * 24
	default:
		return 0
	}
}

func (a *App) handleChannelPickerKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	switch msg.String() {
	case "up":
		if s.OverrideChannelSelected > 0 {
			s.OverrideChannelSelected--
		}
	case "down":
		if s.OverrideChannelSelected < len(s.OverrideChannelList)-1 {
			s.OverrideChannelSelected++
		}
	case "enter":
		if len(s.OverrideChannelList) > 0 && s.OverrideChannelSelected < len(s.OverrideChannelList) {
			ch := s.OverrideChannelList[s.OverrideChannelSelected]
			serverID := a.getActiveServerID()
			chID := ch.ID
			req := &protocol.SetRetentionPolicyRequest{
				ServerID:  serverID,
				ChannelID: &chID,
				// nil limits = keep all messages = exempt
			}
			pmsg, err := protocol.NewMessage(protocol.OpSetRetentionPolicy, req)
			if err == nil {
				_ = a.activeConn.Connection.Send(pmsg)
				a.statusMessage = fmt.Sprintf("#%s is now exempt from pruning", ch.Name)
			}
		}
		s.OverrideChannelPickerOpen = false
	case "esc":
		s.OverrideChannelPickerOpen = false
	}
	return nil
}

func (a *App) handleRemoveExemptKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	switch msg.String() {
	case "up":
		if s.RemoveExemptSelected > 0 {
			s.RemoveExemptSelected--
		}
	case "down":
		if s.RemoveExemptSelected < len(s.ChannelOverrides)-1 {
			s.RemoveExemptSelected++
		}
	case "enter":
		if len(s.ChannelOverrides) > 0 && s.RemoveExemptSelected < len(s.ChannelOverrides) {
			override := s.ChannelOverrides[s.RemoveExemptSelected]
			if override.ChannelID != nil {
				serverID := a.getActiveServerID()
				req := &protocol.DeleteRetentionPolicyRequest{
					ServerID:  serverID,
					ChannelID: *override.ChannelID,
				}
				pmsg, err := protocol.NewMessage(protocol.OpDeleteRetentionPolicy, req)
				if err == nil {
					_ = a.activeConn.Connection.Send(pmsg)
					chName := a.resolveChannelName(*override.ChannelID)
					a.statusMessage = fmt.Sprintf("#%s override removed", chName)
				}
			}
		}
		s.RemoveExemptPickerOpen = false
	case "esc":
		s.RemoveExemptPickerOpen = false
	}
	return nil
}

// renderRemoveExemptPage renders the remove-channel-exemption picker as a full settings page
func (a *App) renderRemoveExemptPage(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 2, 0)

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Remove Channel Override"))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("Select a channel to remove its override — it will use the server default again"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Background(lipgloss.Color(a.theme.Colors.Red)).Bold(true)
	badgeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	if len(s.ChannelOverrides) == 0 {
		middle.writeLine(dimStyle.Render("  No channel overrides to remove"))
	} else {
		for i, override := range s.ChannelOverrides {
			chName := "unknown"
			if override.ChannelID != nil {
				chName = a.resolveChannelName(*override.ChannelID)
			}
			suffix := retentionOverrideSuffix(override)
			id := fmt.Sprintf("srvmgmt-remove-exempt-row:%d", i)
			if i == s.RemoveExemptSelected {
				middle.writeLine(zone.Mark(id, selectedStyle.Render(fmt.Sprintf("  # %s %s", chName, suffix))))
			} else {
				middle.writeLine(zone.Mark(id, normalStyle.Render(fmt.Sprintf("  # %s ", chName))+badgeStyle.Render(suffix)))
			}
		}
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("↑↓ navigate · [Enter] remove exemption · [Esc] cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

func (a *App) handleRetentionFormKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	form := s.RetentionFormState
	if form == nil {
		return nil
	}

	// Fields 0-2 are text inputs; 3 = [S] Save button (virtual)
	onTextField := form.FocusField >= 0 && form.FocusField <= 2

	switch msg.String() {
	case "esc":
		s.RetentionFormState = nil
	case "tab":
		form.FocusField = (form.FocusField + 1) % 3 // cycle through 3 text fields
	case "shift+tab":
		form.FocusField--
		if form.FocusField < 0 {
			form.FocusField = 2
		}
	case "s", "S":
		a.saveRetentionPolicy(form)
	case "enter":
		// Enter on any text field also saves
		if onTextField {
			a.saveRetentionPolicy(form)
		}
	case "backspace":
		if onTextField {
			switch form.FocusField {
			case 0:
				if len(form.TimeRetentionDays) > 0 {
					form.TimeRetentionDays = form.TimeRetentionDays[:len(form.TimeRetentionDays)-1]
				}
			case 1:
				if len(form.SystemTimeRetentionDays) > 0 {
					form.SystemTimeRetentionDays = form.SystemTimeRetentionDays[:len(form.SystemTimeRetentionDays)-1]
				}
			case 2:
				if len(form.MaxMessageCount) > 0 {
					form.MaxMessageCount = form.MaxMessageCount[:len(form.MaxMessageCount)-1]
				}
			}
		}
	default:
		if onTextField && len(msg.String()) == 1 {
			ch := msg.String()[0]
			if ch >= '0' && ch <= '9' {
				switch form.FocusField {
				case 0:
					form.TimeRetentionDays += msg.String()
				case 1:
					form.SystemTimeRetentionDays += msg.String()
				case 2:
					form.MaxMessageCount += msg.String()
				}
			}
		}
	}
	return nil
}

func (a *App) saveRetentionPolicy(form *RetentionFormState) {
	s := a.serverManagementState
	serverID := a.getActiveServerID()

	var timeRetDays, sysTimeRetDays, maxCount *int
	if v, err := strconv.Atoi(form.TimeRetentionDays); err == nil && v > 0 {
		timeRetDays = &v
	}
	if v, err := strconv.Atoi(form.SystemTimeRetentionDays); err == nil && v > 0 {
		sysTimeRetDays = &v
	}
	if v, err := strconv.Atoi(form.MaxMessageCount); err == nil && v > 0 {
		maxCount = &v
	}

	req := &protocol.SetRetentionPolicyRequest{
		ServerID:                serverID,
		ChannelID:               form.ChannelID,
		TimeRetentionDays:       timeRetDays,
		SystemTimeRetentionDays: sysTimeRetDays,
		MaxMessageCount:         maxCount,
	}
	pmsg, err := protocol.NewMessage(protocol.OpSetRetentionPolicy, req)
	if err == nil {
		_ = a.activeConn.Connection.Send(pmsg)
		a.statusMessage = "Retention policy saved"
	}
	s.RetentionFormState = nil
}

func (a *App) handlePruneConfirmKey(msg tea.KeyMsg) tea.Cmd {
	s := a.serverManagementState
	switch msg.String() {
	case "enter":
		serverID := a.getActiveServerID()
		req := &protocol.PruneMessagesRequest{
			ServerID: serverID,
		}
		pmsg, err := protocol.NewMessage(protocol.OpPruneMessages, req)
		if err == nil {
			_ = a.activeConn.Connection.Send(pmsg)
			a.statusMessage = "Pruning messages..."
		}
		s.PruneConfirmOpen = false
	case "esc", "n", "N":
		s.PruneConfirmOpen = false
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

	// -1 (not -2, as this and every content-category call below used to be)
	// for the "Server Settings" title bar row -- see the matching fix and
	// full explanation on settingsSectionBuilder.String() in settings_view.go.
	// Both the sidebar and every content page must agree on this same
	// 1-row budget or they drift apart by a line, which is exactly the
	// live-tested bug this fixes (sidebar border stopping short of the
	// content panel's on every Server Settings page).
	// ── Left: Category list ────────────────────────────────────────
	catPanel := a.renderCategorySidebar(catWidth, totalHeight-1, s)

	// ── Right: Content panel ───────────────────────────────────────
	var contentPanel string
	switch s.SelectedCategory {
	case 0: // Channels
		contentPanel = a.renderChannelsCategory(contentWidth, totalHeight-1, s)
	case 1: // Roles
		contentPanel = a.renderRolesCategory(contentWidth, totalHeight-1, s)
	case 2: // Members
		if s.RoleAssignOpen {
			contentPanel = a.renderMembersRoleAssignPage(contentWidth, totalHeight-1, s)
		} else if s.FilterPanelOpen {
			contentPanel = a.renderMembersFilterPage(contentWidth, totalHeight-1, s)
		} else {
			contentPanel = a.renderMembersCategory(contentWidth, totalHeight-1, s)
		}
	case 3: // Messages
		if s.RemoveExemptPickerOpen {
			contentPanel = a.renderRemoveExemptPage(contentWidth, totalHeight-1, s)
		} else if s.OverrideChannelPickerOpen {
			contentPanel = a.renderChannelPickerPage(contentWidth, totalHeight-1, s)
		} else if s.RetentionFormState != nil {
			contentPanel = a.renderRetentionFormPage(contentWidth, totalHeight-1, s)
		} else if s.PruneConfirmOpen {
			contentPanel = a.renderPruneConfirmPage(contentWidth, totalHeight-1, s)
		} else {
			contentPanel = a.renderMessagesCategory(contentWidth, totalHeight-1, s)
		}
	case 4: // Plugins
		if s.PluginConfigState != nil {
			contentPanel = a.renderPluginConfigPage(contentWidth, totalHeight-1, s)
		} else {
			contentPanel = a.renderPluginsCategory(contentWidth, totalHeight-1, s)
		}
	case 5: // About
		contentPanel = a.renderServerAboutContent(contentWidth, totalHeight-1)
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

	// Show moderation dialogs/pages if active
	if s.KickConfirmOpen {
		return a.renderKickConfirmDialog()
	}
	if s.BanConfirmOpen {
		return a.renderBanConfirmDialog()
	}
	if s.UnmuteConfirmOpen {
		return a.renderUnmuteConfirmDialog()
	}
	if s.MuteDurationOpen {
		return a.renderMuteDurationPage()
	}

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
		buf.WriteString(zone.Mark(fmt.Sprintf("srvmgmt-cat-row:%d", i), line))
		buf.WriteString("\n")
	}

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
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
	if s.OverwriteEditorOpen {
		return a.renderOverwriteEditorPage(width, height, s)
	}
	if s.OverwriteTargetPicker {
		return a.renderOverwriteTargetPickerPage(width, height, s)
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
	textCount, voiceCount, categoryCount, pluginCount := 0, 0, 0, 0
	for _, ch := range s.ChannelList {
		switch ch.Type {
		case models.ChannelTypeText:
			textCount++
		case models.ChannelTypeVoice:
			voiceCount++
		case models.ChannelTypeCategory:
			categoryCount++
		case models.ChannelTypePlugin:
			pluginCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	statsLine := fmt.Sprintf("%d text · %d voice · %d groups", textCount, voiceCount, categoryCount)
	if pluginCount > 0 {
		statsLine += fmt.Sprintf(" · %d plugin", pluginCount)
	}
	top.writeLine(statsStyle.Render(statsLine))
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

		channelName := fmt.Sprintf("%s%s", a.channelIcon(ch), ch.Name)

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + channelName)
		} else {
			line = prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(channelName)
		}
		middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-channel-row:%d", item.listIdx), line))
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
	bottom.writeLine(helpStyle.Render("Actions: C create · E edit · M move · P permissions · D delete"))

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
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderOverwriteTargetPickerPage renders step 1 of the channel
// permission-overwrite flow: pick a role or member to edit.
func (a *App) renderOverwriteTargetPickerPage(width, height int, s *ServerManagementState) string {
	if s.OverwriteChannel == nil {
		return "No channel selected"
	}

	layout := calculateSettingsLayout(width, height, 3, 1)

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render(fmt.Sprintf("Permissions: #%s", s.OverwriteChannel.Name)))
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Select a role or member to set their permissions"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	targets := a.overwriteTargets()

	maxVisible := layout.middleLines - 2
	if maxVisible < 5 {
		maxVisible = 5
	}
	visibleStart, visibleEnd := 0, len(targets)
	if len(targets) > maxVisible {
		half := maxVisible / 2
		visibleStart = s.OverwriteTargetIndex - half
		visibleEnd = s.OverwriteTargetIndex + half
		if visibleStart < 0 {
			visibleStart, visibleEnd = 0, maxVisible
		}
		if visibleEnd > len(targets) {
			visibleEnd = len(targets)
			visibleStart = visibleEnd - maxVisible
			if visibleStart < 0 {
				visibleStart = 0
			}
		}
	}

	if visibleStart > 0 {
		moreStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↑ %d more", visibleStart)))
	}

	currentType := ""
	for i := visibleStart; i < visibleEnd; i++ {
		target := targets[i]
		if target.Type != currentType {
			currentType = target.Type
			label := "── Roles ──"
			if currentType == "member" {
				label = "── Members ──"
			}
			categoryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
			middle.writeLine(categoryStyle.Render(label))
		}

		marker := ""
		if target.HasOverwrite {
			marker = " (overwrite set)"
		}
		prefix := "  "
		if i == s.OverwriteTargetIndex {
			prefix = "▶ "
		}
		line := fmt.Sprintf("%s%s%s", prefix, target.Name, marker)

		if i == s.OverwriteTargetIndex {
			selectedStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth)
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-overwrite-target-row:%d", i), selectedStyle.Render(line)))
		} else {
			normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-overwrite-target-row:%d", i), normalStyle.Render(line)))
		}
	}

	if visibleEnd < len(targets) {
		moreStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(targets)-visibleEnd)))
	}
	if len(targets) == 0 {
		emptyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(emptyStyle.Render("No roles or members found"))
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Enter edit · Esc close"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderOverwriteEditorPage renders step 2: the Inherit/Allow/Deny toggle
// list for the target picked in step 1.
func (a *App) renderOverwriteEditorPage(width, height int, s *ServerManagementState) string {
	if s.OverwriteChannel == nil {
		return "No channel selected"
	}

	layout := calculateSettingsLayout(width, height, 3, 1)

	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render(fmt.Sprintf("#%s — %s", s.OverwriteChannel.Name, s.OverwriteTargetName)))
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Space cycles Inherit → Allow → Deny for the selected permission"))
	top.writeLine(subtitleStyle.Render("Inherit: no rule here · Allow: grants it here · Deny: blocks it here"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	permList := getOverwriteablePermissionList()

	for i, perm := range permList {
		bit := uint64(perm.Bit)
		state, stateStyle := "Inherit", lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		switch {
		case s.OverwriteAllowBits&bit != 0:
			state = "Allow"
			stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Bold(true)
		case s.OverwriteDenyBits&bit != 0:
			state = "Deny"
			stateStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true)
		}

		prefix := "  "
		if i == s.OverwriteSelectedIndex {
			prefix = "▶ "
		}
		line := fmt.Sprintf("%s%-24s [%s]", prefix, perm.Name, stateStyle.Render(state))

		if i == s.OverwriteSelectedIndex {
			selectedStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth)
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-overwrite-editor-row:%d", i), selectedStyle.Render(line)))
		} else {
			normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-overwrite-editor-row:%d", i), normalStyle.Render(line)))
		}
	}
	middle.pad()

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Space cycle state"))
	bottom.writeLine(helpStyle.Render("Actions: Enter save · Esc back to target list"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderRolesCategory renders the Roles management category
func (a *App) renderRolesCategory(width, height int, s *ServerManagementState) string {
	// Check if a form is open and render it instead
	if s.RoleFormOpen && s.RoleFormState != nil {
		return a.renderRoleFormPage(width, height, s)
	}
	if s.PermissionsEditorOpen {
		return a.renderPermissionsEditorPage(width, height, s)
	}

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

	// Stats - get total members from activeConn (source of truth)
	totalMembers := 0
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		totalMembers = len(a.activeConn.Members)
		a.activeConn.mu.RUnlock()
	}
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
		// @everyone role = all members
		if role.IsDefault || role.Name == "@everyone" || role.Name == "everyone" {
			memberCount = totalMembers
		} else {
			// Count members who have this role in their RoleIDs
			if a.activeConn != nil {
				a.activeConn.mu.RLock()
				for _, member := range a.activeConn.Members {
					if member.Member != nil {
						for _, roleID := range member.Member.RoleIDs {
							if roleID == role.ID {
								memberCount++
								break
							}
						}
					}
				}
				a.activeConn.mu.RUnlock()
			}
		}

		// Display "Members" instead of "everyone" for consistency
		displayName := role.Name
		if role.IsDefault || role.Name == "@everyone" || role.Name == "everyone" {
			displayName = "Members"
		}

		roleLine := fmt.Sprintf("%s (%d member", displayName, memberCount)
		if memberCount != 1 {
			roleLine += "s)"
		} else {
			roleLine += ")"
		}

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + roleLine)
		} else {
			line = prefix + lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(roleLine)
		}
		middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-role-row:%d", i), line))
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
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Esc close"))
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
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderPluginsCategory renders the Settings > Plugins list page: every
// plugin discovered under the server's Plugins directory, its version,
// process status, and enabled/disabled state — a Minecraft-mods-style view
// with zero per-plugin code, sourced entirely from OpPluginConfigGet.
// renderServerAboutContent renders Server Settings' About category: the
// connected server's own build identity, reported once at connect time via
// ReadyPayload and cached on the active ServerConnection (see
// handleReady/EventReady in app.go). Distinct from Settings > About
// (Ctrl+S), which shows the client binary's own build info.
func (a *App) renderServerAboutContent(width, height int) string {
	layout := calculateSettingsLayout(width, height, 2, 0)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("About"))
	top.writeLine(dimStyle.Render("Connected server's build information"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	writeRow := func(label, value string) {
		middle.writeLine(labelStyle.Render(label))
		middle.writeLine(normalStyle.Render("    " + value))
		middle.writeBlank()
	}

	if a.activeConn == nil {
		middle.writeLine(dimStyle.Render("  No active server connection"))
	} else {
		a.activeConn.mu.RLock()
		serverVersion := a.activeConn.ServerVersion
		serverGitCommit := a.activeConn.ServerGitCommit
		serverBuildTime := a.activeConn.ServerBuildTime
		a.activeConn.mu.RUnlock()

		writeRow("Version", serverVersion)
		writeRow("Git Commit", serverGitCommit)
		writeRow("Build Time", serverBuildTime)
	}

	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(helpStyle.Render("Esc close"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

func (a *App) renderPluginsCategory(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 3, 1)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Plugins"))

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Drop a plugin's folder into Plugins/ and restart to install it"))

	enabledCount := 0
	for _, p := range s.PluginList {
		if p.Enabled {
			enabledCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statsStyle.Render(fmt.Sprintf("%d plugins · %d enabled", len(s.PluginList), enabledCount)))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	if len(s.PluginList) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(dimStyle.Render("  No plugins installed"))
	} else {
		for i, p := range s.PluginList {
			selected := s.FocusOnForm && i == s.SelectedPlugin

			prefix := "  "
			if selected {
				prefix = "▶ "
			}

			statusStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
			switch p.Status {
			case "running":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green))
			case "crashed":
				statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red))
			}

			toggle := "[ ]"
			if p.Enabled {
				toggle = "[x]"
			}

			line := fmt.Sprintf("%s v%s", pluginDisplayLabel(p.Name, p.Product), p.Version)
			status := statusStyle.Render(p.Status)

			var rendered string
			if selected {
				rendered = lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
					Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
					Bold(true).
					Width(layout.interiorWidth).
					Render(fmt.Sprintf("%s%s %s — %s", prefix, toggle, line, p.Status))
			} else {
				rendered = fmt.Sprintf("%s%s %s — %s", prefix, toggle, line, status)
			}
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-plugin-row:%d", i), rendered))

			if p.LastError != "" && (selected || p.Status == "crashed") {
				errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Italic(true)
				middle.writeLine(errStyle.Render("    ⚠ " + p.LastError))
			}
			// Reference-only for now -- there's no live version check or
			// update-in-place yet (see the item 10/13 7b scoping note in
			// the vault to-do); this just tells an admin where to look.
			if p.SourceURL != "" && selected {
				sourceStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Italic(true)
				middle.writeLine(sourceStyle.Render("    Source: " + p.SourceURL))
			}
		}
	}
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()

	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Esc close"))
	bottom.writeLine(helpStyle.Render("Actions: Enter configure · T toggle enabled"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderPluginConfigPage renders the Settings > Plugins > <name> config
// sub-page: one row per server_config_field declared in that plugin's
// manifest, using the same generic field renderer as the channel-creation
// form's plugin fields (text/number/boolean/select/channel_select).
func (a *App) renderPluginConfigPage(width, height int, s *ServerManagementState) string {
	state := s.PluginConfigState
	if state == nil {
		return ""
	}

	layout := calculateSettingsLayout(width, height, 2, 0)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	top.writeLine(headerStyle.Render("Configure " + pluginDisplayLabel(state.PluginID, state.Product)))

	subtitleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Server-wide settings for this plugin"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	if state.ErrorMsg != "" {
		errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true)
		middle.writeLine(errorStyle.Render("⚠ " + state.ErrorMsg))
		middle.writeBlank()
	}

	if len(state.Fields) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(dimStyle.Render("  This plugin has no server-wide settings."))
		middle.writeBlank()
	}

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	for i, f := range state.Fields {
		focused := state.FocusField == i
		required := ""
		if f.Required {
			required = " *"
		}
		middle.writeLine(labelStyle.Render("▸ " + f.Label + required + ":"))

		switch f.Type {
		case "text", "number":
			view := state.TextInputs[i].View()
			if focused {
				middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Render("  " + view))
			} else {
				middle.writeLine("  " + view)
			}
		default:
			valStyle := lipgloss.NewStyle()
			if focused {
				valStyle = valStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
			}
			val := state.Values[i]
			if val == "" {
				val = "(none)"
			}
			middle.writeLine(valStyle.Render("  ◂ " + val + " ▸"))
		}
		middle.writeBlank()
	}
	middle.pad()

	// ── BOTTOM SECTION ──
	saveField := len(state.Fields)
	backField := len(state.Fields) + 1

	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	var saveButton, backButton string
	if state.FocusField == saveField {
		saveButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Green)).
			Bold(true).Padding(0, 2).Render("Save")
	} else {
		saveButton = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Render("[Save]")
	}
	if state.FocusField == backField {
		backButton = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Comment)).
			Bold(true).Padding(0, 2).Render("Back")
	} else {
		backButton = lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render("[Back]")
	}
	bottom.writeLine("  " + saveButton + "    " + backButton)
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
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
	bannedCount := 0
	for _, member := range s.MemberList {
		if member.User != nil && member.User.Status == models.StatusOnline {
			onlineCount++
		}
		if member.IsBanned {
			bannedCount++
		}
	}
	statsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(statsStyle.Render(fmt.Sprintf("%d members · %d online · %d banned", len(s.MemberList), onlineCount, bannedCount)))

	// Filter bar or search input
	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	if s.SearchInputOpen {
		// Show search input
		searchPrompt := "Search: "
		searchText := s.SearchInputValue
		cursor := ""
		if len(searchText) == 0 {
			cursor = "_"
		}
		searchLine := searchPrompt + searchText + cursor
		searchStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan))
		top.writeLine(searchStyle.Render(searchLine + "  (Esc to cancel, Enter to apply)"))
	} else if s.SearchQuery != "" {
		// Show active search query
		filterText := fmt.Sprintf("Filters: Role: %s | Status: %s | Sort: %s | Search: \"%s\"",
			s.FilterRole, strings.Title(s.FilterOnline), s.SortBy, s.SearchQuery)
		top.writeLine(filterStyle.Render(filterText))
	} else {
		// Show normal filters
		filterText := fmt.Sprintf("Filters: Role: %s | Status: %s | Sort: %s",
			s.FilterRole, strings.Title(s.FilterOnline), s.SortBy)
		top.writeLine(filterStyle.Render(filterText))
	}
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

	// Table header
	tableHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true)
	headerLine := fmt.Sprintf("%-18s  ●  %-15s %-12s %-8s %-7s %-6s",
		"Username", "Role(s)", "Joined", "Banned", "Muted", "Kicks")
	middle.writeLine(tableHeaderStyle.Render(headerLine))

	// Table separator
	sepStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	middle.writeLine(sepStyle.Render(strings.Repeat("─", layout.interiorWidth)))

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

		// Selection indicator
		prefix := "  "
		if selected {
			prefix = "▶ "
		}

		// Status indicator
		statusDot := "○" // offline
		if member.User.Status == models.StatusOnline {
			statusDot = "●" // online
		} else if member.User.Status == models.StatusIdle {
			statusDot = "◑" // idle/AFK
		}

		// Role name (truncate if needed)
		roleName := "@everyone"
		if member.HighestRole != nil {
			roleName = member.HighestRole.Name
		}
		if len(roleName) > 15 {
			roleName = roleName[:12] + "..."
		}

		// Join date
		joinDate := "2026-02-22"
		if member.Member != nil && !member.Member.JoinedAt.IsZero() {
			joinDate = member.Member.JoinedAt.Format("2006-01-02")
		}

		// Moderation status
		bannedStr := "No"
		if member.IsBanned {
			bannedStr = "Yes"
		}
		mutedStr := "No"
		if member.IsMuted {
			mutedStr = "Yes"
		}
		kickStr := fmt.Sprintf("%d", member.KickCount)

		// Format table row: username  status  role  joined  banned  muted  kicks
		// Truncate username if needed to fit
		username := member.User.Username
		if len(username) > 18 {
			username = username[:15] + "..."
		}

		memberLine := fmt.Sprintf("%-18s  %s  %-15s %-12s %-8s %-7s %-6s",
			username, statusDot, roleName, joinDate, bannedStr, mutedStr, kickStr)

		var line string
		if selected {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(prefix + memberLine)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(prefix + memberLine)
		}
		middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-member-row:%d", i), line))
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
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Esc back"))
	bottom.writeLine(helpStyle.Render("Actions: Shift+R role · Shift+K kick · Shift+B ban/unban · Shift+M mute/unmute · F filter · S search"))

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
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderMembersFilterPage renders the filter configuration sub-page
func (a *App) renderMembersFilterPage(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 2, 0) // 4 top lines + 2 padding, 2 bottom lines

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render("Filter Members"))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Configure member list filters"))

	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Get available roles from active server
	var availableRoles []*models.Role
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			availableRoles = roles
		}
		a.activeConn.mu.RUnlock()
	}

	// Sort roles by position (descending, so highest position first)
	sort.Slice(availableRoles, func(i, j int) bool {
		return availableRoles[i].Position > availableRoles[j].Position
	})

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
		Bold(true)

	// Filter by Role
	middle.writeLine(labelStyle.Render("Filter by Role:"))
	middle.writeBlank()

	// "All" option
	allFocused := s.FilterPanelFocus == 0
	allText := "  ( ) All"
	if s.FilterRole == "All" {
		allText = "  (●) All"
	}
	if allFocused {
		middle.writeLine(zone.Mark("srvmgmt-filter-opt:0", selectedStyle.Render(allText)))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-filter-opt:0", normalStyle.Render(allText)))
	}

	// Individual role options
	roleIdx := 1
	for _, role := range availableRoles {
		if role.IsDefault {
			continue // Skip @everyone
		}
		roleFocused := s.FilterPanelFocus == roleIdx
		roleText := fmt.Sprintf("  ( ) %s", role.Name)
		if s.FilterRole == role.Name {
			roleText = fmt.Sprintf("  (●) %s", role.Name)
		}
		id := fmt.Sprintf("srvmgmt-filter-opt:%d", roleIdx)
		if roleFocused {
			middle.writeLine(zone.Mark(id, selectedStyle.Render(roleText)))
		} else {
			middle.writeLine(zone.Mark(id, normalStyle.Render(roleText)))
		}
		roleIdx++
	}

	middle.writeBlank()

	// Filter by Status
	statusBaseIdx := roleIdx
	middle.writeLine(labelStyle.Render("Filter by Status:"))
	middle.writeBlank()

	statuses := []struct {
		label string
		value string
	}{
		{"All", "all"},
		{"Online", "online"},
		{"Offline", "offline"},
	}

	for i, status := range statuses {
		statusSelected := s.FilterPanelFocus == statusBaseIdx+i
		statusText := fmt.Sprintf("  ( ) %s", status.label)
		if s.FilterOnline == status.value {
			statusText = fmt.Sprintf("  (●) %s", status.label)
		}
		id := fmt.Sprintf("srvmgmt-filter-opt:%d", statusBaseIdx+i)
		if statusSelected {
			middle.writeLine(zone.Mark(id, selectedStyle.Render(statusText)))
		} else {
			middle.writeLine(zone.Mark(id, normalStyle.Render(statusText)))
		}
	}

	middle.writeBlank()

	// Sort by
	sortBaseIdx := statusBaseIdx + len(statuses)
	middle.writeLine(labelStyle.Render("Sort by:"))
	middle.writeBlank()

	sortOptions := []struct {
		label string
		value string
	}{
		{"Username", "name"},
		{"Join Date", "joined"},
		{"Role", "role"},
		{"Kick Count", "kicks"},
	}

	for i, sortOpt := range sortOptions {
		sortSelected := s.FilterPanelFocus == sortBaseIdx+i
		sortText := fmt.Sprintf("  ( ) %s", sortOpt.label)
		if s.SortBy == sortOpt.value {
			sortText = fmt.Sprintf("  (●) %s", sortOpt.label)
		}
		id := fmt.Sprintf("srvmgmt-filter-opt:%d", sortBaseIdx+i)
		if sortSelected {
			middle.writeLine(zone.Mark(id, selectedStyle.Render(sortText)))
		} else {
			middle.writeLine(zone.Mark(id, normalStyle.Render(sortText)))
		}
	}

	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Enter apply · Esc cancel"))

	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(0, 1).
		Render(content)
}

// renderMembersRoleAssignPage renders the role assignment sub-page
func (a *App) renderMembersRoleAssignPage(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 2, 0) // 4 top lines + 2 padding, 2 bottom lines

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	memberName := "Unknown"
	if s.RoleAssignMember != nil && s.RoleAssignMember.User != nil {
		memberName = s.RoleAssignMember.User.Username
	}
	top.writeLine(headerStyle.Render(fmt.Sprintf("Assign Roles - %s", memberName)))

	// Subtitle
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Check/uncheck roles to assign or remove"))

	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// Get available roles from active server
	var availableRoles []*models.Role
	if a.activeConn != nil {
		serverID := a.getActiveServerID()
		a.activeConn.mu.RLock()
		if roles, ok := a.activeConn.Roles[serverID]; ok {
			availableRoles = roles
		}
		a.activeConn.mu.RUnlock()
	}

	// Sort roles by position (descending, so highest position first)
	sort.Slice(availableRoles, func(i, j int) bool {
		return availableRoles[i].Position > availableRoles[j].Position
	})

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
		Bold(true)
	commentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	middle.writeLine(labelStyle.Render("Available Roles:"))
	middle.writeBlank()

	// Render role checkboxes
	for i, role := range availableRoles {
		if role.IsDefault {
			// Show @everyone as unmodifiable
			roleText := fmt.Sprintf("  [✓] %s (default role)", role.Name)
			middle.writeLine(commentStyle.Render(roleText))
			continue
		}

		isFocused := i == s.RoleAssignFocus
		isChecked := s.RoleAssignSelections[role.ID]

		checkbox := "[ ]"
		if isChecked {
			checkbox = "[✓]"
		}

		roleColor := lipgloss.NewStyle().
			Foreground(lipgloss.Color(fmt.Sprintf("#%06x", role.Color)))

		roleText := fmt.Sprintf("  %s ", checkbox)
		roleName := roleColor.Render(role.Name)

		if isFocused {
			line := selectedStyle.Render(roleText) + " " + roleName
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-roleassign-row:%d", i), line))
		} else {
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-roleassign-row:%d", i), normalStyle.Render(roleText)+" "+roleName))
		}
	}

	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Space toggle · Enter save · Esc cancel"))

	bottom.pad()

	// ── ASSEMBLE ──
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(0, 1).
		Render(content)
}

// resolveChannelName looks up a channel name from the active server connection
func (a *App) resolveChannelName(channelID uuid.UUID) string {
	if a.activeConn == nil {
		return channelID.String()[:8]
	}
	serverID := a.getActiveServerID()
	a.activeConn.mu.RLock()
	defer a.activeConn.mu.RUnlock()
	if channels, ok := a.activeConn.Channels[serverID]; ok {
		for _, ch := range channels {
			if ch.ID == channelID {
				return ch.Name
			}
		}
	}
	return channelID.String()[:8]
}

// retentionOverrideSuffix describes what a channel override in the
// ChannelOverrides list actually does: "(exempt)" when every limit is nil
// (kept on prune, the original N-key-only behavior), or the specific
// custom limit(s) it sets otherwise -- these are the same underlying
// MessageRetentionPolicy shape, distinguished only by which fields are set.
func retentionOverrideSuffix(override *models.MessageRetentionPolicy) string {
	if override.TimeRetentionDays == nil && override.SystemTimeRetentionDays == nil && override.MaxMessageCount == nil {
		return "(exempt)"
	}
	var parts []string
	if override.TimeRetentionDays != nil {
		parts = append(parts, fmt.Sprintf("%dd", *override.TimeRetentionDays))
	}
	if override.SystemTimeRetentionDays != nil {
		parts = append(parts, fmt.Sprintf("sys %dd", *override.SystemTimeRetentionDays))
	}
	if override.MaxMessageCount != nil {
		parts = append(parts, fmt.Sprintf("%d msgs", *override.MaxMessageCount))
	}
	return "(" + strings.Join(parts, ", ") + ")"
}

// renderMessagesCategory renders the Messages/Retention management category
func (a *App) renderMessagesCategory(width, height int, s *ServerManagementState) string {
	layout := calculateSettingsLayout(width, height, 2, 1)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	top.writeLine(headerStyle.Render("Message Retention Settings"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(descStyle.Render("Configure retention policies · exempt channels from pruning"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE SECTION ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	sectionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	valueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))

	// Server Default Policy
	middle.writeLine(sectionStyle.Render("Server Default Policy"))
	if s.RetentionPolicy == nil {
		middle.writeLine(dimStyle.Render("  No policy set — messages kept indefinitely"))
	} else {
		p := s.RetentionPolicy
		if p.TimeRetentionDays != nil {
			middle.writeLine(valueStyle.Render(fmt.Sprintf("  Time-based:    %d days", *p.TimeRetentionDays)))
		} else {
			middle.writeLine(dimStyle.Render("  Time-based:    disabled"))
		}
		if p.SystemTimeRetentionDays != nil {
			middle.writeLine(valueStyle.Render(fmt.Sprintf("  System msgs:   %d days", *p.SystemTimeRetentionDays)))
		} else {
			middle.writeLine(dimStyle.Render("  System msgs:   disabled"))
		}
		if p.MaxMessageCount != nil {
			middle.writeLine(valueStyle.Render(fmt.Sprintf("  Max per chan:   %d messages", *p.MaxMessageCount)))
		} else {
			middle.writeLine(dimStyle.Render("  Max per chan:   disabled"))
		}
	}
	middle.writeBlank()

	// Channel Overrides -- either a full exemption (all limits nil, "kept on
	// prune") or a custom per-channel limit that differs from the server
	// default. Both are stored the same way (a MessageRetentionPolicy row
	// with a non-nil ChannelID); only the label distinguishes them.
	middle.writeLine(sectionStyle.Render("Channel Overrides"))
	if len(s.ChannelOverrides) == 0 {
		middle.writeLine(dimStyle.Render("  No channel overrides configured"))
	} else {
		for i, override := range s.ChannelOverrides {
			selected := s.FocusOnForm && i == s.SelectedOverride
			chName := "unknown"
			if override.ChannelID != nil {
				chName = a.resolveChannelName(*override.ChannelID)
			}
			suffix := retentionOverrideSuffix(override)
			label := fmt.Sprintf("  # %s %s", chName, suffix)
			if selected {
				line := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
					Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
					Bold(true).
					Width(layout.interiorWidth).
					Render(fmt.Sprintf("▶ # %s %s", chName, suffix))
				middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-override-row:%d", i), line))
			} else {
				middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-override-row:%d", i), dimStyle.Render(label)))
			}
		}
	}
	middle.writeBlank()

	// Actions
	middle.writeLine(sectionStyle.Render("Actions"))
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(actionStyle.Render("  E · Edit selected override, or server default if none selected"))
	middle.writeLine(actionStyle.Render("  N · Exempt a channel from pruning"))
	middle.writeLine(actionStyle.Render("  D · Remove selected channel override"))
	middle.writeLine(actionStyle.Render("  P · Run manual prune now"))
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("↑↓ select exempt channel · Tab focus · Esc back"))
	bottom.writeLine(helpStyle.Render("E edit policy · N exempt channel · D remove · P prune"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderRetentionFormPage renders the retention policy editor -- either the
// server default or one channel's override, per form.Mode -- as a full
// settings page.
func (a *App) renderRetentionFormPage(width, height int, s *ServerManagementState) string {
	form := s.RetentionFormState
	if form == nil {
		return ""
	}

	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + blank + 2 help lines = 4 lines → pageBottomExtra = 1
	layout := calculateSettingsLayout(width, height, 2, 1)

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	scopeLabel := "Editing: server default"
	if form.Mode == "channel" && form.ChannelID != nil {
		scopeLabel = fmt.Sprintf("Editing: #%s", a.resolveChannelName(*form.ChannelID))
	}
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render(scopeLabel))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("Leave fields empty to disable the limit"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	labelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)

	renderField := func(label, value string, fieldIdx int) {
		middle.writeLine(labelStyle.Render(label))
		display := fmt.Sprintf("  [%s]", value)
		id := fmt.Sprintf("srvmgmt-retention-field:%d", fieldIdx)
		if form.FocusField == fieldIdx {
			middle.writeLine(zone.Mark(id, selectedStyle.Render(display)))
		} else {
			middle.writeLine(zone.Mark(id, normalStyle.Render(display)))
		}
		middle.writeBlank()
	}

	renderField("Time Retention (days):", form.TimeRetentionDays, 0)
	renderField("System Message Retention (days):", form.SystemTimeRetentionDays, 1)
	renderField("Max Messages Per Channel:", form.MaxMessageCount, 2)

	saveBtn := zone.Mark("srvmgmt-retention-save", lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Bold(true).Render("[S] Save"))
	cancelBtn := zone.Mark("srvmgmt-retention-cancel", lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Render("[Esc] Cancel"))
	middle.writeLine(fmt.Sprintf("%s  %s", saveBtn, cancelBtn))
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	helpStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeBlank()
	bottom.writeLine(helpStyle.Render("Tab / Shift+Tab · navigate fields"))
	bottom.writeLine(helpStyle.Render("[S] save · [Esc] cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderPruneConfirmPage renders the Confirm Message Pruning as a full settings page
func (a *App) renderPruneConfirmPage(width, height int, s *ServerManagementState) string {
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + 1 help line = 2 lines → pageBottomExtra = 0
	layout := calculateSettingsLayout(width, height, 2, 0)

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow)).Bold(true).
		Render("⚠ Confirm Message Pruning"))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("This action is permanent and cannot be undone"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	bodyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	middle.writeLine(bodyStyle.Render("This will permanently delete old messages according to the"))
	middle.writeLine(bodyStyle.Render("configured retention policies across all channels."))
	middle.writeBlank()
	middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).
		Render("Pinned messages will be preserved."))
	middle.writeBlank()
	middle.writeLine(bodyStyle.Render("Are you sure you want to continue?"))
	middle.writeBlank()
	confirmBtn := zone.Mark("srvmgmt-prune-confirm-yes", lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Bold(true).Render("[Enter] Yes, prune now"))
	cancelBtn := zone.Mark("srvmgmt-prune-confirm-cancel", lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Render("[Esc] Cancel"))
	middle.writeLine(fmt.Sprintf("%s  %s", confirmBtn, cancelBtn))
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("[Enter] confirm prune · [Esc] cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderChannelPickerPage renders the exempt channel picker as a full settings page
func (a *App) renderChannelPickerPage(width, height int, s *ServerManagementState) string {
	// Top: header + subtitle + blank + separator = 4 lines → pageTopExtra = 2
	// Bottom: separator + 1 help line = 2 lines → pageBottomExtra = 0
	layout := calculateSettingsLayout(width, height, 2, 0)

	// ── TOP ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true).
		Render("Exempt Channel from Pruning"))
	top.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("Selected channel will keep all messages when a prune is executed"))
	top.writeBlank()
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ── MIDDLE ──
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)
	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))

	if len(s.OverrideChannelList) == 0 {
		middle.writeLine(dimStyle.Render("  No channels available to exempt"))
	} else {
		for i, ch := range s.OverrideChannelList {
			line := fmt.Sprintf("  # %s", ch.Name)
			id := fmt.Sprintf("srvmgmt-exempt-channel-row:%d", i)
			if i == s.OverrideChannelSelected {
				middle.writeLine(zone.Mark(id, selectedStyle.Render(line)))
			} else {
				middle.writeLine(zone.Mark(id, normalStyle.Render(line)))
			}
		}
	}
	middle.pad()

	// ── BOTTOM ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	bottom.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("↑↓ navigate · [Enter] exempt channel · [Esc] cancel"))
	bottom.pad()

	content := lipgloss.JoinVertical(lipgloss.Left, top.String(), middle.String(), bottom.String())
	return lipgloss.NewStyle().
		Width(width).Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).Render(content)
}

// renderChannelFormPage renders the channel create/edit form as a full page
func (a *App) renderChannelFormPage(width, height int, s *ServerManagementState) string {
	state := s.ChannelFormState
	if state == nil {
		return ""
	}

	// Use same layout calculation as channel list
	layout := calculateSettingsLayout(width, height, 2, 0) // 2 = 2 padding lines, 0 = bottom is correct

	// Determine layout vars
	formLayout := computeChannelFormLayout(state)
	isVoice := formLayout.isVoice
	pluginTypeLocked := state.Mode == "edit" && state.OriginalType == models.ChannelTypePlugin
	submitField := formLayout.submitField
	cancelField := formLayout.cancelField

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	title := "Create Channel"
	if state.Mode == "edit" {
		switch {
		case pluginTypeLocked:
			title = "Edit " + state.PluginDisplayLabel + " Channel"
		case state.TypeIndex == 1:
			title = "Edit Voice Channel"
		case state.TypeIndex == 2:
			title = "Edit Channel Group"
		default:
			title = "Edit Text Channel"
		}
	}
	top.writeLine(headerStyle.Render(title))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	subtitle := "Create a text channel, voice channel, channel group, or plugin channel"
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
		middle.writeLine(zone.Mark("srvmgmt-channelform-name", inputStyle.Render("  "+inputView)))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-channelform-name", "  "+inputView))
	}
	middle.writeBlank()

	// Type selection
	middle.writeLine(labelStyle.Render("▸ Type:"))

	if pluginTypeLocked {
		lockedStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(lockedStyle.Render("  " + state.PluginDisplayLabel + " (type cannot be changed)"))
	} else {
		typeStyle := lipgloss.NewStyle()
		if state.FocusField == 1 {
			typeStyle = typeStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
		}

		typeOptions := []struct {
			label string
			idx   int
			hint  string
		}{
			{"Text Channel", 0, "# Text-only messaging"},
			{"Voice Channel", 1, "♪ Voice + text messaging"},
			{"Channel Group", 2, "▼ Groups channels together"},
		}
		for _, opt := range typeOptions {
			radio := "( ) "
			if state.TypeIndex == opt.idx {
				radio = "(●) "
			}
			hint := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Render("  " + opt.hint)
			writeZoneMarkedLines(middle, fmt.Sprintf("srvmgmt-channelform-type:%d", opt.idx),
				typeStyle.Render("  "+radio+opt.label), hint)
		}
		// Plugin-provided kinds, advertised at READY — a folder dropped into
		// Plugins/ shows up here with zero changes to this rendering code.
		for i, kind := range a.pluginKindOptions() {
			idx := 3 + i
			radio := "( ) "
			if state.TypeIndex == idx {
				radio = "(●) "
			}
			icon := kind.Icon
			if icon == "" {
				icon = "▤"
			}
			hint := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Render(fmt.Sprintf("  %s Plugin channel", icon))
			writeZoneMarkedLines(middle, fmt.Sprintf("srvmgmt-channelform-type:%d", idx),
				typeStyle.Render("  "+radio+kind.DisplayName), hint)
		}
	}
	middle.writeBlank()

	// Max Users field — voice channels only
	if isVoice {
		middle.writeLine(labelStyle.Render("▸ Max Users (0 = unlimited):"))
		maxUsersView := state.MaxUsersInput.View()
		if state.FocusField == formLayout.maxUsersField {
			middle.writeLine(zone.Mark("srvmgmt-channelform-maxusers", lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
				Render("  "+maxUsersView)))
		} else {
			middle.writeLine(zone.Mark("srvmgmt-channelform-maxusers", "  "+maxUsersView))
		}
		middle.writeBlank()
	}

	// Plugin-declared create_fields, one per manifest field — the same
	// generic field renderer will be reused for Settings > Plugins config.
	if formLayout.isPlugin {
		for i, f := range state.PluginFields {
			focused := state.FocusField == formLayout.pluginStart+i
			fieldLabelStyle := labelStyle
			required := ""
			if f.Required {
				required = " *"
			}
			middle.writeLine(fieldLabelStyle.Render("▸ " + f.Label + required + ":"))

			switch f.Type {
			case "text", "number":
				view := state.PluginTextInputs[i].View()
				if focused {
					middle.writeLine(lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Render("  " + view))
				} else {
					middle.writeLine("  " + view)
				}
			default: // boolean, select, channel_select
				valStyle := lipgloss.NewStyle()
				if focused {
					valStyle = valStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
				}
				val := state.PluginValues[i]
				if val == "" {
					val = "(none)"
				}
				middle.writeLine(valStyle.Render("  ◂ " + val + " ▸"))
			}
			middle.writeBlank()
		}
	}

	// Show parent group if applicable (text/voice only, categories and plugin channels are always top-level)
	if state.TypeIndex != 2 && !formLayout.isPlugin && state.CategoryID != nil && a.activeConn != nil {
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
	if state.FocusField == submitField {
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

	if state.FocusField == cancelField {
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
	createButton = zone.Mark("srvmgmt-channelform-submit", createButton)
	cancelButton = zone.Mark("srvmgmt-channelform-cancel", cancelButton)

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
	bottom.writeLine(helpStyle.Render("Tab/Shift+Tab: Navigate · ↑↓←→: Select type · Enter: Submit · Esc: Cancel"))

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
		Height(height - 2).
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
		middle.writeLine(zone.Mark("srvmgmt-move-row:0", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
			Bold(true).
			Width(layout.interiorWidth).
			Render("▶ Top Level (no group)")))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-move-row:0", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Render(topLevelText)))
	}

	// Category list
	for i, category := range state.CategoryList {
		categoryText := fmt.Sprintf("  ▼ %s", category.Name)
		listIndex := i + 1 // +1 because 0 is "Top Level"
		id := fmt.Sprintf("srvmgmt-move-row:%d", listIndex)

		if state.SelectedIndex == listIndex {
			middle.writeLine(zone.Mark(id, lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth).
				Render(fmt.Sprintf("▶ ▼ %s", category.Name))))
		} else {
			middle.writeLine(zone.Mark(id, lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(categoryText)))
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
		Height(height - 2).
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
			Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
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

// renderDialogButton renders a button with consistent styling. id is used to
// zone.Mark the rendered button so a click can resolve which button was
// pressed -- shared by every confirm dialog built on this helper (Kick,
// Ban/Unban, Unmute), so this one change covers all of them.
func (a *App) renderDialogButton(id, label string, focused bool, color lipgloss.Color, destructive bool) string {
	var rendered string
	if focused {
		// Focused button: filled background
		rendered = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(color).
			Bold(true).
			Padding(0, 2).
			Render(label)
	} else if destructive {
		// Unfocused destructive button: colored text, no border
		rendered = lipgloss.NewStyle().
			Foreground(color).
			Padding(0, 2).
			Render(label)
	} else {
		// Unfocused normal button: normal text, no border
		rendered = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Padding(0, 2).
			Render(label)
	}
	return zone.Mark(id, rendered)
}

// renderKickConfirmDialog renders the kick confirmation dialog
func (a *App) renderKickConfirmDialog() string {
	s := a.serverManagementState
	dialogWidth := 50

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("▲  Confirm Kick"))
	content.WriteString("\n\n")

	// Message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	memberName := "Unknown"
	if s.KickConfirmMember != nil && s.KickConfirmMember.User != nil {
		memberName = s.KickConfirmMember.User.Username
	}

	content.WriteString(msgStyle.Render(fmt.Sprintf("Kick %s from the server?\n\nThey can rejoin with an invite.", memberName)))
	content.WriteString("\n\n")

	// Buttons
	confirmBtn := a.renderDialogButton("kick-confirm-yes", "Yes, Kick", s.KickConfirmFocusedBtn == 0, lipgloss.Color(a.theme.Colors.Red), true)
	cancelBtn := a.renderDialogButton("kick-confirm-cancel", "No, Cancel", s.KickConfirmFocusedBtn == 1, lipgloss.Color(a.theme.Colors.Comment), false)

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
	buttonRowStyle := lipgloss.NewStyle().Width(dialogWidth - 4).Align(lipgloss.Center)
	content.WriteString(buttonRowStyle.Render(buttonRow))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Tab/←→] Switch  [Enter] Confirm  [Y] Yes  [N/Esc] Cancel"))

	// Wrap in dialog box
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

// renderBanConfirmDialog renders the ban/unban confirmation dialog
func (a *App) renderBanConfirmDialog() string {
	s := a.serverManagementState
	dialogWidth := 50

	var content strings.Builder

	memberName := "Unknown"
	isBanned := false
	if s.BanConfirmMember != nil && s.BanConfirmMember.User != nil {
		memberName = s.BanConfirmMember.User.Username
		isBanned = s.BanConfirmMember.IsBanned
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	if isBanned {
		content.WriteString(titleStyle.Render("▲  Confirm Unban"))
	} else {
		content.WriteString(titleStyle.Render("▲  Confirm Ban"))
	}
	content.WriteString("\n\n")

	// Message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	if isBanned {
		content.WriteString(msgStyle.Render(fmt.Sprintf("Unban %s?\n\nThey will be able to rejoin the server.", memberName)))
	} else {
		content.WriteString(msgStyle.Render(fmt.Sprintf("Ban %s from the server?\n\nThey will not be able to rejoin.", memberName)))
	}
	content.WriteString("\n\n")

	// Buttons
	var confirmBtn, cancelBtn string
	if isBanned {
		confirmBtn = a.renderDialogButton("ban-confirm-yes", "Yes, Unban", s.BanConfirmFocusedBtn == 0, lipgloss.Color(a.theme.Colors.Green), true)
	} else {
		confirmBtn = a.renderDialogButton("ban-confirm-yes", "Yes, Ban", s.BanConfirmFocusedBtn == 0, lipgloss.Color(a.theme.Colors.Red), true)
	}
	cancelBtn = a.renderDialogButton("ban-confirm-cancel", "No, Cancel", s.BanConfirmFocusedBtn == 1, lipgloss.Color(a.theme.Colors.Comment), false)

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
	buttonRowStyle := lipgloss.NewStyle().Width(dialogWidth - 4).Align(lipgloss.Center)
	content.WriteString(buttonRowStyle.Render(buttonRow))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Tab/←→] Switch  [Enter] Confirm  [Y] Yes  [N/Esc] Cancel"))

	// Wrap in dialog box
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

// renderUnmuteConfirmDialog renders the unmute confirmation dialog
func (a *App) renderUnmuteConfirmDialog() string {
	s := a.serverManagementState
	dialogWidth := 50

	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Green)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("▲  Confirm Unmute"))
	content.WriteString("\n\n")

	// Message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	memberName := "Unknown"
	if s.UnmuteConfirmMember != nil && s.UnmuteConfirmMember.User != nil {
		memberName = s.UnmuteConfirmMember.User.Username
	}

	content.WriteString(msgStyle.Render(fmt.Sprintf("Unmute %s?\n\nThey will be able to send messages again.", memberName)))
	content.WriteString("\n\n")

	// Buttons
	confirmBtn := a.renderDialogButton("unmute-confirm-yes", "Yes, Unmute", s.UnmuteConfirmFocusedBtn == 0, lipgloss.Color(a.theme.Colors.Green), true)
	cancelBtn := a.renderDialogButton("unmute-confirm-cancel", "No, Cancel", s.UnmuteConfirmFocusedBtn == 1, lipgloss.Color(a.theme.Colors.Comment), false)

	buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
	buttonRowStyle := lipgloss.NewStyle().Width(dialogWidth - 4).Align(lipgloss.Center)
	content.WriteString(buttonRowStyle.Render(buttonRow))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Tab/←→] Switch  [Enter] Confirm  [Y] Yes  [N/Esc] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Green)).
		Padding(1, 2).
		Width(dialogWidth)

	dialog := dialogStyle.Render(content.String())

	return lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)
}

// renderMuteDurationPage renders the mute duration selection page
func (a *App) renderMuteDurationPage() string {
	s := a.serverManagementState
	dialogWidth := 60

	var content strings.Builder

	memberName := "Unknown"
	if s.MuteDurationMember != nil && s.MuteDurationMember.User != nil {
		memberName = s.MuteDurationMember.User.Username
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Yellow)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render(fmt.Sprintf("Mute %s", memberName)))
	content.WriteString("\n\n")

	// Duration options
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	content.WriteString(labelStyle.Render("Select Duration:"))
	content.WriteString("\n\n")

	normalStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
		Bold(true)

	options := []string{"1 Hour", "24 Hours", "7 Days", "Permanent"}
	for i, opt := range options {
		line := fmt.Sprintf("  ( ) %s", opt)
		id := fmt.Sprintf("srvmgmt-mute-duration-row:%d", i)
		if i == s.MuteDurationFocus {
			content.WriteString(zone.Mark(id, selectedStyle.Render(line)))
		} else {
			content.WriteString(zone.Mark(id, normalStyle.Render(line)))
		}
		content.WriteString("\n")
	}

	content.WriteString("\n")
	content.WriteString(labelStyle.Render("Custom Duration:"))
	content.WriteString("\n")
	customLine := fmt.Sprintf("  [%s]  (e.g., 30m, 5h, 2d)", s.MuteDurationCustom)
	if s.MuteDurationFocus == 4 {
		content.WriteString(zone.Mark("srvmgmt-mute-duration-row:4", selectedStyle.Render(customLine)))
	} else {
		content.WriteString(zone.Mark("srvmgmt-mute-duration-row:4", normalStyle.Render(customLine)))
	}
	content.WriteString("\n\n")

	content.WriteString(labelStyle.Render("Reason (optional):"))
	content.WriteString("\n")
	reasonLine := fmt.Sprintf("  [%s]", s.MuteDurationReason)
	if s.MuteDurationFocus == 5 {
		content.WriteString(zone.Mark("srvmgmt-mute-duration-row:5", selectedStyle.Render(reasonLine)))
	} else {
		content.WriteString(zone.Mark("srvmgmt-mute-duration-row:5", normalStyle.Render(reasonLine)))
	}
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)
	content.WriteString(helpStyle.Render("[↑↓] Navigate  [Enter] Confirm  [Esc] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Yellow)).
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

	content.WriteString(titleStyle.Render("▲  Confirm Deletion"))
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

	// Buttons (with focus support)
	buttonRowStyle := lipgloss.NewStyle().
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	// Button 0: Yes, Delete (focused = filled background, unfocused = normal text)
	var confirmBtn string
	if s.DeleteConfirmFocusedButton == 0 {
		confirmBtn = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Red)).
			Bold(true).
			Padding(0, 2).
			Render("Yes, Delete")
	} else {
		confirmBtn = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Padding(0, 2).
			Render("Yes, Delete")
	}
	confirmBtn = zone.Mark("srvmgmt-delete-confirm-yes", confirmBtn)

	// Button 1: No, Cancel (focused = filled background, unfocused = normal text)
	var cancelBtn string
	if s.DeleteConfirmFocusedButton == 1 {
		cancelBtn = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Background)).
			Background(lipgloss.Color(a.theme.Colors.Comment)).
			Bold(true).
			Padding(0, 2).
			Render("No, Cancel")
	} else {
		cancelBtn = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Padding(0, 2).
			Render("No, Cancel")
	}
	cancelBtn = zone.Mark("srvmgmt-delete-confirm-cancel", cancelBtn)

	buttons := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
	content.WriteString(buttonRowStyle.Render(buttons))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)
	content.WriteString(helpStyle.Render("[Tab/←→] Switch  [Enter] Confirm  [Y] Yes  [N/Esc] Cancel"))

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

// renderRoleFormPage renders the role create/edit form as a full page
func (a *App) renderRoleFormPage(width, height int, s *ServerManagementState) string {
	state := s.RoleFormState
	if state == nil {
		return ""
	}

	layout := calculateSettingsLayout(width, height, 2, 0)

	// ── TOP SECTION ──
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	title := "Create New Role"
	if state.Mode == "edit" {
		title = "Edit Role"
	}
	top.writeLine(headerStyle.Render(title))

	// Subtitle (optional)
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	subtitle := "Configure role permissions and appearance"
	top.writeLine(subtitleStyle.Render(subtitle))
	top.writeBlank()

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

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)

	// Field 0: Role Name
	middle.writeLine(labelStyle.Render("▸ Role Name:"))
	nameDisplay := state.NameInput
	if nameDisplay == "" {
		nameDisplay = "[]"
	}
	if state.FocusField == 0 {
		// Show cursor
		before := nameDisplay[:state.NameCursor]
		after := nameDisplay[state.NameCursor:]
		nameDisplay = before + "█" + after
		middle.writeLine(zone.Mark("srvmgmt-roleform-name", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render("  "+nameDisplay)))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-roleform-name", "  "+nameDisplay))
	}
	middle.writeBlank()

	// Field 1: Permission Preset
	middle.writeLine(labelStyle.Render("▸ Permission Preset:"))
	presets := []string{
		"Members - Basic chat permissions",
		"Moderator - Moderation + chat",
		"Admin - All permissions",
		"Custom - Select manually",
	}
	for i, preset := range presets {
		prefix := "  ( ) "
		if state.PresetIndex == i {
			prefix = "  (●) "
		}
		line := prefix + preset
		id := fmt.Sprintf("srvmgmt-roleform-preset:%d", i)
		if state.FocusField == 1 {
			middle.writeLine(zone.Mark(id, lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
				Render(line)))
		} else {
			middle.writeLine(zone.Mark(id, line))
		}
	}
	middle.writeBlank()

	// Field 2: Color
	middle.writeLine(labelStyle.Render("▸ Color:"))
	colors := []struct {
		name  string
		value string
	}{
		{"Gold", a.theme.Colors.Yellow},
		{"Blue", a.theme.Colors.Cyan},
		{"Red", a.theme.Colors.Red},
		{"Green", a.theme.Colors.Green},
		{"Purple", a.theme.Colors.Purple},
	}
	var colorLine string
	for i, color := range colors {
		prefix := " ○ "
		if state.ColorIndex == i {
			prefix = " ● "
		}
		coloredCircle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(color.value)).
			Render(prefix)

		// Highlight the selected color name when focused
		colorName := color.name
		if state.FocusField == 2 && state.ColorIndex == i {
			colorName = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
				Render(colorName)
		}

		colorLine += zone.Mark(fmt.Sprintf("srvmgmt-roleform-color:%d", i), coloredCircle+colorName)
	}
	middle.writeLine("  " + colorLine)
	middle.writeBlank()

	// Field 3: IsHoisted
	hoistedPrefix := "  ☐ "
	if state.IsHoisted {
		hoistedPrefix = "  ☑ "
	}
	hoistedLine := hoistedPrefix + "Show separately in members list"
	if state.FocusField == 3 {
		middle.writeLine(zone.Mark("srvmgmt-roleform-hoisted", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render(hoistedLine)))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-roleform-hoisted", hoistedLine))
	}

	// Field 4: IsMentionable
	mentionablePrefix := "  ☐ "
	if state.IsMentionable {
		mentionablePrefix = "  ☑ "
	}
	mentionableLine := mentionablePrefix + "Allow anyone to @mention this role"
	if state.FocusField == 4 {
		middle.writeLine(zone.Mark("srvmgmt-roleform-mentionable", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render(mentionableLine)))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-roleform-mentionable", mentionableLine))
	}
	middle.writeBlank()

	// Field 5: Display Order
	middle.writeLine(labelStyle.Render("▸ Display Order (member panel sorting):"))
	orderDisplay := state.DisplayOrder
	if orderDisplay == "" {
		orderDisplay = "[0]"
	}
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)
	helpText := helpStyle.Render("  (Lower numbers appear first: 1=top, 2=next, etc.)")

	if state.FocusField == 5 {
		before := orderDisplay[:state.DisplayOrderCursor]
		after := orderDisplay[state.DisplayOrderCursor:]
		orderDisplay = before + "█" + after
		middle.writeLine(zone.Mark("srvmgmt-roleform-order", lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Render("  "+orderDisplay)+helpText))
	} else {
		middle.writeLine(zone.Mark("srvmgmt-roleform-order", "  "+orderDisplay+helpText))
	}
	middle.writeBlank()

	// Buttons
	createLabel := "Create"
	if state.Mode == "edit" {
		createLabel = "Save"
	}

	var createButton, cancelButton string
	if state.FocusField == 6 {
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

	if state.FocusField == 7 {
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

	createButton = zone.Mark("srvmgmt-roleform-submit", createButton)
	cancelButton = zone.Mark("srvmgmt-roleform-cancel", cancelButton)
	buttonsLine := "  " + createButton + "  " + cancelButton
	middle.writeLine(buttonsLine)

	// Fill remaining middle section space
	middle.pad()

	// ── BOTTOM SECTION ──
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(footerStyle.Render("Tab/↑↓: Navigate · ←→: Color · Space: Toggle · Enter: Submit · Esc: Cancel"))

	// Fill remaining bottom section space
	bottom.pad()

	// Assemble sections
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

// renderPermissionsEditorPage renders the permissions editor as a full settings page
func (a *App) renderPermissionsEditorPage(width, height int, s *ServerManagementState) string {
	if s.PermissionsEditorRole == nil {
		return "No role selected"
	}

	// ═══════════════════════════════════════════════════════════════
	// STEP 1: Calculate Layout
	// ═══════════════════════════════════════════════════════════════
	// Top: header + subtitle + blank + description + commands + blank + separator = 7 lines + 2 padding
	// Bottom: separator + blank + 2 help lines = 4 lines
	layout := calculateSettingsLayout(width, height, 5, 1)

	// ═══════════════════════════════════════════════════════════════
	// STEP 2: Build TOP Section
	// ═══════════════════════════════════════════════════════════════
	top := newSectionBuilder(layout.topLines, layout.interiorWidth)

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Bold(true)
	top.writeLine(headerStyle.Render(fmt.Sprintf("Edit Permissions: %s", s.PermissionsEditorRole.Name)))

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	top.writeLine(subtitleStyle.Render("Configure role permissions and appearance"))

	// Blank line
	top.writeBlank()

	// Get selected permission details
	permList := getPermissionList()
	selectedPermName := ""
	if s.PermSelectedIndex >= 0 && s.PermSelectedIndex < len(permList) {
		selectedPermName = permList[s.PermSelectedIndex].Name
	}

	// Description of selected permission
	if selectedPermName != "" {
		description, commands := getPermissionDescription(selectedPermName)

		descStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground))
		top.writeLine(descStyle.Render(description))

		// Commands/examples
		if commands != "" {
			cmdStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Italic(true)
			top.writeLine(cmdStyle.Render(commands))
		} else {
			top.writeBlank()
		}
	} else {
		// Fallback if no permission selected
		top.writeLine("Select a permission to see its description")
		top.writeBlank()
	}

	// Blank line before separator
	top.writeBlank()

	// Separator
	top.writeLine(a.renderSeparator(layout.interiorWidth))

	// ═══════════════════════════════════════════════════════════════
	// STEP 3: Build MIDDLE Section (Scrollable Permissions List)
	// ═══════════════════════════════════════════════════════════════
	middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

	// permList already declared in top section, reuse it here

	// Calculate scrollable viewport
	maxVisible := layout.middleLines - 2 // Leave room for padding
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Calculate scroll window to keep selected item centered
	visibleStart := 0
	visibleEnd := len(permList)

	if len(permList) > maxVisible {
		halfVisible := maxVisible / 2
		visibleStart = s.PermSelectedIndex - halfVisible
		visibleEnd = s.PermSelectedIndex + halfVisible

		if visibleStart < 0 {
			visibleStart = 0
			visibleEnd = maxVisible
		}
		if visibleEnd > len(permList) {
			visibleEnd = len(permList)
			visibleStart = visibleEnd - maxVisible
			if visibleStart < 0 {
				visibleStart = 0
			}
		}
	}

	// Show "↑ X more" if scrolled down
	if visibleStart > 0 {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↑ %d more", visibleStart)))
	}

	// Render permissions grouped by category
	currentCategory := ""
	for i := visibleStart; i < visibleEnd; i++ {
		perm := permList[i]

		// Category header
		if perm.Category != currentCategory {
			currentCategory = perm.Category
			categoryStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment))
			middle.writeLine(categoryStyle.Render(fmt.Sprintf("── %s ──", currentCategory)))
		}

		// Check if this permission is enabled
		isEnabled := (s.PermModifiedBits & uint64(perm.Bit)) != 0
		checkbox := "[ ]"
		if isEnabled {
			checkbox = "[✓]"
		}

		// Selection indicator
		prefix := "  "
		if i == s.PermSelectedIndex {
			prefix = "▶ "
		}

		// Build line
		line := fmt.Sprintf("%s%s %s", prefix, checkbox, perm.Name)

		// Style based on selection
		if i == s.PermSelectedIndex {
			selectedStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
				Bold(true).
				Width(layout.interiorWidth)
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-perm-row:%d", i), selectedStyle.Render(line)))
		} else {
			normalStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground))
			middle.writeLine(zone.Mark(fmt.Sprintf("srvmgmt-perm-row:%d", i), normalStyle.Render(line)))
		}
	}

	// Show "↓ X more" if not at bottom
	if visibleEnd < len(permList) {
		moreStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		middle.writeLine(moreStyle.Render(fmt.Sprintf("  ↓ %d more", len(permList)-visibleEnd)))
	}

	// Fill remaining space
	middle.pad()

	// ═══════════════════════════════════════════════════════════════
	// STEP 4: Build BOTTOM Section (Help Text)
	// ═══════════════════════════════════════════════════════════════
	bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

	// Separator
	bottom.writeLine(a.renderSeparator(layout.interiorWidth))

	// Blank line
	bottom.writeBlank()

	// Help text (2 lines)
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))
	bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Space: toggle · A: all · N: none"))
	bottom.writeLine(helpStyle.Render("Actions: Enter: save · Esc: cancel (discard changes)"))

	// Fill remaining space
	bottom.pad()

	// ═══════════════════════════════════════════════════════════════
	// STEP 5: Assemble All Sections
	// ═══════════════════════════════════════════════════════════════
	content := lipgloss.JoinVertical(lipgloss.Left,
		top.String(),
		middle.String(),
		bottom.String(),
	)

	// ═══════════════════════════════════════════════════════════════
	// STEP 6: Render with Border and Padding
	// ═══════════════════════════════════════════════════════════════
	return lipgloss.NewStyle().
		Width(width).
		Height(height - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Padding(0, 1).
		Render(content)
}

func (a *App) handleRoleFormSubmit() tea.Cmd {
	state := a.serverManagementState.RoleFormState
	if state == nil {
		return nil
	}

	// Validation
	name := strings.TrimSpace(state.NameInput)
	if name == "" {
		state.ErrorMsg = "Role name is required"
		return nil
	}
	if len(name) > 50 {
		state.ErrorMsg = "Role name must be 1-50 characters"
		return nil
	}

	// Connection check
	if a.activeConn == nil || a.currentServer == nil {
		state.ErrorMsg = "Not connected to server"
		return nil
	}

	// Map preset to permissions
	var permissions uint64
	switch state.PresetIndex {
	case 0: // Text Only
		permissions = uint64(models.PermissionsText)
	case 1: // Moderator
		permissions = uint64(models.PermissionsModerator)
	case 2: // Admin
		permissions = uint64(models.PermissionsAdmin)
	case 3: // Custom
		permissions = state.CustomPermissions
	}

	// Map color index to RGB value
	colorValues := []int{
		0xFFD700, // Gold
		0x5865F2, // Blue
		0xED4245, // Red
		0x57F287, // Green
		0x9B59B6, // Purple
	}
	color := colorValues[state.ColorIndex]

	// Parse display order (optional, 0 if empty)
	displayOrder := 0
	if state.DisplayOrder != "" {
		parsed, err := strconv.Atoi(state.DisplayOrder)
		if err != nil {
			state.ErrorMsg = "Display order must be a number"
			return nil
		}
		displayOrder = parsed
	}

	serverID := a.currentServer.ID
	channelID := uuid.Nil
	if a.currentChannel != nil {
		channelID = a.currentChannel.ID
	}

	var req interface{}
	var opCode protocol.OpCode

	if state.Mode == "create" {
		req = &protocol.CreateRoleRequest{
			ServerID:      serverID,
			ChannelID:     channelID,
			Name:          name,
			Permissions:   permissions,
			Color:         color,
			DisplayOrder:  &displayOrder,
			IsHoisted:     state.IsHoisted,
			IsMentionable: state.IsMentionable,
		}
		opCode = protocol.OpCreateRole
	} else {
		// Edit mode
		if state.EditingRoleID == nil {
			state.ErrorMsg = "Invalid role ID"
			return nil
		}
		req = &protocol.UpdateRoleRequest{
			ServerID:      serverID,
			ChannelID:     channelID,
			RoleID:        *state.EditingRoleID,
			Name:          name,
			Permissions:   permissions,
			Color:         color,
			DisplayOrder:  &displayOrder,
			IsHoisted:     state.IsHoisted,
			IsMentionable: state.IsMentionable,
		}
		opCode = protocol.OpUpdateRole
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
	a.serverManagementState.RoleFormOpen = false
	a.serverManagementState.RoleFormState = nil
	a.statusMessage = "Request sent..."

	return nil
}
