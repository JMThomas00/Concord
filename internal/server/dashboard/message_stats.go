package dashboard

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// MessageStatsView displays message statistics
type MessageStatsView struct {
	totalMessages int
	channelCounts map[string]int
	peakRate      float64
	avgRate       float64

	width  int
	height int
}

// NewMessageStatsView creates a new message stats view
func NewMessageStatsView() *MessageStatsView {
	return &MessageStatsView{
		channelCounts: make(map[string]int),
	}
}

// Update updates the message statistics
func (m *MessageStatsView) Update(totalMessages int, channelCounts map[string]int, peakRate float64, avgRate float64) {
	m.totalMessages = totalMessages
	m.channelCounts = channelCounts
	m.peakRate = peakRate
	m.avgRate = avgRate
}

// Resize adjusts the view dimensions
func (m *MessageStatsView) Resize(width, height int) {
	m.width = width
	m.height = height
}

// Render returns the rendered view
func (m *MessageStatsView) Render() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("220")). // Yellow
		Padding(1, 2).
		Width(m.width - 2).
		Height(m.height - 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("220")).
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")) // Pink

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	content := titleStyle.Render("MESSAGE STATS (24h)") + "\n\n"

	content += fmt.Sprintf("%s %s\n\n",
		labelStyle.Render("Total Messages:"),
		valueStyle.Render(fmt.Sprintf("%d", m.totalMessages)))

	// Sort channels by message count (descending), then by name for stability
	type channelStat struct {
		name  string
		count int
	}
	channels := make([]channelStat, 0, len(m.channelCounts))
	for name, count := range m.channelCounts {
		channels = append(channels, channelStat{name, count})
	}
	sort.Slice(channels, func(i, j int) bool {
		// Sort by count (descending), then by name (ascending) for stable ordering
		if channels[i].count != channels[j].count {
			return channels[i].count > channels[j].count
		}
		return channels[i].name < channels[j].name
	})

	// Display top channels (up to height - 10 to leave room for totals)
	maxChannels := m.height - 10
	if maxChannels < 1 {
		maxChannels = 1
	}

	displayCount := len(channels)
	if displayCount > maxChannels {
		displayCount = maxChannels
	}

	for i := 0; i < displayCount; i++ {
		ch := channels[i]
		content += fmt.Sprintf("#%-12s %s\n",
			ch.name,
			valueStyle.Render(fmt.Sprintf("%d", ch.count)))
	}

	if len(channels) > maxChannels {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		content += dimStyle.Render(fmt.Sprintf("... and %d more channels\n", len(channels)-maxChannels))
	}

	content += "\n"
	content += fmt.Sprintf("%s %s\n",
		labelStyle.Render("Peak Rate: "),
		valueStyle.Render(fmt.Sprintf("%.1f msg/s", m.peakRate)))

	content += fmt.Sprintf("%s %s",
		labelStyle.Render("Avg Rate:  "),
		valueStyle.Render(fmt.Sprintf("%.1f msg/s", m.avgRate)))

	return borderStyle.Render(content)
}
