package dashboard

import (
	"fmt"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// ActivityFeedView displays real-time activity log
type ActivityFeedView struct {
	events []ActivityEvent
	mu     sync.RWMutex

	width  int
	height int
}

// ActivityEvent represents a single activity event
type ActivityEvent struct {
	Timestamp time.Time
	Level     string // INFO, WARN, ERROR
	Component string // AUTH, MSG, HUB, etc.
	Message   string
}

// NewActivityFeedView creates a new activity feed view
func NewActivityFeedView() *ActivityFeedView {
	return &ActivityFeedView{
		events: make([]ActivityEvent, 0, 1000),
	}
}

// AddEvent adds a new event to the activity feed
func (a *ActivityFeedView) AddEvent(level, component, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()

	event := ActivityEvent{
		Timestamp: time.Now(),
		Level:     level,
		Component: component,
		Message:   message,
	}

	a.events = append(a.events, event)

	// Keep only last 1000 events
	if len(a.events) > 1000 {
		a.events = a.events[len(a.events)-1000:]
	}
}

// Resize adjusts the view dimensions
func (a *ActivityFeedView) Resize(width, height int) {
	a.width = width
	a.height = height
}

// Render returns the rendered view
func (a *ActivityFeedView) Render() string {
	a.mu.RLock()
	defer a.mu.RUnlock()

	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("86")). // Cyan
		Padding(1, 2).
		Width(a.width - 2).
		Height(a.height - 2)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	content := titleStyle.Render("ACTIVITY FEED") + "\n\n"

	if len(a.events) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
		content += dimStyle.Render("No activity yet")
	} else {
		// Calculate how many events we can display
		maxEvents := a.height - 6 // Account for border, title, padding
		if maxEvents < 1 {
			maxEvents = 1
		}

		// Show most recent events (from the end of the slice)
		startIdx := len(a.events) - maxEvents
		if startIdx < 0 {
			startIdx = 0
		}

		for i := startIdx; i < len(a.events); i++ {
			event := a.events[i]
			content += a.renderEvent(event) + "\n"
		}
	}

	return borderStyle.Render(content)
}

// renderEvent renders a single event
func (a *ActivityFeedView) renderEvent(event ActivityEvent) string {
	timeStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	timeStr := event.Timestamp.Format("15:04:05")

	icon := a.getIcon(event.Level)
	iconStyle := lipgloss.NewStyle().Foreground(a.getIconColor(event.Level))

	componentStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")). // Pink
		Bold(true)

	messageStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("255"))

	return fmt.Sprintf("%s %s %s %s",
		timeStyle.Render(timeStr),
		iconStyle.Render(icon),
		componentStyle.Render(fmt.Sprintf("[%s]", event.Component)),
		messageStyle.Render(event.Message))
}

// getIcon returns the icon for a given log level
func (a *ActivityFeedView) getIcon(level string) string {
	switch level {
	case "INFO":
		return "●"
	case "WARN":
		return "⚠️"
	case "ERROR":
		return "❌"
	case "DEBUG":
		return "🔍"
	default:
		return "○"
	}
}

// getIconColor returns the color for a given log level
func (a *ActivityFeedView) getIconColor(level string) lipgloss.Color {
	switch level {
	case "INFO":
		return lipgloss.Color("86") // Cyan
	case "WARN":
		return lipgloss.Color("220") // Yellow
	case "ERROR":
		return lipgloss.Color("196") // Red
	case "DEBUG":
		return lipgloss.Color("240") // Dim
	default:
		return lipgloss.Color("255") // White
	}
}
