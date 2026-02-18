package client

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// initAddServerForm initializes the Add Server form
func (a *App) initAddServerForm() {
	// Initialize form fields
	a.addServerName = textinput.New()
	a.addServerName.Placeholder = "e.g., My Server"
	a.addServerName.CharLimit = 50
	a.addServerName.Width = 50
	a.addServerName.Focus()

	a.addServerAddress = textinput.New()
	a.addServerAddress.Placeholder = "e.g., localhost or 192.168.1.100"
	a.addServerAddress.CharLimit = 255
	a.addServerAddress.Width = 50

	a.addServerPort = textinput.New()
	a.addServerPort.Placeholder = "8080"
	a.addServerPort.CharLimit = 5
	a.addServerPort.Width = 50
	a.addServerPort.SetValue("8080") // Default port

	a.addServerUseTLS = false
	a.addServerFocus = 0
	a.addServerError = ""
}

// renderAddServerView renders the Add Server dialog
func (a *App) renderAddServerView() string {
	// Calculate dialog dimensions
	dialogWidth := 60
	dialogHeight := 18

	// Center the dialog
	leftPadding := (a.width - dialogWidth) / 2
	topPadding := (a.height - dialogHeight) / 2

	if leftPadding < 0 {
		leftPadding = 0
	}
	if topPadding < 0 {
		topPadding = 0
	}

	// Build the dialog content
	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	title := "Add New Server"
	if a.editingServerID != nil {
		title = "Edit Server"
	}
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")

	// Error message if present
	if a.addServerError != "" {
		errorStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Width(dialogWidth - 4).
			Align(lipgloss.Center)
		content.WriteString(errorStyle.Render(a.addServerError))
		content.WriteString("\n\n")
	}

	// Form fields with clean styling
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(dialogWidth - 4)

	fieldContainerStyle := lipgloss.NewStyle().
		Padding(0, 1).
		Width(dialogWidth - 6)

	// Server Name
	content.WriteString(labelStyle.Render("Server Name:"))
	content.WriteString("\n")
	content.WriteString(fieldContainerStyle.Render(a.addServerName.View()))
	content.WriteString("\n\n")

	// Server Address
	content.WriteString(labelStyle.Render("Address:"))
	content.WriteString("\n")
	content.WriteString(fieldContainerStyle.Render(a.addServerAddress.View()))
	content.WriteString("\n\n")

	// Server Port
	content.WriteString(labelStyle.Render("Port:"))
	content.WriteString("\n")
	content.WriteString(fieldContainerStyle.Render(a.addServerPort.View()))
	content.WriteString("\n\n")

	// Use TLS toggle
	content.WriteString(labelStyle.Render("Use TLS (WSS):"))
	content.WriteString("\n")

	tlsValue := "[ ] No"
	if a.addServerUseTLS {
		tlsValue = "[✓] Yes"
	}

	tlsStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	if a.addServerFocus == 3 {
		tlsStyle = tlsStyle.Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	}

	content.WriteString(fieldContainerStyle.Render(tlsStyle.Render(tlsValue)))
	content.WriteString("\n\n")

	// Help text
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	content.WriteString(helpStyle.Render("[Tab] Next  [Space] Toggle TLS  [Enter] Add  [Esc] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(1, 2).
		Width(dialogWidth).
		Height(dialogHeight)

	dialog := dialogStyle.Render(content.String())

	// Center the dialog on screen
	centeredDialog := lipgloss.NewStyle().
		Width(a.width).
		Height(a.height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(dialog)

	return centeredDialog
}

// handleAddServerSubmit validates and submits the Add Server form
func (a *App) handleAddServerSubmit() tea.Cmd {
	// Validate inputs
	name := strings.TrimSpace(a.addServerName.Value())
	address := strings.TrimSpace(a.addServerAddress.Value())
	portStr := strings.TrimSpace(a.addServerPort.Value())

	if name == "" {
		a.addServerError = "Server name is required"
		return nil
	}

	if address == "" {
		a.addServerError = "Server address is required"
		return nil
	}

	if portStr == "" {
		portStr = "8080" // Default port
	}

	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		a.addServerError = "Port must be a number between 1 and 65535"
		return nil
	}

	// If editing an existing server, update instead of add
	if a.editingServerID != nil {
		idx := a.editingServerIndex
		if idx >= 0 && idx < len(a.clientServers) {
			cs := a.clientServers[idx]
			cs.Name = name
			cs.Address = address
			cs.Port = port
			cs.UseTLS = a.addServerUseTLS
			if name != "" {
				cs.IconLetter = string([]rune(strings.ToUpper(name))[0])
			}
			updatedServer := cs

			a.editingServerID = nil
			a.view = ViewManageServers
			a.addServerError = ""

			return func() tea.Msg {
				if err := a.configMgr.UpdateServer(updatedServer); err != nil {
					return ErrorMsg{Error: fmt.Sprintf("Failed to save server: %v", err)}
				}
				return nil
			}
		}
		a.editingServerID = nil
		a.view = ViewManageServers
		return nil
	}

	// Check for duplicate server (same address and port, skip if editing)
	for _, existing := range a.clientServers {
		if existing.Address == address && existing.Port == port {
			a.addServerError = fmt.Sprintf("Server %s:%d already exists", address, port)
			return nil
		}
	}

	// Create new server info
	newServer := NewClientServerInfo(name, address, port, a.addServerUseTLS)

	// Add to client servers list
	a.clientServers = append(a.clientServers, newServer)

	// Add to connection manager
	if _, err := a.connMgr.AddServer(newServer); err != nil {
		a.addServerError = fmt.Sprintf("Failed to add server: %v", err)
		return nil
	}

	a.addServerError = ""

	// Always return to ViewMain and auto-connect in background
	a.view = ViewMain
	a.statusMessage = fmt.Sprintf("Server '%s' added. Connecting...", name)

	// Save to servers.json
	saveCmd := func() tea.Msg {
		configMgr, err := NewConfigManager()
		if err != nil {
			return ErrorMsg{Error: fmt.Sprintf("Failed to create config manager: %v", err)}
		}
		if err := configMgr.AddServer(newServer); err != nil {
			return ErrorMsg{Error: fmt.Sprintf("Failed to save server: %v", err)}
		}
		return nil
	}

	// Auto-connect if identity is already configured
	var autoConnectCmd tea.Cmd
	if a.localIdentity != nil {
		autoConnectCmd = a.autoConnectServer(newServer.ID)
	}

	return tea.Batch(saveCmd, autoConnectCmd)
}

// updateAddServerForm updates the Add Server form state
func (a *App) updateAddServerForm(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd

	switch a.addServerFocus {
	case 0:
		a.addServerName, cmd = a.addServerName.Update(msg)
	case 1:
		a.addServerAddress, cmd = a.addServerAddress.Update(msg)
	case 2:
		a.addServerPort, cmd = a.addServerPort.Update(msg)
	}

	return cmd
}

// cycleAddServerFocus cycles through the Add Server form fields
func (a *App) cycleAddServerFocus() {
	a.addServerFocus = (a.addServerFocus + 1) % 4

	// Update field focus states
	a.addServerName.Blur()
	a.addServerAddress.Blur()
	a.addServerPort.Blur()

	switch a.addServerFocus {
	case 0:
		a.addServerName.Focus()
	case 1:
		a.addServerAddress.Focus()
	case 2:
		a.addServerPort.Focus()
	case 3:
		// TLS toggle - no textinput to focus
	}
}

// cycleAddServerFocusReverse cycles backwards through the Add Server form fields
func (a *App) cycleAddServerFocusReverse() {
	a.addServerFocus--
	if a.addServerFocus < 0 {
		a.addServerFocus = 3
	}

	// Update field focus states
	a.addServerName.Blur()
	a.addServerAddress.Blur()
	a.addServerPort.Blur()

	switch a.addServerFocus {
	case 0:
		a.addServerName.Focus()
	case 1:
		a.addServerAddress.Focus()
	case 2:
		a.addServerPort.Focus()
	case 3:
		// TLS toggle - no textinput to focus
	}
}
