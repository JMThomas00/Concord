package dashboard

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// HybridRenderer renders dashboard panels inline (not full-screen)
type HybridRenderer struct {
	systemStats     *SystemStatsView
	broadcastStats  *BroadcastStatsView
	clientList      *ClientListView
	activitySummary *ActivitySummaryView

	dashboardStartLine    int // Line number where dashboard starts
	scrollRegionStartLine int // Line number where scrolling logs begin
	panelWidth            int // Width of each panel
	panelHeight           int // Height of each panel
}

// NewHybridRenderer creates a new hybrid dashboard renderer
func NewHybridRenderer() *HybridRenderer {
	return &HybridRenderer{
		systemStats:     NewSystemStatsView(),
		broadcastStats:  NewBroadcastStatsView(),
		clientList:      NewClientListView(),
		activitySummary: NewActivitySummaryView(),
		panelWidth:      40,
		panelHeight:     8, // 8 lines total (7 panel lines + 1 trailing blank line)
	}
}

// SetDashboardStartLine sets the line number where the dashboard begins
func (h *HybridRenderer) SetDashboardStartLine(line int) {
	h.dashboardStartLine = line
}

// SetScrollRegionStartLine sets the line number where the scroll region begins
func (h *HybridRenderer) SetScrollRegionStartLine(line int) {
	h.scrollRegionStartLine = line
}

// RenderInitial prints the dashboard for the first time
func (h *HybridRenderer) RenderInitial() string {
	// Render 4 panels side-by-side
	panels := []string{
		h.systemStats.RenderCompact(),
		h.broadcastStats.RenderCompact(),
		h.clientList.RenderCompact(),
		h.activitySummary.RenderCompact(),
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, panels...)
}

// UpdateInPlace updates the dashboard without disrupting logs
func (h *HybridRenderer) UpdateInPlace() {
	// Move to dashboard start line (absolute positioning)
	fmt.Printf("\033[%d;1H", h.dashboardStartLine)

	// Clear dashboard area (panelHeight lines)
	for i := 0; i < h.panelHeight; i++ {
		fmt.Print("\033[2K") // Clear entire line
		if i < h.panelHeight-1 {
			fmt.Print("\033[B") // Move down one line
		}
	}

	// Move back to dashboard start
	fmt.Printf("\033[%d;1H", h.dashboardStartLine)

	// Render updated panels
	fmt.Print(h.RenderInitial())

	// Move cursor to scroll region start (for logs to continue in correct area)
	fmt.Printf("\033[%d;1H", h.scrollRegionStartLine)
}

// UpdateSystemStats updates the system statistics panel
func (h *HybridRenderer) UpdateSystemStats(uptime time.Duration, memoryMB uint64, goroutines int, connections int, maxConn int, messageRate float64) {
	h.systemStats.uptime = uptime
	h.systemStats.memoryMB = memoryMB
	h.systemStats.goroutines = goroutines
	h.systemStats.connections = connections
	h.systemStats.maxConn = maxConn
	h.systemStats.messageRate = messageRate
}

// UpdateBroadcastStats updates the broadcast statistics panel
func (h *HybridRenderer) UpdateBroadcastStats(channelCounts map[string]int) {
	h.broadcastStats.Update(channelCounts)
}

// UpdateClientList updates the connected clients panel
func (h *HybridRenderer) UpdateClientList(clients []*ClientInfo) {
	h.clientList.Update(clients)
}

// UpdateActivitySummary updates the activity summary panel
func (h *HybridRenderer) UpdateActivitySummary(lastMsg string, lastConn string, totalEvents int) {
	h.activitySummary.lastMessage = lastMsg
	h.activitySummary.lastConnection = lastConn
	h.activitySummary.totalEvents = totalEvents
}
