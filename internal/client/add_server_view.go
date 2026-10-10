package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// renderAddServerView renders Add Server (and Edit Server) on the login
// stage: a shell command that builds itself as you type, over the fields.
func (a *App) renderAddServerView() string {
	label, headline, accent := "Add a server", "Where's your server?", "server"
	if a.editingServerID != nil {
		label, headline, accent = "Edit server", "Change where it lives", "lives"
	}
	var b strings.Builder
	b.WriteString(a.addServerCommand() + "\n\n")
	b.WriteString(a.stageField("Name", a.addServerFocus == 0, a.addServerName.View()) + "\n")
	b.WriteString(a.stageField("Address", a.addServerFocus == 1, a.addServerAddress.View()) + "\n")
	b.WriteString(a.stageField("Port", a.addServerFocus == 2, a.addServerPort.View()) + "\n")
	tls := "○ off   (ws://)"
	if a.addServerUseTLS {
		tls = "● on    (wss://)"
	}
	tlsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
	if a.addServerFocus == 3 {
		tlsStyle = tlsStyle.Foreground(lipgloss.Color(a.theme.Colors.Purple)).Bold(true)
	}
	b.WriteString(a.stageField("TLS", a.addServerFocus == 3, tlsStyle.Render(tls)) + "\n")
	if a.addServerError != "" {
		b.WriteString("\n" + a.stageNotice(a.addServerError, true))
	}
	hints := []keyHint{{"Tab", "Next"}, {"Space", "TLS"}, {"Enter", "Connect"}, {"Ctrl+G", "Discover"}, {"Esc", "Cancel"}}
	if a.editingServerID != nil {
		hints[2] = keyHint{"Enter", "Save"}
	}
	return a.stagePage(label, headline, accent, b.String(), hints)
}

// addServerCommand is the form as a shell command, built as you type:
// $ concord connect wss://host:port --name "My Server"
func (a *App) addServerCommand() string {
	c := a.theme.Colors
	st := func(col string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(col)) }
	scheme := "ws://"
	if a.addServerUseTLS {
		scheme = "wss://"
	}
	host := strings.TrimSpace(a.addServerAddress.Value())
	hostPart := st(c.Purple).Bold(true).Render(host)
	if host == "" {
		hostPart = st(c.Comment).Render("host")
	}
	port := strings.TrimSpace(a.addServerPort.Value())
	if port == "" {
		port = "8080"
	}
	cmd := st(c.Green).Bold(true).Render("$ ") + st(c.Foreground).Render("concord connect ") +
		st(c.Comment).Render(scheme) + hostPart + st(c.Comment).Render(":") + st(c.Pink).Render(port)
	if name := strings.TrimSpace(a.addServerName.Value()); name != "" {
		cmd += st(c.Comment).Render(" --name ") + st(c.Yellow).Render(fmt.Sprintf("%q", name))
	}
	if time.Now().UnixMilli()/530%2 == 0 {
		cmd += st(c.Foreground).Render("▌")
	}
	return ansi.Truncate(cmd, stageWidth, "…")
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
			// Return to appropriate view based on context
			if a.settingsState != nil {
				a.settingsState.ServerFormOpen = false
				a.view = ViewSettings
			} else if a.serverManagementState != nil {
				a.view = ViewServerManagement
			} else {
				a.view = ViewMain
			}
			a.addServerError = ""

			return func() tea.Msg {
				if err := a.configMgr.UpdateServer(updatedServer); err != nil {
					return ErrorMsg{Error: fmt.Sprintf("Failed to save server: %v", err)}
				}
				return nil
			}
		}
		a.editingServerID = nil
		// Return to appropriate view based on context
		if a.settingsState != nil {
			a.settingsState.ServerFormOpen = false
			a.view = ViewSettings
		} else if a.serverManagementState != nil {
			a.view = ViewServerManagement
		} else {
			a.view = ViewMain
		}
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

	// Return to appropriate view based on context
	if a.settingsState != nil {
		a.settingsState.ServerFormOpen = false
		a.view = ViewSettings
	} else if a.serverManagementState != nil {
		a.view = ViewServerManagement
	} else {
		a.view = ViewMain
	}
	a.statusMessage = fmt.Sprintf("Server '%s' added. Connecting...", name)
	if a.view == ViewMain && a.localIdentity != nil {
		a.startConnecting([]*ClientServerInfo{newServer}) // its handshake
	}

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
