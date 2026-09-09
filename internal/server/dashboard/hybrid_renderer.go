package dashboard

import (
	"fmt"
	"strings"
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

// UpdateInPlace returns the ANSI sequence that redraws the dashboard in
// place without disrupting logs -- the caller writes it in one shot (e.g.
// fmt.Print(renderer.UpdateInPlace())), matching RenderInitial's own
// "build a string, let the caller print it" convention rather than this
// method writing to stdout directly, which also makes it testable without
// redirecting a real file descriptor.
//
// Overwrites each dashboard line directly rather than blanking the whole
// region first and redrawing after. The previous two-pass approach (clear
// every line via its own fmt.Print, THEN print the new content via a
// separate fmt.Print) let the terminal actually paint the fully-blanked
// intermediate frame before the real content arrived over the wire --
// visible as a flash on every ~1s update tick (updateHybridDashboardLoop,
// server.go). Fixed 2026-09-08. Since every panel is a fixed-width/height
// lipgloss box, there's no need to blank first to avoid stale leftover
// characters either -- "\033[K" (clear to end of line) after each line's
// own content handles that case without ever showing a blank frame.
func (h *HybridRenderer) UpdateInPlace() string {
	var b strings.Builder

	// Move to dashboard start line (absolute positioning).
	fmt.Fprintf(&b, "\033[%d;1H", h.dashboardStartLine)

	lines := strings.Split(h.RenderInitial(), "\n")
	for i, line := range lines {
		b.WriteString(line)
		b.WriteString("\033[K") // erase any leftover chars past this line's new content
		if i < len(lines)-1 {
			b.WriteString("\r\n") // explicit CR+LF -- reliable column-1 reset regardless of terminal LF handling
		}
	}

	// Move cursor to scroll region start (for logs to continue in correct area).
	fmt.Fprintf(&b, "\033[%d;1H", h.scrollRegionStartLine)

	return b.String()
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
