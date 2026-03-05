package dashboard

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ClientListView displays connected clients
type ClientListView struct {
	clients []*ClientInfo

	width  int
	height int
}

// NewClientListView creates a new client list view
func NewClientListView() *ClientListView {
	return &ClientListView{
		clients: make([]*ClientInfo, 0),
	}
}

// Update updates the client list
func (c *ClientListView) Update(clients []*ClientInfo) {
	c.clients = clients
}

// Resize adjusts the view dimensions
func (c *ClientListView) Resize(width, height int) {
	c.width = width
	c.height = height
}

// Render returns the rendered view
func (c *ClientListView) Render() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("213")). // Pink
		Padding(1, 2).
		Width(c.width - 2).
		Height(c.height - 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")).
		Bold(true)

	content := titleStyle.Render(fmt.Sprintf("CONNECTED CLIENTS (%d)", len(c.clients))) + "\n\n"

	if len(c.clients) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		content += dimStyle.Render("No clients connected")
	} else {
		// Display up to (height - 6) clients to fit in the panel
		maxClients := c.height - 6
		if maxClients < 1 {
			maxClients = 1
		}

		displayCount := len(c.clients)
		if displayCount > maxClients {
			displayCount = maxClients
		}

		for i := 0; i < displayCount; i++ {
			client := c.clients[i]
			content += c.renderClient(client) + "\n"
		}

		// Show "... and N more" if there are more clients
		if len(c.clients) > maxClients {
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
			content += dimStyle.Render(fmt.Sprintf("... and %d more", len(c.clients)-maxClients))
		}
	}

	return borderStyle.Render(content)
}

// RenderCompact renders a compact version for horizontal layout
func (c *ClientListView) RenderCompact() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("213")). // Pink
		Padding(0, 1).
		Width(38). // Fixed width for horizontal layout
		Height(5)  // Compact height (1 top + 3 content + 1 bottom)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")).
		Bold(true)

	content := titleStyle.Render(fmt.Sprintf("CLIENTS (%d)", len(c.clients))) + "\n"

	if len(c.clients) == 0 {
		content += "\nNo clients"
	} else {
		// Show top 2 clients (compact mode)
		maxDisplay := 2
		if len(c.clients) < maxDisplay {
			maxDisplay = len(c.clients)
		}

		for i := 0; i < maxDisplay; i++ {
			client := c.clients[i]

			// Status icon
			statusIcon := "●"
			statusColor := "86" // Online - cyan
			if client.Status == "offline" {
				statusIcon = "○"
				statusColor = "240" // Offline - dim
			}

			iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))

			// Truncate username to fit
			username := client.Username
			if len(username) > 12 {
				username = username[:12]
			}

			content += fmt.Sprintf("\n%s %-12s",
				iconStyle.Render(statusIcon),
				username)
		}

		// Pad remaining lines to 2 total
		for i := maxDisplay; i < 2; i++ {
			content += "\n"
		}
	}

	return borderStyle.Render(content)
}

// renderClient renders a single client entry
func (c *ClientListView) renderClient(client *ClientInfo) string {
	// Status icon
	statusIcon := "●"
	statusColor := "86" // Online - cyan
	if client.Status == "offline" {
		statusIcon = "○"
		statusColor = "240" // Offline - dim
	} else if client.Status == "idle" {
		statusColor = "220" // Idle - yellow
	} else if client.Status == "dnd" {
		statusColor = "196" // DND - red
	}

	iconStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(statusColor))

	// Username
	usernameStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")).
		Bold(true)

	username := fmt.Sprintf("%s#%s", client.Username, client.Discriminator)

	// Status badge
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	statusBadge := fmt.Sprintf("[%s]", strings.Title(client.Status))

	// Activity
	activityStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	activity := client.Activity
	if activity == "" {
		activity = "Idle"
	}

	// Combine all parts
	return fmt.Sprintf("%s %-20s %s %s",
		iconStyle.Render(statusIcon),
		usernameStyle.Render(username),
		statusStyle.Render(statusBadge),
		activityStyle.Render(activity))
}
