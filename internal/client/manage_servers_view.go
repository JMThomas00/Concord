package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// renderManageServersView renders the Manage Servers modal dialog
func (a *App) renderManageServersView() string {
	width := a.width
	height := a.height

	// Modal dimensions
	modalWidth := 80
	modalHeight := 30

	// Calculate centering
	leftPadding := (width - modalWidth) / 2
	topPadding := (height - modalHeight) / 2

	if leftPadding < 0 {
		leftPadding = 0
	}
	if topPadding < 0 {
		topPadding = 0
	}

	// Build modal content
	var content strings.Builder

	// Header
	header := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(modalWidth - 4).
		Align(lipgloss.Center).
		Render("Manage Servers")

	content.WriteString(header + "\n\n")

	// Instructions
	instructions := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render("↑/↓: Navigate  •  Shift+↑/↓: Reorder  •  P: Ping  •  E: Edit  •  D: Delete  •  Enter: Select  •  Esc: Close")

	content.WriteString(instructions + "\n\n")

	// Separator
	separator := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Render(strings.Repeat("─", modalWidth-4))
	content.WriteString(separator + "\n\n")

	// Server list
	servers := a.configMgr.GetClientServers()

	if len(servers) == 0 {
		emptyMsg := lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Render("No servers configured. Press Enter on 'Add New Server' below.")
		content.WriteString(emptyMsg + "\n\n")
	} else {
		for i, server := range servers {
			selected := i == a.manageServersFocus
			serverRow := a.renderServerRow(server, selected)
			content.WriteString(serverRow + "\n")
		}
		content.WriteString("\n")
	}

	// Add new server button
	addButtonSelected := a.manageServersFocus == len(servers)
	addButton := a.renderAddServerButton(addButtonSelected)
	content.WriteString(addButton + "\n")

	// Modal box
	modalStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(1, 2).
		Width(modalWidth).
		Height(modalHeight)

	modal := modalStyle.Render(content.String())

	// Center the modal
	paddedModal := lipgloss.NewStyle().
		PaddingLeft(leftPadding).
		PaddingTop(topPadding).
		Render(modal)

	return paddedModal
}

// renderServerRow renders a single server row with status indicator
func (a *App) renderServerRow(server *ClientServerInfo, selected bool) string {
	// Status indicator
	var statusIcon string
	var statusText string

	if a.pingResults != nil {
		if result, exists := a.pingResults[server.ID]; exists {
			if result.InProgress {
				statusIcon = "◐"
				statusText = "(pinging...)"
			} else if result.Success {
				statusIcon = "●"
				statusText = fmt.Sprintf("(✓ %dms)", result.Latency.Milliseconds())
			} else {
				statusIcon = "○"
				statusText = fmt.Sprintf("(✗ %s)", result.Error)
			}
		} else {
			statusIcon = "○"
			statusText = ""
		}
	} else {
		statusIcon = "○"
		statusText = ""
	}

	// Server info
	protocol := "http"
	if server.UseTLS {
		protocol = "https"
	}
	serverAddr := fmt.Sprintf("%s://%s:%d", protocol, server.Address, server.Port)

	// Build row
	nameStyle := lipgloss.NewStyle().Bold(true)
	if selected {
		nameStyle = nameStyle.Foreground(lipgloss.Color(a.theme.Colors.Purple))
	}

	row := fmt.Sprintf("  %s  %s  %s %s",
		statusIcon,
		nameStyle.Render(server.Name),
		serverAddr,
		statusText,
	)

	if selected {
		row = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Render("▶ ") + row[2:]
	}

	return row
}

// renderAddServerButton renders the "Add New Server" button
func (a *App) renderAddServerButton(selected bool) string {
	buttonText := "+ Add New Server"

	if selected {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Purple)).
			Bold(true).
			Render("▶ " + buttonText)
	}

	return "  " + buttonText
}

// handleManageServersKey handles keyboard input in Manage Servers view
func (a *App) handleManageServersKey(msg tea.KeyMsg) tea.Cmd {
	servers := a.configMgr.GetClientServers()
	maxIndex := len(servers) // Includes "Add New Server" button

	switch msg.String() {
	case "esc":
		// Return to previous view (main if logged in, login otherwise)
		if a.activeConn != nil && a.activeConn.GetState() == StateReady {
			a.view = ViewMain
		} else {
			a.view = ViewLogin
		}
		return nil

	case "up":
		if a.manageServersFocus > 0 {
			a.manageServersFocus--
		}
		return nil

	case "down":
		if a.manageServersFocus < maxIndex {
			a.manageServersFocus++
		}
		return nil

	case "shift+up":
		// Move focused server up in the list
		i := a.manageServersFocus
		if i > 0 && i < len(servers) && i < len(a.clientServers) {
			a.clientServers[i], a.clientServers[i-1] = a.clientServers[i-1], a.clientServers[i]
			a.manageServersFocus--
			a.saveServersOrder()
		}
		return nil

	case "shift+down":
		// Move focused server down in the list
		i := a.manageServersFocus
		if i < len(servers)-1 && i+1 < len(a.clientServers) {
			a.clientServers[i], a.clientServers[i+1] = a.clientServers[i+1], a.clientServers[i]
			a.manageServersFocus++
			a.saveServersOrder()
		}
		return nil

	case "p", "P":
		// Ping selected server
		if a.manageServersFocus < len(servers) {
			server := servers[a.manageServersFocus]

			// Initialize ping results map if needed
			if a.pingResults == nil {
				a.pingResults = make(map[uuid.UUID]*PingResult)
			}

			// Mark as in progress
			a.pingResults[server.ID] = &PingResult{InProgress: true}

			return PingServerCmd(server)
		}
		return nil

	case "e", "E":
		// Edit selected server
		if a.manageServersFocus < len(servers) {
			server := servers[a.manageServersFocus]

			// Find this server in clientServers
			for i, cs := range a.clientServers {
				if cs.ID == server.ID {
					a.editingServerIndex = i
					serverID := cs.ID
					a.editingServerID = &serverID
					break
				}
			}

			// Pre-fill add server form with existing values
			a.initAddServerForm()
			a.addServerName.SetValue(server.Name)
			a.addServerAddress.SetValue(server.Address)
			a.addServerPort.SetValue(fmt.Sprintf("%d", server.Port))
			a.addServerUseTLS = server.UseTLS
			a.addServerName.Focus()
			a.view = ViewAddServer
		}
		return nil

	case "d", "D":
		// Delete selected server
		if a.manageServersFocus < len(servers) {
			server := servers[a.manageServersFocus]

			// Remove from config
			a.configMgr.RemoveServer(server.ID)

			// Remove from in-memory client servers list
			for i, cs := range a.clientServers {
				if cs.ID == server.ID {
					a.clientServers = append(a.clientServers[:i], a.clientServers[i+1:]...)
					break
				}
			}

			// If deleted server was active or current, clear state
			if a.activeConn != nil && a.activeConn.ServerID == server.ID {
				a.activeConn = nil
				a.currentServer = nil
				a.currentChannel = nil
			}
			if a.currentClientServer != nil && a.currentClientServer.ID == server.ID {
				if len(a.clientServers) > 0 {
					a.currentClientServer = a.clientServers[0]
				} else {
					a.currentClientServer = nil
				}
			}

			// Adjust server index if needed
			if a.serverIndex >= len(a.clientServers) && a.serverIndex > 0 {
				a.serverIndex--
			}

			// Adjust manage servers focus if needed
			if a.manageServersFocus >= len(a.clientServers) && a.manageServersFocus > 0 {
				a.manageServersFocus--
			}

			// Clear ping result
			if a.pingResults != nil {
				delete(a.pingResults, server.ID)
			}
		}
		return nil

	case "enter":
		if a.manageServersFocus == len(servers) {
			// "Add New Server" button selected - clear editing state
			a.editingServerID = nil
			a.initAddServerForm()
			a.addServerName.Focus()
			a.view = ViewAddServer
			return nil
		}
		// Select server as login target and go to login view
		if a.manageServersFocus < len(servers) {
			server := servers[a.manageServersFocus]
			// Find index in clientServers
			for i, cs := range a.clientServers {
				if cs.ID == server.ID {
					a.serverIndex = i
					a.currentClientServer = cs
					break
				}
			}
			a.view = ViewLogin
			a.initLoginView()
		}
		return nil
	}

	return nil
}
