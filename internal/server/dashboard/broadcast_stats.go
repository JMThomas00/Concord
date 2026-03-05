package dashboard

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// BroadcastStatsView shows message counts per channel
type BroadcastStatsView struct {
	channelCounts map[string]int
	width         int
	height        int
}

// NewBroadcastStatsView creates a new broadcast stats view
func NewBroadcastStatsView() *BroadcastStatsView {
	return &BroadcastStatsView{
		channelCounts: make(map[string]int),
		width:         40,
		height:        6,
	}
}

// Update updates the channel message counts
func (b *BroadcastStatsView) Update(counts map[string]int) {
	b.channelCounts = counts
}

// RenderCompact renders a compact version for horizontal layout
func (b *BroadcastStatsView) RenderCompact() string {
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("220")). // Yellow
		Padding(0, 1).
		Width(38). // Fixed width for horizontal layout
		Height(5)  // Compact height (1 top + 3 content + 1 bottom)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("220")).
		Bold(true)

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	content := titleStyle.Render("BROADCAST") + "\n"

	// Sort channels by message count (descending), then by name
	type channelStat struct {
		name  string
		count int
	}
	channels := make([]channelStat, 0, len(b.channelCounts))
	for name, count := range b.channelCounts {
		channels = append(channels, channelStat{name, count})
	}
	sort.Slice(channels, func(i, j int) bool {
		if channels[i].count != channels[j].count {
			return channels[i].count > channels[j].count
		}
		return channels[i].name < channels[j].name
	})

	// Show top 2 channels (compact mode)
	if len(channels) == 0 {
		content += "\nNo channels"
	} else {
		maxDisplay := 2
		if len(channels) < maxDisplay {
			maxDisplay = len(channels)
		}

		for i := 0; i < maxDisplay; i++ {
			ch := channels[i]
			content += fmt.Sprintf("\n#%-8s %s",
				truncate(ch.name, 8),
				valueStyle.Render(fmt.Sprintf("%d", ch.count)))
		}

		// Pad remaining lines to 2 total
		for i := maxDisplay; i < 2; i++ {
			content += "\n"
		}
	}

	return borderStyle.Render(content)
}

// truncate truncates a string to a maximum length
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
