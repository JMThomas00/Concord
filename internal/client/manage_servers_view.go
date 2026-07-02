package client

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// renderManageServersView renders the Manage Servers modal dialog
func (a *App) renderManageServersView() string {
	// If delete confirmation dialog is shown, render it on top
	if a.deleteConfirmServerID != nil {
		return a.renderDeleteServerConfirmation()
	}

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
		Render("↑/↓: Navigate  •  Shift+↑/↓: Reorder  •  P: Ping  •  E: Edit  •  D: Delete  •  B: Browse Hub  •  Enter: Select  •  Esc: Close")

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

// renderDeleteServerConfirmation renders the delete confirmation dialog
func (a *App) renderDeleteServerConfirmation() string {
	// Find the server being deleted
	var serverName string
	if a.deleteConfirmServerID != nil {
		for _, cs := range a.clientServers {
			if cs.ID == *a.deleteConfirmServerID {
				serverName = cs.Name
				break
			}
		}
	}

	// Dialog dimensions
	dialogWidth := 60
	dialogHeight := 10

	// Calculate centering
	leftPadding := (a.width - dialogWidth) / 2
	topPadding := (a.height - dialogHeight) / 2

	if leftPadding < 0 {
		leftPadding = 0
	}
	if topPadding < 0 {
		topPadding = 0
	}

	// Build dialog content
	var content strings.Builder

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Red)).
		Bold(true).
		Align(lipgloss.Center).
		Width(dialogWidth - 4)

	content.WriteString(titleStyle.Render("Delete Server"))
	content.WriteString("\n\n")

	// Message
	msgStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	message := fmt.Sprintf("Are you sure you want to delete \"%s\"?", serverName)
	content.WriteString(msgStyle.Render(message))
	content.WriteString("\n")

	warningStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center).
		Italic(true)

	content.WriteString(warningStyle.Render("This action cannot be undone."))
	content.WriteString("\n\n")

	// Buttons
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(dialogWidth - 4).
		Align(lipgloss.Center)

	content.WriteString(helpStyle.Render("[Y] Yes, delete  •  [N] Cancel"))

	// Wrap in dialog box
	dialogStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Red)).
		Padding(1, 2).
		Width(dialogWidth).
		Height(dialogHeight)

	dialog := dialogStyle.Render(content.String())

	// Center the dialog on screen
	centeredDialog := lipgloss.NewStyle().
		PaddingLeft(leftPadding).
		PaddingTop(topPadding).
		Render(dialog)

	return centeredDialog
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
				if result.Attempts > 0 {
					statusText = fmt.Sprintf("(✗ %d attempts)", result.Attempts)
				} else {
					statusText = fmt.Sprintf("(✗ failed)")
				}
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
		// If delete confirmation is shown, cancel it
		if a.deleteConfirmServerID != nil {
			a.deleteConfirmServerID = nil
			return nil
		}

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
		// Show delete confirmation dialog
		if a.manageServersFocus < len(servers) {
			server := servers[a.manageServersFocus]
			serverID := server.ID
			a.deleteConfirmServerID = &serverID
		}
		return nil

	case "y", "Y":
		// Confirm delete (when confirmation dialog is shown)
		if a.deleteConfirmServerID != nil {
			serverID := *a.deleteConfirmServerID
			a.deleteConfirmServerID = nil

			// Find the server to delete
			var serverToDelete *ClientServerInfo
			for _, cs := range a.clientServers {
				if cs.ID == serverID {
					serverToDelete = cs
					break
				}
			}

			if serverToDelete != nil {
				// Remove from config
				a.configMgr.RemoveServer(serverID)

				// Remove from in-memory client servers list
				for i, cs := range a.clientServers {
					if cs.ID == serverID {
						a.clientServers = append(a.clientServers[:i], a.clientServers[i+1:]...)
						break
					}
				}

				// If deleted server was active or current, clear state
				if a.activeConn != nil && a.activeConn.ServerID == serverID {
					oldConn := a.activeConn
					a.activeConn = nil
					log.Printf("DEBUG handleManageServersKey (delete): Changed activeConn from %p to nil (deleted serverID=%s)",
						oldConn, serverID)
					a.currentServer = nil
					a.currentChannel = nil
				}
				if a.currentClientServer != nil && a.currentClientServer.ID == serverID {
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
					delete(a.pingResults, serverID)
				}
			}
		}
		return nil

	case "b", "B":
		if a.deleteConfirmServerID == nil {
			return a.openHubBrowser()
		}
		return nil

	case "n", "N":
		// Cancel delete confirmation
		if a.deleteConfirmServerID != nil {
			a.deleteConfirmServerID = nil
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
