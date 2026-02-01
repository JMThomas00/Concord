package client

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// renderLoginView renders the login screen
func (a *App) renderLoginView() string {
	// Calculate centering
	boxWidth := 50
	boxHeight := 15

	// Build the login box content
	var b strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		MarginBottom(1)

	b.WriteString(titleStyle.Render("╔═══════════════════════════════════════════════╗"))
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("║              Welcome to Concord               ║"))
	b.WriteString("\n")
	b.WriteString(titleStyle.Render("╚═══════════════════════════════════════════════╝"))
	b.WriteString("\n\n")

	// Subtitle
	subtitleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true)
	b.WriteString(subtitleStyle.Render("        Terminal Chat - Login to continue"))
	b.WriteString("\n\n")

	// Email input
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

	b.WriteString(labelStyle.Render("Email:"))
	if a.loginFocus == 0 {
		b.WriteString(focusedInputStyle.Render(a.loginEmail.View()))
	} else {
		b.WriteString(inputStyle.Render(a.loginEmail.View()))
	}
	b.WriteString("\n\n")

	// Password input
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

	// Instructions
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Faint(true)
	b.WriteString(helpStyle.Render("Tab: Switch fields  •  Enter: Login  •  Ctrl+C: Quit"))

	// Create the box
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(1, 2).
		Width(boxWidth)

	loginBox := boxStyle.Render(b.String())

	// Center the box
	return lipgloss.Place(
		a.width,
		a.height,
		lipgloss.Center,
		lipgloss.Center,
		loginBox,
		lipgloss.WithWhitespaceChars(" "),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)),
	)
}

// updateLoginForm handles login form input
func (a *App) updateLoginForm(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	if a.loginFocus == 0 {
		a.loginEmail, cmd = a.loginEmail.Update(msg)
	} else {
		a.loginPassword, cmd = a.loginPassword.Update(msg)
	}

	return cmd
}

// handleLoginSubmit attempts to log in
func (a *App) handleLoginSubmit() tea.Cmd {
	email := strings.TrimSpace(a.loginEmail.Value())
	password := a.loginPassword.Value()

	if email == "" || password == "" {
		a.loginError = "Please enter email and password"
		return nil
	}

	a.loginError = ""
	a.statusMessage = "Logging in..."

	// TODO: Implement actual login via connection
	// For now, return a placeholder command
	return func() tea.Msg {
		// This would normally call the connection's login method
		return nil
	}
}

// renderMainView renders the main chat interface
func (a *App) renderMainView() string {
	// Calculate panel sizes
	sidebarWidth := a.width / 5
	if sidebarWidth < 20 {
		sidebarWidth = 20
	}
	if sidebarWidth > 30 {
		sidebarWidth = 30
	}

	userListWidth := a.width / 6
	if userListWidth < 15 {
		userListWidth = 15
	}
	if userListWidth > 25 {
		userListWidth = 25
	}

	chatWidth := a.width - sidebarWidth - userListWidth - 4 // borders
	if chatWidth < 30 {
		// Collapse user list on small screens
		userListWidth = 0
		chatWidth = a.width - sidebarWidth - 2
	}

	// Render each panel
	sidebar := a.renderSidebar(sidebarWidth, a.height-2)
	chat := a.renderChatPanel(chatWidth, a.height-2)

	var userList string
	if userListWidth > 0 {
		userList = a.renderUserList(userListWidth, a.height-2)
	}

	// Combine panels horizontally
	var mainContent string
	if userListWidth > 0 {
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chat, userList)
	} else {
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, chat)
	}

	// Add status bar
	statusBar := a.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, mainContent, statusBar)
}

// renderSidebar renders the server/channel sidebar
func (a *App) renderSidebar(width, height int) string {
	var b strings.Builder

	// Server header
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

	// Channel list
	channelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	selectedChannelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Bold(true).
		Width(width - 2).
		PaddingLeft(1)

	for i, ch := range a.channels {
		prefix := "# "
		if ch.Type == 1 { // Voice channel
			prefix = "🔊 "
		}

		channelName := prefix + ch.Name
		if len(channelName) > width-4 {
			channelName = channelName[:width-7] + "..."
		}

		if i == a.channelIndex && a.focus == FocusSidebar {
			b.WriteString(selectedChannelStyle.Render(channelName))
		} else if a.currentChannel != nil && ch.ID == a.currentChannel.ID {
			b.WriteString(channelStyle.Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Render(channelName))
		} else {
			b.WriteString(channelStyle.Render(channelName))
		}
		b.WriteString("\n")
	}

	// Add placeholder if no channels
	if len(a.channels) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)
		b.WriteString(placeholderStyle.Render("No channels"))
		b.WriteString("\n")
	}

	// Style the entire sidebar
	sidebarStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Border(lipgloss.RoundedBorder(), false, true, false, false).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	if a.focus == FocusSidebar {
		sidebarStyle = sidebarStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	return sidebarStyle.Render(b.String())
}

// renderChatPanel renders the main chat area
func (a *App) renderChatPanel(width, height int) string {
	inputHeight := 3
	chatHeight := height - inputHeight - 1

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

	// Chat viewport
	chatStyle := lipgloss.NewStyle().
		Width(width).
		Height(chatHeight - 1).
		Background(lipgloss.Color(a.theme.Colors.Background))

	if a.focus == FocusChat {
		chatStyle = chatStyle.Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	chatContent := a.chatViewport.View()
	if len(a.messages) == 0 {
		emptyStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(width).
			Align(lipgloss.Center).
			MarginTop(chatHeight / 3)
		chatContent = emptyStyle.Render("No messages yet. Say hello!")
	}
	chat := chatStyle.Render(chatContent)

	// Typing indicator
	typingStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Height(1)

	typingText := ""
	if len(a.typingUsers) > 0 {
		if len(a.typingUsers) == 1 {
			typingText = a.typingUsers[0] + " is typing..."
		} else if len(a.typingUsers) == 2 {
			typingText = a.typingUsers[0] + " and " + a.typingUsers[1] + " are typing..."
		} else {
			typingText = fmt.Sprintf("%d people are typing...", len(a.typingUsers))
		}
	}
	typing := typingStyle.Render(typingText)

	// Input area
	inputStyle := lipgloss.NewStyle().
		Width(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 1)

	if a.focus == FocusInput {
		inputStyle = inputStyle.BorderForeground(lipgloss.Color(a.theme.Colors.Purple))
	}

	input := inputStyle.Render(a.input.View())

	// Combine vertically
	return lipgloss.JoinVertical(lipgloss.Left, header, chat, typing, input)
}

// renderUserList renders the user/member list
func (a *App) renderUserList(width, height int) string {
	var b strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Bold(true).
		PaddingLeft(1)
	b.WriteString(headerStyle.Render("MEMBERS"))
	b.WriteString("\n\n")

	// Online members
	onlineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Green)).
		PaddingLeft(1)

	offlineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		PaddingLeft(1)

	for _, m := range a.members {
		statusDot := "●"
		style := offlineStyle

		if m.User.Status == "online" {
			style = onlineStyle
		} else if m.User.Status == "idle" {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Yellow)).
				PaddingLeft(1)
		} else if m.User.Status == "dnd" {
			style = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				PaddingLeft(1)
		}

		name := m.User.GetDisplayName()
		if len(name) > width-5 {
			name = name[:width-8] + "..."
		}

		b.WriteString(style.Render(statusDot + " " + name))
		b.WriteString("\n")
	}

	// Add placeholder if no members
	if len(a.members) == 0 {
		placeholderStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			PaddingLeft(1)
		b.WriteString(placeholderStyle.Render("No members"))
		b.WriteString("\n")
	}

	// Style the entire user list
	userListStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Border(lipgloss.RoundedBorder(), false, false, false, true).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection))

	return userListStyle.Render(b.String())
}

// renderStatusBar renders the bottom status bar
func (a *App) renderStatusBar() string {
	statusStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(a.theme.Colors.Selection)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(a.width).
		Padding(0, 1)

	// Left side: connection status and user
	leftContent := ""
	if a.connected {
		leftContent += lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Green)).
			Render("● Connected")
	} else {
		leftContent += lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Render("○ Disconnected")
	}

	if a.user != nil {
		leftContent += "  |  " + a.user.FullUsername()
	}

	// Right side: help text
	rightContent := "Tab: Navigate  |  Ctrl+S: Switch Server  |  Ctrl+C: Quit"

	// Center: status message
	centerContent := ""
	if a.statusMessage != "" {
		if a.statusError {
			centerContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Red)).
				Render(a.statusMessage)
		} else {
			centerContent = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
				Render(a.statusMessage)
		}
	}

	// Calculate spacing
	leftLen := lipgloss.Width(leftContent)
	rightLen := lipgloss.Width(rightContent)
	centerLen := lipgloss.Width(centerContent)
	totalSpace := a.width - leftLen - rightLen - centerLen - 4

	var bar string
	if totalSpace > 0 {
		leftPad := totalSpace / 2
		rightPad := totalSpace - leftPad
		bar = leftContent + strings.Repeat(" ", leftPad) + centerContent + strings.Repeat(" ", rightPad) + rightContent
	} else {
		bar = leftContent + "  " + centerContent
	}

	return statusStyle.Render(bar)
}
