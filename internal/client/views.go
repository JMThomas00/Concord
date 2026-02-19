package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// renderLoginView renders the login screen
func (a *App) renderLoginView() string {
	// Render ASCII art banner separately (no width constraint)
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center)

	banner := bannerStyle.Render(a.banner.Art)

	// Render login form with fixed width
	formWidth := 50
	var b strings.Builder

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)

	// Form field styles
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Width(10)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 1).
		Width(36)

	focusedInputStyle := inputStyle.
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple))

	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Faint(true)

	if a.localIdentity != nil {
		// Local identity mode: show welcome + password only
		b.WriteString(subtitleStyle.Render(fmt.Sprintf("Welcome back, %s!", a.localIdentity.Alias)))
		b.WriteString("\n\n")

		// Email shown as read-only label (not editable)
		emailStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		b.WriteString(labelStyle.Render("Email:"))
		b.WriteString(emailStyle.Render(a.localIdentity.Email))
		b.WriteString("\n\n")

		// Password field
		b.WriteString(labelStyle.Render("Password:"))
		b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
		b.WriteString("\n\n")

		// Error message
		if a.loginError != "" {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				Bold(true)
			b.WriteString(errorStyle.Render("⚠ " + a.loginError))
			b.WriteString("\n\n")
		}

		b.WriteString(helpStyle.Render("Enter: Unlock  •  Ctrl+B: Manage Servers  •  Ctrl+T: Themes  •  Ctrl+Q: Quit"))
	} else {
		// Standard login mode: email + password + register link
		b.WriteString(subtitleStyle.Render("Terminal Chat - Login to continue"))
		b.WriteString("\n\n")

		// Email field
		b.WriteString(labelStyle.Render("Email:"))
		if a.loginFocus == 0 {
			b.WriteString(focusedInputStyle.Render(a.loginEmail.View()))
		} else {
			b.WriteString(inputStyle.Render(a.loginEmail.View()))
		}
		b.WriteString("\n\n")

		// Password field
		b.WriteString(labelStyle.Render("Password:"))
		if a.loginFocus == 1 {
			b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
		} else {
			b.WriteString(inputStyle.Render(a.loginPassword.View()))
		}
		b.WriteString("\n\n")

		// Error message
		if a.loginError != "" {
			errorStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				Bold(true)
			b.WriteString(errorStyle.Render("⚠ " + a.loginError))
			b.WriteString("\n\n")
		}

		// Register link
		linkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment))
		focusedLinkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Bold(true).
			Underline(true)
		if a.registerLinkFocused {
			b.WriteString(focusedLinkStyle.Render("→ Register New Account"))
		} else {
			b.WriteString(linkStyle.Render("  Register New Account"))
		}
		b.WriteString("\n\n")

		b.WriteString(helpStyle.Render("Tab: Switch fields  •  Enter: Login/Register  •  Ctrl+B: Manage Servers  •  Ctrl+T: Themes  •  Ctrl+Q: Quit"))
	}

	// Create the form box with padding and fixed width
	formStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(formWidth)

	loginForm := formStyle.Render(b.String())

	// Stack banner and form vertically, both centered
	combined := lipgloss.JoinVertical(
		lipgloss.Center,
		banner,
		"\n", // Spacing between banner and form
		loginForm,
	)

	// Center everything on screen
	return lipgloss.Place(
		a.width,
		a.height,
		lipgloss.Center,
		lipgloss.Center,
		combined,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// renderRegisterView renders the registration screen
func (a *App) renderRegisterView() string {
	// Render ASCII art banner separately (no width constraint)
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center)

	banner := bannerStyle.Render(a.banner.Art)

	// Render registration form with fixed width
	formWidth := 50
	var b strings.Builder

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)

	b.WriteString(subtitleStyle.Render("Create a New Account"))
	b.WriteString("\n\n")

	// Form field styles
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Width(18)

	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 1).
		Width(36)

	focusedInputStyle := inputStyle.
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple))

	// Email field
	b.WriteString(labelStyle.Render("Email:"))
	if a.loginFocus == 0 {
		b.WriteString(focusedInputStyle.Render(a.loginEmail.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginEmail.View()))
	}
	b.WriteString("\n\n")

	// Alias (Username) field
	b.WriteString(labelStyle.Render("Alias:"))
	if a.loginFocus == 1 {
		b.WriteString(focusedInputStyle.Render(a.loginUsername.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginUsername.View()))
	}
	b.WriteString("\n\n")

	// Password field
	b.WriteString(labelStyle.Render("Password:"))
	if a.loginFocus == 2 {
		b.WriteString(focusedInputStyle.Render(a.loginPassword.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginPassword.View()))
	}
	b.WriteString("\n\n")

	// Confirm Password field
	b.WriteString(labelStyle.Render("Confirm Password:"))
	if a.loginFocus == 3 {
		b.WriteString(focusedInputStyle.Render(a.loginPasswordConfirm.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginPasswordConfirm.View()))
	}
	b.WriteString("\n\n")

	// Error message
	if a.loginError != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Bold(true)
		b.WriteString(errorStyle.Render("⚠ " + a.loginError))
		b.WriteString("\n\n")
	}

	// Back link
	linkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment))

	b.WriteString(linkStyle.Render("  Esc: Back to Login"))
	b.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Faint(true)

	helpText := "Tab: Switch fields  •  Enter: Create Account  •  Ctrl+Q: Quit"
	b.WriteString(helpStyle.Render(helpText))

	// Create the form box with padding and fixed width
	formStyle := lipgloss.NewStyle().
		Padding(1, 2).
		Width(formWidth)

	registerForm := formStyle.Render(b.String())

	// Stack banner and form vertically, both centered
	combined := lipgloss.JoinVertical(
		lipgloss.Center,
		banner,
		"\n", // Spacing between banner and form
		registerForm,
	)

	// Center everything on screen
	return lipgloss.Place(
		a.width,
		a.height,
		lipgloss.Center,
		lipgloss.Center,
		combined,
		lipgloss.WithWhitespaceChars(" "),
	)
}

// updateLoginForm handles login form input
func (a *App) updateLoginForm(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	if a.view == ViewRegister {
		// Register view has 4 fields: email, username, password, confirm password
		switch a.loginFocus {
		case 0:
			a.loginEmail, cmd = a.loginEmail.Update(msg)
		case 1:
			a.loginUsername, cmd = a.loginUsername.Update(msg)
		case 2:
			a.loginPassword, cmd = a.loginPassword.Update(msg)
		case 3:
			a.loginPasswordConfirm, cmd = a.loginPasswordConfirm.Update(msg)
		}
	} else {
		// Login view has 2 fields: email, password
		// (Note: registerLinkFocused is handled by keyboard navigation, not textinput)
		switch a.loginFocus {
		case 0:
			a.loginEmail, cmd = a.loginEmail.Update(msg)
		case 1:
			a.loginPassword, cmd = a.loginPassword.Update(msg)
		}
	}

	return cmd
}

// handleLoginSubmit attempts to log in
func (a *App) handleLoginSubmit() tea.Cmd {
	password := a.loginPassword.Value()

	// Local identity mode: verify password locally then auto-connect all servers
	if a.localIdentity != nil {
		if password == "" {
			a.loginError = "Please enter your password"
			return nil
		}
		if password != a.localIdentity.Password {
			a.loginError = "Incorrect password"
			return nil
		}
		a.loginError = ""
		a.view = ViewMain
		a.statusMessage = "Connecting to servers..."
		// Auto-connect all configured servers in background
		servers := a.configMgr.GetClientServers()
		cmds := make([]tea.Cmd, 0, len(servers))
		for _, server := range servers {
			cmds = append(cmds, a.autoConnectServer(server.ID))
		}
		return tea.Batch(cmds...)
	}

	// Standard server login (no local identity configured)
	email := strings.TrimSpace(a.loginEmail.Value())
	if email == "" || password == "" {
		a.loginError = "Please enter email and password"
		return nil
	}

	if a.currentClientServer == nil {
		a.loginError = "No server selected"
		return nil
	}

	a.loginError = ""
	a.statusMessage = "Logging in..."

	// Attempt login via ConnectionManager
	return func() tea.Msg {
		serverID := a.currentClientServer.ID

		// Login via HTTP API
		user, token, err := a.connMgr.Login(serverID, email, password)
		if err != nil {
			return LoginErrorMsg{Error: err.Error()}
		}

		// Return success - WebSocket connection will be established asynchronously
		return LoginSuccessMsg{
			User:     user,
			Token:    token,
			ServerID: serverID,
			Servers:  []*models.Server{},
		}
	}
}

// handleRegisterSubmit attempts to register a new account
func (a *App) handleRegisterSubmit() tea.Cmd {
	email := strings.TrimSpace(a.loginEmail.Value())
	username := strings.TrimSpace(a.loginUsername.Value())
	password := a.loginPassword.Value()
	confirmPassword := a.loginPasswordConfirm.Value()

	// Validation
	if email == "" || username == "" || password == "" || confirmPassword == "" {
		a.loginError = "All fields are required"
		return nil
	}

	if len(username) < 2 || len(username) > 32 {
		a.loginError = "Alias must be 2-32 characters"
		return nil
	}

	if len(password) < 8 {
		a.loginError = "Password must be at least 8 characters"
		return nil
	}

	if password != confirmPassword {
		a.loginError = "Passwords do not match"
		return nil
	}

	if a.currentClientServer == nil {
		a.loginError = "No server selected"
		return nil
	}

	a.loginError = ""
	a.statusMessage = "Creating account..."

	// Attempt registration via ConnectionManager
	return func() tea.Msg {
		serverID := a.currentClientServer.ID

		// Register via HTTP API
		user, token, err := a.connMgr.Register(serverID, username, email, password)
		if err != nil {
			return LoginErrorMsg{Error: err.Error()}
		}

		// Connect WebSocket
		if err := a.connMgr.ConnectServer(serverID); err != nil {
			return LoginErrorMsg{Error: fmt.Sprintf("Failed to connect: %v", err)}
		}

		// Authenticate with token
		if err := a.connMgr.Identify(serverID, token); err != nil {
			return LoginErrorMsg{Error: fmt.Sprintf("Failed to authenticate: %v", err)}
		}

		// Set active connection
		a.activeConn = a.connMgr.GetConnection(serverID)

		return LoginSuccessMsg{
			User:    user,
			Token:   token,
			Servers: []*models.Server{},
		}
	}
}

// renderServerIcons renders the server icons column (leftmost column)
func (a *App) renderServerIcons(width, height int) string {
	var b strings.Builder

	// Available inner width (subtract border)
	innerWidth := width - 2

	// Get servers from disk (source of truth)
	servers := a.configMgr.GetClientServers()

	// Render server list
	for i, server := range servers {
		// Get connection state
		var state ConnectionState
		if conn := a.connMgr.GetConnection(server.ID); conn != nil {
			state = conn.GetState()
		}

		// Connection indicator
		indicator := "○"
		indicatorColor := a.theme.Colors.Comment
		if state == StateReady {
			indicator = "●"
			indicatorColor = a.theme.Colors.Green
		} else if state == StateConnecting || state == StateAuthenticating {
			indicator = "◐"
			indicatorColor = a.theme.Colors.Yellow
		} else if state == StateError {
			indicator = "○"
			indicatorColor = a.theme.Colors.Red
		}

		// Truncate server name to fit: innerWidth - 3 (prefix) - 2 (indicator+space)
		maxNameLen := innerWidth - 5
		if maxNameLen < 4 {
			maxNameLen = 4
		}
		name := server.Name
		if len([]rune(name)) > maxNameLen {
			name = string([]rune(name)[:maxNameLen-1]) + "…"
		}

		indicatorStr := lipgloss.NewStyle().Foreground(lipgloss.Color(indicatorColor)).Render(indicator)

		// Unread dot for this server (any channel has unreads?)
		serverUnreadDot := ""
		if counts := a.unreadCounts[server.ID]; len(counts) > 0 {
			hasMention := false
			for chID, n := range counts {
				if n > 0 {
					if a.mentionCounts[server.ID] != nil && a.mentionCounts[server.ID][chID] > 0 {
						hasMention = true
						break
					}
				}
			}
			dotColor := a.theme.Colors.Foreground
			if hasMention {
				dotColor = a.theme.Colors.Red
			}
			serverUnreadDot = lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render("·")
		}

		var line string
		if i == a.serverIndex {
			// Selected: bold purple with ▶ prefix
			nameStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(maxNameLen).
				Render(name)
			line = fmt.Sprintf("▶ %s %s%s", nameStr, indicatorStr, serverUnreadDot)
		} else {
			// Unselected: dimmed
			nameStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Width(maxNameLen).
				Render(name)
			line = fmt.Sprintf("  %s %s%s", nameStr, indicatorStr, serverUnreadDot)
		}

		b.WriteString(line)
		b.WriteString("\n")
	}

	// Add spacing before "+" button
	if len(servers) > 0 {
		b.WriteString("\n")
	}

	// Add "+" button to add new server (highlighted when selected)
	isAddSelected := a.serverIndex >= len(servers)
	var addButtonStr string
	if isAddSelected {
		addButtonStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Bold(true)
		addButtonStr = addButtonStyle.Render("▶ + Add Server")
	} else {
		addButtonStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Green))
		addButtonStr = addButtonStyle.Render("  + Add Server")
	}
	b.WriteString(addButtonStr)

	// Wrap in bordered box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Width(width).
		Height(height).
		Padding(0, 0)

	// Highlight border if focused
	if a.focus == FocusServerIcons {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return boxStyle.Render(b.String())
}

// renderMainView renders the main chat interface with 4-column layout
func (a *App) renderMainView() string {
	// Use width-1 to account for potential terminal scrollbar or edge
	availableWidth := a.width - 1

	// Fixed widths for 4-column layout (as per CLAUDE.md specification)
	serverIconsWidth := 22   // Server list column (wide enough to show names)
	channelsWidth := 26      // Channels list column
	membersWidth := 30       // Members list column
	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth

	// Ensure chat has minimum width
	if chatWidth < 60 {
		// If terminal is too narrow, reduce members width
		membersWidth = 20
		chatWidth = availableWidth - serverIconsWidth - channelsWidth - membersWidth
	}

	// Height for panels (reserve 1 line for status bar, 1 line for top border visibility)
	panelHeight := a.height - 2

	// Render each panel with exact dimensions (borders included in width/height)
	serverIcons := a.renderServerIcons(serverIconsWidth, panelHeight)
	channels := a.renderChannelList(channelsWidth, panelHeight)
	chat := a.renderChatPanel(chatWidth, panelHeight)
	members := a.renderUserList(membersWidth, panelHeight)

	// Combine panels horizontally (4 columns)
	mainContent := lipgloss.JoinHorizontal(lipgloss.Top, serverIcons, channels, chat, members)

	// Add status bar
	statusBar := a.renderStatusBar()

	// Add top margin line for border visibility
	topMargin := ""

	return lipgloss.JoinVertical(lipgloss.Left, topMargin, mainContent, statusBar)
}

// renderSidebar renders the server/channel sidebar
// renderServerList renders the server list pane (top 40% of sidebar)
func (a *App) renderServerList(width, height int) string {
	var b strings.Builder

	// Header: "SERVERS"
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)
	b.WriteString(headerStyle.Render("SERVERS"))
	b.WriteString("\n")

	// Server list styles
	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	unselectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	// Get protocol servers from active connection
	var servers []*models.Server
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		servers = a.activeConn.Servers
		a.activeConn.mu.RUnlock()
	}

	// Render server list
	for i, srv := range servers {
		name := srv.Name
		if len(name) > width-4 {
			name = name[:width-7] + "..."
		}

		if i == a.protocolServerIndex && a.focus == FocusServerIcons {
			b.WriteString(selectedStyle.Render(name))
		} else if a.currentServer != nil && srv.ID == a.currentServer.ID {
			b.WriteString(unselectedStyle.Foreground(
				lipgloss.Color(a.theme.Colors.Foreground)).Render(name))
		} else {
			b.WriteString(unselectedStyle.Render(name))
		}
		b.WriteString("\n")
	}

	// Placeholder if no servers
	if len(servers) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)
		b.WriteString(placeholderStyle.Render("No servers"))
		b.WriteString("\n")
	}

	// Apply border
	boxStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusServerIcons {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return boxStyle.Render(b.String())
}

// renderChannelList renders the channel list pane (bottom 60% of sidebar)
// renderCategoryRow renders a category row in the channel list
func (a *App) renderCategoryRow(node *ChannelTreeNode, width int) string {
	// Collapse indicator
	indicator := "▼"
	if a.collapsedCategories[node.Channel.ID] {
		indicator = "▶"
	}

	// Category name (uppercase, bold, comment color)
	name := strings.ToUpper(node.Channel.Name)

	// Truncate if needed
	maxLen := width - 6 // Leave room for indicator and padding
	if len(name) > maxLen {
		name = name[:maxLen-3] + "..."
	}

	categoryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)

	return categoryStyle.Render(fmt.Sprintf("%s %s", indicator, name))
}

// renderChannelRow renders a channel row in the channel list
func (a *App) renderChannelRow(node *ChannelTreeNode, width int) string {
	// Indent if has parent category
	indent := ""
	if node.Parent != nil && node.Parent.IsCategory {
		indent = "  " // 2-space indent
	}

	// Channel prefix
	prefix := "# "
	if node.Channel.Type == models.ChannelTypeVoice {
		prefix = "🔊 "
	}

	// Build unread badge (right-aligned suffix)
	var badge string
	isSelected := a.currentChannel != nil && a.currentChannel.ID == node.Channel.ID
	if !isSelected && a.currentClientServer != nil {
		serverID := a.currentClientServer.ID
		mentions := 0
		unreads := 0
		if a.mentionCounts[serverID] != nil {
			mentions = a.mentionCounts[serverID][node.Channel.ID]
		}
		if a.unreadCounts[serverID] != nil {
			unreads = a.unreadCounts[serverID][node.Channel.ID]
		}
		if mentions > 0 {
			badge = fmt.Sprintf(" @%d", mentions)
		} else if unreads > 0 {
			badge = " ●"
		}
	}

	// Channel name
	channelName := prefix + node.Channel.Name

	// Truncate if needed (leave room for badge)
	maxLen := width - len(indent) - 4 - len(badge)
	if maxLen < 4 {
		maxLen = 4
	}
	if len(channelName) > maxLen {
		channelName = channelName[:maxLen-3] + "..."
	}

	// Selection indicator
	selectionPrefix := " "
	if isSelected {
		selectionPrefix = ">"
	}

	fullText := indent + selectionPrefix + channelName + badge

	// Apply style
	channelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	selectedChannelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	// Check if this channel is currently selected
	if isSelected {
		if a.focus == FocusChannelList {
			return selectedChannelStyle.Render(fullText)
		}
		return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Render(fullText)
	}

	// Unread channels render brighter
	if badge != "" {
		if a.currentClientServer != nil {
			serverID := a.currentClientServer.ID
			if a.mentionCounts[serverID] != nil && a.mentionCounts[serverID][node.Channel.ID] > 0 {
				return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true).Render(fullText)
			}
		}
		return channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Bold(true).Render(fullText)
	}

	return channelStyle.Render(fullText)
}

func (a *App) renderChannelList(width, height int) string {
	var b strings.Builder

	// Top border separator
	topBorderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Selection)).
		Width(width - 2)
	b.WriteString(topBorderStyle.Render(strings.Repeat("─", width-2)))
	b.WriteString("\n")

	// Server name header
	serverNameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		Padding(0, 1)

	serverName := "No Server"
	if a.currentServer != nil {
		serverName = a.currentServer.Name
	}
	b.WriteString(serverNameStyle.Render(serverName))
	b.WriteString("\n\n")

	// Channels header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)
	b.WriteString(headerStyle.Render("CHANNELS"))
	b.WriteString("\n")

	// Render hierarchical channel tree
	if a.channelTree != nil && len(a.channelTree.FlatList) > 0 {
		for _, node := range a.channelTree.FlatList {
			if node.IsCategory {
				b.WriteString(a.renderCategoryRow(node, width))
			} else {
				b.WriteString(a.renderChannelRow(node, width))
			}
			b.WriteString("\n")
		}
	} else {
		// Placeholder if no channels
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)

		if a.currentServer == nil {
			b.WriteString(placeholderStyle.Render("Select a server"))
		} else {
			b.WriteString(placeholderStyle.Render("No channels"))
		}
		b.WriteString("\n")
	}

	// Apply border
	boxStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusChannelList {
		boxStyle = boxStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return boxStyle.Render(b.String())
}

// renderSidebar renders the sidebar with split panes (server list + channel list)
func (a *App) renderSidebar(width, height int) string {
	// Split: 40% server list, 60% channel list
	serverListHeight := int(float64(height) * 0.4)
	channelListHeight := height - serverListHeight

	// Ensure minimum heights
	if serverListHeight < 10 {
		serverListHeight = 10
		channelListHeight = height - 10
	}
	if channelListHeight < 10 {
		channelListHeight = 10
		serverListHeight = height - 10
	}

	// Render both panes
	serverList := a.renderServerList(width, serverListHeight)
	channelList := a.renderChannelList(width, channelListHeight)

	// Join vertically (server list on top, channel list on bottom)
	return lipgloss.JoinVertical(lipgloss.Left, serverList, channelList)
}

// renderChatPanel renders the main chat area
func (a *App) renderChatPanel(width, height int) string {
	// Interior width (account for borders)
	interiorWidth := width - 2

	// Calculate heights: textarea has 5 lines + 1 border (bottom only, no top) + 1 for header + 1 for spacer
	// The input box shares its top visual border with the chat viewport's bottom border (1 row total, not 2)
	inputHeight := 6 // 5 lines of text + 1 bottom border (top border removed to avoid double-border gap)
	headerHeight := 2 // header line + spacer line below it
	chatHeight := height - inputHeight - headerHeight

	// Channel header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width).
		Padding(0, 1)

	channelHeader := "Select a channel"
	if a.currentChannel != nil {
		channelHeader = "# " + a.currentChannel.Name
		if a.currentChannel.Topic != "" {
			channelHeader += " - " + a.currentChannel.Topic
		}
	}
	header := headerStyle.Render(channelHeader)

	// Chat viewport - always show border for consistent sizing
	// Subtract 2 for border to get interior content height
	chatStyle := lipgloss.NewStyle().
		Width(width).
		Height(chatHeight).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusChat {
		chatStyle = chatStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	// Keep textarea width in sync with panel interior
	a.input.SetWidth(interiorWidth - 2)

	// Update viewport size to match interior
	if a.chatViewport.Width != interiorWidth || a.chatViewport.Height != chatHeight-2 {
		a.chatViewport.Width = interiorWidth
		a.chatViewport.Height = chatHeight - 2
	}

	chatContent := a.chatViewport.View()

	// Check if there are messages in active connection
	var hasMessages bool
	if a.activeConn != nil && a.currentChannel != nil {
		messages := a.activeConn.GetMessages(a.currentChannel.ID)
		hasMessages = len(messages) > 0
	}

	if !hasMessages {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(interiorWidth).
			Align(lipgloss.Center).
			MarginTop((chatHeight - 2) / 3)
		chatContent = emptyStyle.Render("No messages yet. Say hello!")
	}
	chat := chatStyle.Render(chatContent)

	// Typing indicator — only rendered when someone is actually typing (no reserved blank row)
	typing := ""
	if len(a.typingUsers) > 0 {
		typingText := ""
		if len(a.typingUsers) == 1 {
			typingText = "  " + a.typingUsers[0] + " is typing..."
		} else if len(a.typingUsers) == 2 {
			typingText = "  " + a.typingUsers[0] + " and " + a.typingUsers[1] + " are typing..."
		} else {
			typingText = fmt.Sprintf("  %d people are typing...", len(a.typingUsers))
		}
		typing = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(width).
			Render(typingText)
	}

	// Input area — full rounded border; textarea is 4 content lines so that
	// 1 top border + 4 content + 1 bottom border = 6 rows total (same slot, no gap).
	inputBorderColor := lipgloss.Color(a.theme.Colors.Comment)
	if a.focus == FocusInput {
		inputBorderColor = lipgloss.Color(a.theme.Colors.Purple)
	}
	inputStyle := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(inputBorderColor)

	input := inputStyle.Render(a.input.View())

	// Spacer between header and chat viewport (aligns viewport border with panel borders)
	spacer := lipgloss.NewStyle().Width(width).Height(1).Render("")

	// @mention autocomplete popup (rendered just above the input)
	mentionPopup := ""
	if a.showMentionPopup && len(a.mentionSuggestions) > 0 {
		var popBuf strings.Builder
		popStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Background(lipgloss.Color(a.theme.Colors.CurrentLine)).
			Width(width - 2).
			PaddingLeft(1)
		hlStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Background(lipgloss.Color(a.theme.Colors.CurrentLine)).
			Bold(true).
			Width(width - 2).
			PaddingLeft(1)
		for i, s := range a.mentionSuggestions {
			line := "@" + s
			if i == 0 {
				popBuf.WriteString(hlStyle.Render(line))
			} else {
				popBuf.WriteString(popStyle.Render(line))
			}
			popBuf.WriteString("\n")
		}
		mentionPopup = strings.TrimRight(popBuf.String(), "\n")
	}

	// Combine vertically — only include typing row when someone is actually typing
	// (an empty string in JoinVertical still adds a blank line, which creates an unwanted gap)
	parts := []string{header, spacer, chat}
	if typing != "" {
		parts = append(parts, typing)
	}
	if mentionPopup != "" {
		parts = append(parts, mentionPopup)
	}
	parts = append(parts, input)
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// renderMemberAvatar renders a colored circle with the member's initial, e.g. "(A)"
func (a *App) renderMemberAvatar(name, colorHex string) string {
	initial := "?"
	if len([]rune(name)) > 0 {
		initial = strings.ToUpper(string([]rune(name)[:1]))
	}
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color(colorHex)).
		Bold(true).
		Render("(" + initial + ")")
}

// presenceDot returns the dot character and color for a member's status
func presenceDot(status models.UserStatus, theme *themes.Theme) (string, string) {
	switch status {
	case models.StatusOnline:
		return "●", theme.Colors.Green
	case models.StatusIdle:
		return "◑", theme.Colors.Yellow
	case models.StatusDND:
		return "●", theme.Colors.Red
	default:
		return "○", theme.Colors.Comment
	}
}

// renderUserList renders the role-grouped member list panel
func (a *App) renderUserList(width, height int) string {
	var b strings.Builder

	// Inner width available for text (subtract border chars used by lipgloss border)
	innerWidth := width - 2

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		Width(innerWidth)
	b.WriteString(headerStyle.Render("MEMBERS"))
	b.WriteString("\n")

	// Collect members
	var members []*MemberDisplay
	if a.activeConn != nil {
		a.activeConn.mu.RLock()
		members = a.activeConn.Members
		a.activeConn.mu.RUnlock()
	}

	if len(members) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true)
		b.WriteString(placeholderStyle.Render("No members"))
		b.WriteString("\n")
	} else {
		// Gather distinct hoisted roles present among members, sorted by position DESC
		type roleSection struct {
			role    *models.Role
			members []*MemberDisplay
		}
		roleSectionMap := make(map[uuid.UUID]*roleSection)
		var roleSectionOrder []uuid.UUID
		var regularMembers []*MemberDisplay

		for _, m := range members {
			if m.HighestRole != nil {
				rs, exists := roleSectionMap[m.HighestRole.ID]
				if !exists {
					rs = &roleSection{role: m.HighestRole}
					roleSectionMap[m.HighestRole.ID] = rs
					roleSectionOrder = append(roleSectionOrder, m.HighestRole.ID)
				}
				rs.members = append(rs.members, m)
			} else {
				regularMembers = append(regularMembers, m)
			}
		}

		// Sort roleSectionOrder by position DESC (simple insertion sort — small N)
		for i := 1; i < len(roleSectionOrder); i++ {
			for j := i; j > 0; j-- {
				a := roleSectionMap[roleSectionOrder[j]]
				b2 := roleSectionMap[roleSectionOrder[j-1]]
				if a.role.Position > b2.role.Position {
					roleSectionOrder[j], roleSectionOrder[j-1] = roleSectionOrder[j-1], roleSectionOrder[j]
				} else {
					break
				}
			}
		}

		sectionHeaderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Bold(true).
			Width(innerWidth)

		nameMaxLen := innerWidth - 7 // avatar(3) + space(1) + dot(1) + space(1) = 6 + 1 padding

		renderMember := func(m *MemberDisplay) {
			dot, dotColor := presenceDot(m.User.Status, a.theme)
			dotStr := lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render(dot)
			avatar := a.renderMemberAvatar(m.User.GetDisplayName(), m.AvatarColor)

			name := m.User.GetDisplayName()
			if len([]rune(name)) > nameMaxLen {
				name = string([]rune(name)[:nameMaxLen-1]) + "…"
			}
			nameStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Render(name)

			b.WriteString(" " + avatar + " " + nameStr + " " + dotStr + "\n")
		}

		// Render hoisted role sections
		for _, roleID := range roleSectionOrder {
			rs := roleSectionMap[roleID]
			roleName := strings.ToUpper(rs.role.Name)
			header := fmt.Sprintf("── %s (%d) ──", roleName, len(rs.members))
			b.WriteString(sectionHeaderStyle.Render(header))
			b.WriteString("\n")
			for _, m := range rs.members {
				renderMember(m)
			}
		}

		// Render regular members section
		if len(regularMembers) > 0 {
			header := fmt.Sprintf("── MEMBERS (%d) ──", len(regularMembers))
			b.WriteString(sectionHeaderStyle.Render(header))
			b.WriteString("\n")
			for _, m := range regularMembers {
				renderMember(m)
			}
		}
	}

	userListStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusUserList {
		userListStyle = userListStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return userListStyle.Render(b.String())
}

// renderStatusBar renders the bottom status bar
func (a *App) renderStatusBar() string {
	// Create styles with background for each text segment
	connectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Green))

	disconnectedStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Red))

	textStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))

	// Left side: connection status and user
	leftContent := ""
	isConnected := false
	var currentUser *models.User

	if a.activeConn != nil {
		isConnected = a.activeConn.GetState() == StateReady
		a.activeConn.mu.RLock()
		currentUser = a.activeConn.User
		a.activeConn.mu.RUnlock()
	}

	if isConnected {
		leftContent = connectedStyle.Render(" ● Connected")
	} else {
		leftContent = disconnectedStyle.Render(" ○ Disconnected")
	}

	if currentUser != nil {
		leftContent += textStyle.Render("  |  " + currentUser.FullUsername())
	}

	// Right side: help text
	rightContent := textStyle.Render("Tab: Navigate  |  Up/Down: Select  |  Ctrl+B: Manage Servers  |  Type /help  |  Ctrl+Q: Quit ")

	// Calculate spacing (must know left/right widths before truncating center)
	leftLen := lipgloss.Width(leftContent)
	rightLen := lipgloss.Width(rightContent)

	// Center: status message (truncated to fit available width)
	centerContent := ""
	if a.statusMessage != "" {
		centerStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Colors.Selection))
		if a.statusError {
			centerStyle = centerStyle.Foreground(lipgloss.Color(a.theme.Colors.Red))
		} else {
			centerStyle = centerStyle.Foreground(lipgloss.Color(a.theme.Colors.Cyan))
		}
		maxCenter := a.width - leftLen - rightLen - 4
		msg := a.statusMessage
		if maxCenter > 6 && len([]rune(msg)) > maxCenter {
			msg = string([]rune(msg)[:maxCenter-3]) + "..."
		}
		centerContent = centerStyle.Render(msg)
	}

	centerLen := lipgloss.Width(centerContent)
	totalSpace := a.width - leftLen - rightLen - centerLen

	var bar string
	spacerStyle := textStyle
	if totalSpace > 0 {
		leftPad := totalSpace / 2
		rightPad := totalSpace - leftPad
		bar = leftContent + spacerStyle.Render(strings.Repeat(" ", leftPad)) + centerContent + spacerStyle.Render(strings.Repeat(" ", rightPad)) + rightContent
	} else {
		bar = leftContent + spacerStyle.Render("  ") + centerContent + rightContent
	}

	return bar
}
