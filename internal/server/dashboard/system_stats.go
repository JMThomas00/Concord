package dashboard

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// SystemStatsView displays system statistics
type SystemStatsView struct {
	uptime      time.Duration
	memoryMB    uint64
	goroutines  int
	connections int
	maxConn     int
	messageRate float64

	width  int
	height int
}

// NewSystemStatsView creates a new system stats view
func NewSystemStatsView() *SystemStatsView {
	return &SystemStatsView{
		maxConn: 1000, // Default max connections
	}
}

// Update updates the system stats
func (s *SystemStatsView) Update(uptime time.Duration, memoryMB uint64, goroutines int, connections int, maxConn int, messageRate float64) {
	s.uptime = uptime
	s.memoryMB = memoryMB
	s.goroutines = goroutines
	s.connections = connections
	s.maxConn = maxConn
	s.messageRate = messageRate
}

// Resize adjusts the view dimensions
func (s *SystemStatsView) Resize(width, height int) {
	s.width = width
	s.height = height
}

// Render returns the rendered view
func (s *SystemStatsView) Render() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")). // Cyan
		Padding(1, 2).
		Width(s.width - 2).
		Height(s.height - 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")) // Pink

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	content := titleStyle.Render("SYSTEM STATS") + "\n\n"

	content += fmt.Sprintf("%s  %s\n",
		labelStyle.Render("Uptime:      "),
		valueStyle.Render(s.uptime.Round(time.Second).String()))

	content += fmt.Sprintf("%s  %s\n",
		labelStyle.Render("Memory:      "),
		valueStyle.Render(fmt.Sprintf("%d MB", s.memoryMB)))

	content += fmt.Sprintf("%s  %s\n",
		labelStyle.Render("Goroutines:  "),
		valueStyle.Render(fmt.Sprintf("%d", s.goroutines)))

	content += fmt.Sprintf("%s  %s\n",
		labelStyle.Render("Connections: "),
		valueStyle.Render(fmt.Sprintf("%d / %d", s.connections, s.maxConn)))

	content += fmt.Sprintf("%s  %s",
		labelStyle.Render("Messages/s:  "),
		valueStyle.Render(fmt.Sprintf("%.1f", s.messageRate)))

	return borderStyle.Render(content)
}

// RenderCompact renders a compact version for horizontal layout
func (s *SystemStatsView) RenderCompact() string {
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

	content := titleStyle.Render("SYSTEM STATS")

	// Format uptime compactly
	uptime := s.uptime.Round(time.Second)
	uptimeStr := formatDuration(uptime)

	// Combine uptime and memory on one line
	content += "\n" + valueStyle.Render(fmt.Sprintf("Up:%-8s Mem:%dMB", uptimeStr, s.memoryMB))
	// Combine connections and message rate on one line
	content += "\n" + valueStyle.Render(fmt.Sprintf("Conn:%d/%d Msg/s:%.1f", s.connections, s.maxConn, s.messageRate))

	return borderStyle.Render(content)
}

// formatDuration formats a duration compactly (e.g., "10m", "2h 34m", "1d 5h")
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		hours := int(d.Hours())
		mins := int(d.Minutes()) % 60
		if mins == 0 {
			return fmt.Sprintf("%dh", hours)
		}
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, hours)
}
