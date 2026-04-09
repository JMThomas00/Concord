package client

import (
	"fmt"
	"log"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// typingFrames is the braille spinner sequence used for the typing animation.
// Each frame is shown for ~400ms, cycling smoothly at ~2.5 frames/sec.
var typingFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// renderLoginView renders the login screen
func (a *App) renderLoginView() string {
	// Render ASCII art banner. Trim trailing whitespace from each line so lipgloss
	// measures the true visual width. Many banner strings have trailing spaces that
	// inflate the block width, causing lipgloss.Place to add too little left padding
	// and making the art appear left-shifted on screen.
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true)

	banner := bannerStyle.Render(trimBannerArt(a.banner.Art))

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

		b.WriteString(helpStyle.Render("Enter: Unlock  •  Ctrl+S: Settings  •  Ctrl+T: Themes  •  Ctrl+Q: Quit"))
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

		b.WriteString(helpStyle.Render("Tab: Switch fields  •  Enter: Login/Register  •  Ctrl+S: Settings  •  Ctrl+T: Themes  •  Ctrl+Q: Quit"))
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
	// Render ASCII art banner — trim trailing whitespace for correct centering.
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true)

	banner := bannerStyle.Render(trimBannerArt(a.banner.Art))

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
		log.Printf("DEBUG handleLoginSubmit: Changing view to ViewMain, setting focus to FocusServerIcons")
		a.view = ViewMain
		a.focus = FocusServerIcons // Start on server icons (consistent with auto-login)

		// Only auto-connect servers that aren't already connected
		// (servers auto-connect in background during Init, so they may already be ready)
		servers := a.configMgr.GetClientServers()
		cmds := make([]tea.Cmd, 0, len(servers))
		allConnected := true
		for _, server := range servers {
			conn := a.connMgr.GetConnection(server.ID)
			log.Printf("DEBUG handleLoginSubmit: Server %s - conn=%v, state=%v",
				server.Name, conn != nil,
				func() string { if conn != nil { return fmt.Sprintf("%v", conn.GetState()) }; return "nil" }())
			if conn == nil || conn.GetState() != StateReady {
				log.Printf("DEBUG handleLoginSubmit: Server %s NOT ready, calling autoConnectServer", server.Name)
				cmds = append(cmds, a.autoConnectServer(server.ID))
				allConnected = false
			} else {
				log.Printf("DEBUG handleLoginSubmit: Server %s already connected, skipping reconnect", server.Name)
			}
		}

		if allConnected {
			log.Printf("DEBUG handleLoginSubmit: All servers already connected")
			a.statusMessage = "Ready"
		} else {
			log.Printf("DEBUG handleLoginSubmit: Some servers not connected, reconnecting")
			a.statusMessage = "Connecting to servers..."
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
		oldConn := a.activeConn
		a.activeConn = a.connMgr.GetConnection(serverID)
		log.Printf("DEBUG handleAddServerSubmit: Changed activeConn from %p to %p (serverID=%s)",
			oldConn, a.activeConn, serverID)

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

	// Removed "Manage Servers" button - now accessible via 's' key or Ctrl+S

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

	showMembers := a.uiConfig == nil || a.uiConfig.ShowMembersList
	if !showMembers {
		membersWidth = 0
	}

	chatWidth := availableWidth - serverIconsWidth - channelsWidth - membersWidth
	// lipgloss Width(n) sets content width; outer rendered width = n+2 (left+right border).
	// In 4-column mode each panel contributes n+2 outer width, making total = availableWidth+8.
	// In 3-column mode (members hidden) each of the 3 panels adds 2 extra outer chars = 6 total,
	// so we subtract 6 from chatWidth to keep total outer == availableWidth and show the right border.
	if !showMembers {
		chatWidth -= 6
	}

	// Ensure chat has minimum width
	if chatWidth < 60 && showMembers {
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

	// Combine panels horizontally (3 or 4 columns depending on members visibility)
	var mainContent string
	if showMembers {
		members := a.renderUserList(membersWidth, panelHeight)
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, serverIcons, channels, chat, members)
	} else {
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, serverIcons, channels, chat)
	}

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
	// Check if this category is currently selected
	isSelected := a.currentChannel != nil && a.currentChannel.ID == node.Channel.ID

	// Collapse indicator
	indicator := "▼"
	if a.collapsedCategories[node.Channel.ID] {
		indicator = "▶"
	}

	// Selection prefix
	selectionPrefix := " "
	if isSelected {
		selectionPrefix = ">"
	}

	// Category name (uppercase, bold, comment color)
	name := strings.ToUpper(node.Channel.Name)

	// Truncate if needed
	maxLen := width - 6 // Leave room for indicator and padding
	if len(name) > maxLen {
		name = name[:maxLen-3] + "..."
	}

	fullText := fmt.Sprintf("%s %s", indicator, name)

	// Apply style based on selection
	categoryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)

	selectedCategoryStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	if isSelected {
		return selectedCategoryStyle.Render(selectionPrefix + fullText)
	}
	return categoryStyle.Render(selectionPrefix + fullText)
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
		prefix = "♪ "
	}

	// Add lock icon for locked channels
	lockIcon := ""
	if node.Channel.IsLocked {
		lockIcon = "⊗ "
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
	channelName := prefix + lockIcon + node.Channel.Name

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
	serverOffline := false
	if a.currentServer != nil {
		serverName = a.currentServer.Name
	} else if a.currentClientServer != nil {
		serverName = a.currentClientServer.Name
		serverOffline = true
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

		var placeholder string
		switch {
		case serverOffline:
			placeholder = "Server offline"
		case a.currentServer == nil:
			placeholder = "Select a server"
		default:
			placeholder = "No channels"
		}
		b.WriteString(placeholderStyle.Render(placeholder))
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
	chatHeight -= 1 // Reserve space for typing indicator (always present, blank when inactive)

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

	// Pinned messages header — shown above the chat viewport when pins exist
	pinnedHeader := ""
	pinnedHeaderLines := 0
	if a.activeConn != nil && a.currentChannel != nil {
		a.activeConn.mu.RLock()
		pinnedMsgs := a.activeConn.PinnedMessages[a.currentChannel.ID]
		a.activeConn.mu.RUnlock()

		if len(pinnedMsgs) > 0 {
			// Header style (grey/comment color)
			pinHeaderStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Comment)).
				Width(interiorWidth).
				PaddingLeft(1)
			// Message content style (yellow - more noticeable)
			pinContentStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Yellow)).
				Width(interiorWidth).
				PaddingLeft(1)

			var pinBuf strings.Builder
			// Cap display to 3 pinned messages to limit height consumption
			displayCount := len(pinnedMsgs)
			if displayCount > 3 {
				displayCount = 3
			}
			pinBuf.WriteString(pinHeaderStyle.Render(fmt.Sprintf("★ %d pinned message(s)  (/unpin N to remove)", len(pinnedMsgs))))
			pinBuf.WriteString("\n")
			for i := 0; i < displayCount; i++ {
				pm := pinnedMsgs[i]
				// Word-wrap the message content to fit the width
				// Account for "[i] " prefix (4 chars max) and padding
				maxWidth := interiorWidth - 6
				if maxWidth < 20 {
					maxWidth = 20
				}
				wrappedLines := a.splitMessageIntoLines(pm.Content, maxWidth)

				// Render each wrapped line with proper indentation
				for lineIdx, line := range wrappedLines {
					var prefix string
					if lineIdx == 0 {
						// First line: show message number
						prefix = fmt.Sprintf("[%d] ", i+1)
					} else {
						// Continuation lines: indent to align with first line content
						prefix = "    "
					}
					pinBuf.WriteString(pinContentStyle.Render(prefix + line))
					pinBuf.WriteString("\n")
				}
			}
			pinBuf.WriteString(pinHeaderStyle.Render(strings.Repeat("─", interiorWidth-2)))
			pinnedHeader = strings.TrimRight(pinBuf.String(), "\n")
			// Count actual lines by counting newlines in rendered output (+1 for final line)
			pinnedHeaderLines = strings.Count(pinnedHeader, "\n") + 1
		}
	}

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

	// Update viewport size to match interior, reducing height for pinned messages
	viewportHeight := chatHeight - 2 - pinnedHeaderLines
	if a.chatViewport.Width != interiorWidth || a.chatViewport.Height != viewportHeight {
		a.chatViewport.Width = interiorWidth
		a.chatViewport.Height = viewportHeight
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

	// Prepend pinned messages INSIDE the chat border (above the viewport content)
	if pinnedHeader != "" {
		chatContent = pinnedHeader + "\n" + chatContent
	}

	chat := chatStyle.Render(chatContent)

	// Typing indicator — always reserve space (render blank when inactive to prevent layout shift).
	// The braille spinner (typingFrames) advances every 400ms via typingTickMsg.
	typing := ""
	if len(a.typingUsers) > 0 {
		frame := typingFrames[a.typingFrame%len(typingFrames)]
		var who string
		switch len(a.typingUsers) {
		case 1:
			who = a.typingUsers[0] + " is typing"
		case 2:
			who = a.typingUsers[0] + " and " + a.typingUsers[1] + " are typing"
		default:
			who = fmt.Sprintf("%d people are typing", len(a.typingUsers))
		}
		spinnerStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Bold(true)
		textStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true)
		typingLine := "  " + spinnerStyle.Render(frame) + " " + textStyle.Render(who+"...")
		typing = lipgloss.NewStyle().Width(width).Render(typingLine)
	} else {
		// Render blank line to maintain spacing (prevents border shift when typing starts/stops)
		typing = lipgloss.NewStyle().Width(width).Height(1).Render("")
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

	// Prepare input content with optional reply quote
	inputContent := a.injectMentionGhost(a.input.View())
	if a.replyTarget != nil {
		// Show reply quote above input (styled, dimmed, italic)
		replyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).  // Dim gray
			Italic(true)
		replyLine := fmt.Sprintf("↩ Replying to %s: %s", a.replyTarget.AuthorName, a.replyQuote)
		inputContent = replyStyle.Render(replyLine) + "\n" + inputContent
	}
	input := inputStyle.Render(inputContent)

	// Spacer between header and chat viewport (aligns viewport border with panel borders)
	spacer := lipgloss.NewStyle().Width(width).Height(1).Render("")

	// Combine vertically — always include typing row (blank when inactive) to prevent border shift
	// Note: pinnedHeader is now rendered INSIDE the chat border, not as a separate element
	parts := []string{header, spacer, chat, typing, input}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// injectMentionGhost post-processes the textarea View() output to show the
// top @mention suggestion as inline ghost text (dim italic) immediately after
// the cursor, replacing an equal number of trailing spaces so line width stays
// constant and borders are not pushed around.
func (a *App) injectMentionGhost(view string) string {
	if !a.showMentionPopup || len(a.mentionSuggestions) == 0 {
		return view
	}
	suggestion := a.mentionSuggestions[0]
	if len(suggestion) <= len(a.mentionQuery) {
		return view
	}
	ghost := suggestion[len(a.mentionQuery):]

	ghostRendered := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Render(ghost)

	lines := strings.Split(view, "\n")
	cursorLine := a.input.Line()
	if cursorLine >= len(lines) {
		return view
	}

	line := lines[cursorLine]

	// The textarea renders the cursor character with reverse-video (\x1b[7m).
	// Find that sequence and skip past it and its reset to locate the injection point.
	cursorSeq := "\x1b[7m"
	cursorStart := strings.Index(line, cursorSeq)
	if cursorStart < 0 {
		return view
	}
	// Skip \x1b[7m + one cursor rune + any trailing CSI reset sequences.
	i := cursorStart + len(cursorSeq)
	if i < len(line) {
		_, size := utf8.DecodeRuneInString(line[i:])
		i += size
	}
	for i < len(line) && line[i] == '\x1b' {
		j := i + 1
		if j < len(line) && line[j] == '[' {
			j++
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j < len(line) {
				j++
			}
			i = j
		} else {
			break
		}
	}
	insertOffset := i

	// Remove len(ghost) trailing visible characters from the suffix so the
	// overall visual width of the line does not change.
	suffix := line[insertOffset:]
	suffix = ansiTrimTrailingVisChars(suffix, len(ghost))

	lines[cursorLine] = line[:insertOffset] + ghostRendered + suffix
	return strings.Join(lines, "\n")
}

// ansiVisColToByteOffset returns the byte offset in s at which visible column
// visCol (0-indexed) begins, skipping ANSI/CSI/OSC escape sequences entirely.
// Returns len(s) when visCol exceeds the visible length.
func ansiVisColToByteOffset(s string, visCol int) int {
	col := 0
	i := 0
	for i < len(s) {
		if col >= visCol {
			return i
		}
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				// CSI: \x1b[ params finalByte (0x40–0x7e)
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			} else if j < len(s) && s[j] == ']' {
				// OSC: ends with BEL or ST (\x1b\\)
				j++
				for j < len(s) {
					if s[j] == '\a' {
						j++
						break
					}
					if s[j] == '\x1b' && j+1 < len(s) && s[j+1] == '\\' {
						j += 2
						break
					}
					j++
				}
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		col++
	}
	return i
}

// ansiTrimTrailingVisChars removes up to n trailing visible characters from s,
// preserving ANSI escape sequences. Returns the truncated string.
func ansiTrimTrailingVisChars(s string, n int) string {
	if n <= 0 {
		return s
	}
	// Count total visible characters.
	visLen := 0
	i := 0
	for i < len(s) {
		if s[i] == '\x1b' {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		visLen++
	}
	target := visLen - n
	if target <= 0 {
		return ""
	}
	return s[:ansiVisColToByteOffset(s, target)]
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

	// Top border separator (for symmetry with channels panel)
	topBorderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Selection)).
		Width(innerWidth)
	b.WriteString(topBorderStyle.Render(strings.Repeat("─", innerWidth)))
	b.WriteString("\n")

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
		// Build flat member list for highlighting if focused
		var flatMembers []*MemberDisplay
		if a.focus == FocusUserList {
			flatMembers = a.buildFlatMemberList()
		}

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

		// Sort roleSectionOrder by DisplayOrder ASC (lower number = higher priority/top)
		// If DisplayOrder is equal, sort by role name alphabetically for consistency
		for i := 1; i < len(roleSectionOrder); i++ {
			for j := i; j > 0; j-- {
				curr := roleSectionMap[roleSectionOrder[j]]
				prev := roleSectionMap[roleSectionOrder[j-1]]

				currOrder := curr.role.DisplayOrder
				prevOrder := prev.role.DisplayOrder

				// Sort by DisplayOrder ASC (0 is highest priority, shows at top)
				// If equal, sort by name alphabetically
				shouldSwap := false
				if currOrder < prevOrder {
					shouldSwap = true
				} else if currOrder == prevOrder {
					// Secondary sort: alphabetical by role name
					shouldSwap = curr.role.Name < prev.role.Name
				}

				if shouldSwap {
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

		nameMaxLen := innerWidth - 9 // avatar(3) + prefix(2) + space(1) + dot(1) + space(1) = 8 + 1 padding

		// Track position in flat list for selection highlighting
		flatIndex := 0

		renderMember := func(m *MemberDisplay) {
			prefix := "  "
			baseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))

			// Highlight selected member
			isSelected := a.focus == FocusUserList && len(flatMembers) > 0 && flatIndex == a.selectedMemberIndex
			if isSelected {
				prefix = "> "
				baseStyle = baseStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
			}

			dot, dotColor := presenceDot(m.User.Status, a.theme)
			dotStr := lipgloss.NewStyle().Foreground(lipgloss.Color(dotColor)).Render(dot)
			avatar := a.renderMemberAvatar(m.User.GetDisplayName(), m.AvatarColor)

			name := m.User.GetDisplayName()
			if len([]rune(name)) > nameMaxLen {
				name = string([]rune(name)[:nameMaxLen-1]) + "…"
			}
			nameStr := baseStyle.Render(name)

			line := prefix + avatar + " " + nameStr + " " + dotStr
			if isSelected {
				// Apply background to entire line
				line = baseStyle.Width(innerWidth).Render(line)
			}

			b.WriteString(line + "\n")

			// Render custom title if present (yellow, bold, indented)
			if m.Member != nil && m.Member.CustomTitle != "" {
				titleStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Yellow)).
					Bold(true)
				if isSelected {
					titleStyle = titleStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
				}
				// Truncate title if too long - very conservative to ensure ellipsis shows
				titleText := m.Member.CustomTitle
				titleMaxLen := innerWidth - 10 // Extra conservative for ellipsis visibility
				if titleMaxLen < 10 {
					titleMaxLen = 10 // Minimum readable length
				}
				if len([]rune(titleText)) > titleMaxLen {
					runes := []rune(titleText)
					titleText = string(runes[:titleMaxLen-1]) + "…"
				}
				titleLine := "    " + titleStyle.Render(titleText)
				b.WriteString(titleLine + "\n")
			}

			// Render status text if present (gray, italic, indented)
			if m.User.StatusText != "" {
				statusStyle := lipgloss.NewStyle().
					Foreground(lipgloss.Color(a.theme.Colors.Comment)).
					Italic(true)
				if isSelected {
					statusStyle = statusStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
				}
				// Truncate status if too long - very conservative to ensure ellipsis shows
				statusText := m.User.StatusText
				statusMaxLen := innerWidth - 10 // Extra conservative for ellipsis visibility
				if statusMaxLen < 10 {
					statusMaxLen = 10 // Minimum readable length
				}
				if len([]rune(statusText)) > statusMaxLen {
					runes := []rune(statusText)
					statusText = string(runes[:statusMaxLen-1]) + "…"
				}
				statusLine := "    " + statusStyle.Render(statusText)
				b.WriteString(statusLine + "\n")
			}

			flatIndex++
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

	// Removed "Manage Members" button - now accessible via Ctrl+B

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

	// Right side: help text (include Server Settings for admins)
	helpText := "Tab: Navigate  |  Ctrl+S: Settings  |  "
	if a.currentUserRoleLevel() >= roleLevelAdmin {
		helpText += "Ctrl+B: Server Settings  |  "
	}
	helpText += "Type /help  |  Ctrl+Q: Quit "
	rightContent := textStyle.Render(helpText)

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

// renderLinkBrowserOverlay renders the link browser modal overlay on top of the base view
func (a *App) renderLinkBrowserOverlay(baseView string) string {
	if a.linkBrowserState == nil {
		return baseView
	}

	// Calculate overlay dimensions (centered modal)
	overlayWidth := 80
	if overlayWidth > a.width-4 {
		overlayWidth = a.width - 4
	}

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).
		Bold(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	header := headerStyle.Render("Links")

	// Link list
	var linkLines []string
	for i, link := range a.linkBrowserState.Links {
		numberStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Background(lipgloss.Color(a.theme.Semantic.InputBg)).
			Bold(true)
		linkStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
			Background(lipgloss.Color(a.theme.Semantic.InputBg))

		// Highlight selected link
		if i == a.linkBrowserState.SelectedIndex {
			numberStyle = numberStyle.
				Background(lipgloss.Color(a.theme.Colors.Selection))
			linkStyle = linkStyle.
				Background(lipgloss.Color(a.theme.Colors.Selection))
		}

		// Truncate link if too long
		maxLinkLen := overlayWidth - 10
		displayLink := link
		if len(displayLink) > maxLinkLen {
			displayLink = displayLink[:maxLinkLen-1] + "…"
		}

		line := fmt.Sprintf("%s%s",
			numberStyle.Render(fmt.Sprintf("[%d]", i+1)),
			linkStyle.Render(" " + displayLink))

		// Wrap line in full-width style to ensure background fills
		lineStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Semantic.InputBg)).
			Width(overlayWidth - 2)
		linkLines = append(linkLines, lineStyle.Render(line))
	}

	// Footer with keybind hints
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).
		Italic(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	hints := hintStyle.Render("Enter: Open  •  C: Copy  •  Esc: Close")

	// Build modal content
	var modalContent strings.Builder
	modalContent.WriteString(header + "\n\n")
	for _, line := range linkLines {
		modalContent.WriteString(line + "\n")
	}
	modalContent.WriteString("\n" + hints)

	// Calculate modal height
	modalHeight := len(linkLines) + 5 // header + links + footer + spacing

	// Wrap in box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(overlayWidth).
		Height(modalHeight).
		Padding(1).
		Background(lipgloss.Color(a.theme.Semantic.InputBg))

	modal := boxStyle.Render(modalContent.String())

	// Place modal centered on screen
	yOffset := (a.height - modalHeight) / 2
	if yOffset < 0 {
		yOffset = 0
	}
	xOffset := (a.width - overlayWidth) / 2
	if xOffset < 0 {
		xOffset = 0
	}

	// Use lipgloss.Place to overlay the modal on the base view
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(""),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)))
}

// renderHelpModalOverlay renders the help modal overlay on top of the base view
func (a *App) renderHelpModalOverlay(baseView string) string {
	if a.helpModalState == nil {
		return baseView
	}

	// Calculate overlay dimensions (centered modal, slightly wider than link browser)
	overlayWidth := 100
	if overlayWidth > a.width-4 {
		overlayWidth = a.width - 4
	}

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	header := headerStyle.Render("Help - Available Commands")

	// Content (preserve existing formatting from command handler)
	contentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Width(overlayWidth - 4)
	content := contentStyle.Render(a.helpModalState.Content)

	// Footer with keybind hints
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Italic(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	hints := hintStyle.Render("Esc: Close")

	// Build modal content
	var modalContent strings.Builder
	modalContent.WriteString(header + "\n\n")
	modalContent.WriteString(content)
	modalContent.WriteString("\n\n" + hints)

	// Calculate modal height (based on content lines + header + footer + padding)
	contentLines := strings.Count(a.helpModalState.Content, "\n") + 1
	modalHeight := contentLines + 6 // header + content + footer + spacing

	// Cap height to avoid overflow
	maxHeight := a.height - 4
	if modalHeight > maxHeight {
		modalHeight = maxHeight
	}

	// Wrap in box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(overlayWidth).
		Height(modalHeight).
		Padding(1).
		Background(lipgloss.Color(a.theme.Colors.Background))

	modal := boxStyle.Render(modalContent.String())

	// Place modal centered on screen
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(""),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)))
}

// renderMemberContextMenuOverlay renders the member action context menu overlay
func (a *App) renderMemberContextMenuOverlay(baseView string) string {
	if a.memberContextMenu == nil {
		return baseView
	}

	// Calculate overlay dimensions
	overlayWidth := 50
	if overlayWidth > a.width-4 {
		overlayWidth = a.width - 4
	}

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	title := titleStyle.Render(fmt.Sprintf("Actions for @%s", a.memberContextMenu.TargetMember.User.Username))

	// Action list
	var actionLines []string
	for i, action := range a.memberContextMenu.Actions {
		keyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Bold(true)
		labelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Foreground))

		prefix := "  "

		// Highlight selected action
		if i == a.memberContextMenu.SelectedIndex {
			prefix = "> "
			keyStyle = keyStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
			labelStyle = labelStyle.Background(lipgloss.Color(a.theme.Semantic.SidebarSelected))
		}

		line := fmt.Sprintf("%s%s %s",
			prefix,
			keyStyle.Render(fmt.Sprintf("[%s]", action.Key)),
			labelStyle.Render(action.Label))
		actionLines = append(actionLines, line)
	}

	// Footer with keybind hints
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Align(lipgloss.Center).
		Width(overlayWidth - 2)
	hints := hintStyle.Render("Enter: Execute  •  Esc: Close")

	// Build modal content
	var modalContent strings.Builder
	modalContent.WriteString(title + "\n\n")
	for _, line := range actionLines {
		modalContent.WriteString(line + "\n")
	}
	modalContent.WriteString("\n" + hints)

	// Calculate modal height
	modalHeight := len(actionLines) + 5 // title + actions + footer + spacing

	// Wrap in box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(overlayWidth).
		Height(modalHeight).
		Padding(1).
		Background(lipgloss.Color(a.theme.Colors.Background))

	modal := boxStyle.Render(modalContent.String())

	// Use lipgloss.Place to overlay the modal on the base view
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(""),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)))
}
