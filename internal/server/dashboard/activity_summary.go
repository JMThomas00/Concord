package dashboard

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// ActivitySummaryView shows recent activity summary
type ActivitySummaryView struct {
	lastMessage    string // Time ago string like "2s"
	lastConnection string // Time ago string like "5s"
	totalEvents    int

	width  int
	height int
}

// NewActivitySummaryView creates a new activity summary view
func NewActivitySummaryView() *ActivitySummaryView {
	return &ActivitySummaryView{
		lastMessage:    "Never",
		lastConnection: "Never",
		totalEvents:    0,
		width:          40,
		height:         6,
	}
}

// RenderCompact renders a compact version for horizontal layout
func (a *ActivitySummaryView) RenderCompact() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")). // Cyan
		Padding(0, 1).
		Width(38). // Fixed width for horizontal layout
		Height(5)  // Compact height (1 top + 3 content + 1 bottom)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	content := titleStyle.Render("ACTIVITY") + "\n"

	// Combine last message and last connection on one line
	content += "\n" + valueStyle.Render(fmt.Sprintf("Msg:%s Conn:%s", a.lastMessage, a.lastConnection))
	content += "\n" + valueStyle.Render(fmt.Sprintf("Total: %d", a.totalEvents))

	return borderStyle.Render(content)
}
