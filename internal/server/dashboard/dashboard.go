package dashboard

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model represents the dashboard TUI state
type Model struct {
	// UI components
	systemStats  *SystemStatsView
	clientList   *ClientListView
	messageStats *MessageStatsView
	activityFeed *ActivityFeedView

	// State
	width      int
	height     int
	lastUpdate time.Time
	ready      bool
}

// tickMsg is sent every second to trigger updates
type tickMsg time.Time

// NewModel creates a new dashboard model
func NewModel() *Model {
	return &Model{
		systemStats:  NewSystemStatsView(),
		clientList:   NewClientListView(),
		messageStats: NewMessageStatsView(),
		activityFeed: NewActivityFeedView(),
		lastUpdate:   time.Now(),
	}
}

// Init initializes the dashboard
func (m Model) Init() tea.Cmd {
	return tick()
}

// tick returns a command that sends a tickMsg every second
func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles messages and updates the model
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			// Force refresh
			m.lastUpdate = time.Now()
		}

	case tickMsg:
		// Auto-refresh every second
		m.lastUpdate = time.Now()
		return m, tick()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true
		m.resize()
	}

	return m, nil
}

// resize adjusts component sizes based on terminal dimensions
func (m *Model) resize() {
	// Calculate panel dimensions
	// Top half: System Stats (left) | Connected Clients (right)
	// Bottom half: Message Stats (left) | Activity Feed (right)

	topHeight := (m.height - 4) / 2    // -4 for header and footer
	bottomHeight := m.height - topHeight - 4
	leftWidth := m.width / 2
	rightWidth := m.width - leftWidth

	m.systemStats.Resize(leftWidth, topHeight)
	m.clientList.Resize(rightWidth, topHeight)
	m.messageStats.Resize(leftWidth, bottomHeight)
	m.activityFeed.Resize(rightWidth, bottomHeight)
}

// View renders the dashboard
func (m Model) View() string {
	if !m.ready {
		return "Initializing dashboard..."
	}

	// Render header
	header := m.renderHeader()

	// Render panels in 2x2 grid
	topLeft := m.systemStats.Render()
	topRight := m.clientList.Render()
	bottomLeft := m.messageStats.Render()
	bottomRight := m.activityFeed.Render()

	// Combine top row
	topRow := lipgloss.JoinHorizontal(lipgloss.Top, topLeft, topRight)

	// Combine bottom row
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, bottomLeft, bottomRight)

	// Stack vertically
	content := lipgloss.JoinVertical(lipgloss.Left, topRow, bottomRow)

	// Render footer
	footer := m.renderFooter()

	// Combine all sections
	return lipgloss.JoinVertical(lipgloss.Left, header, content, footer)
}

// renderHeader renders the dashboard header
func (m Model) renderHeader() string {
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Padding(0, 1).
		Width(m.width).
		Align(lipgloss.Center)

	return headerStyle.Render("CONCORD SERVER DASHBOARD v0.1.0")
}

// renderFooter renders the dashboard footer with keyboard hints
func (m Model) renderFooter() string {
	footerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(m.width)

	hints := "[q] Quit  [r] Refresh  [↑/↓] Scroll  [?] Help"
	return footerStyle.Render(hints)
}

// UpdateSystemStats updates the system statistics view
func (m *Model) UpdateSystemStats(uptime time.Duration, memoryMB uint64, goroutines int, connections int, maxConn int, messageRate float64) {
	m.systemStats.Update(uptime, memoryMB, goroutines, connections, maxConn, messageRate)
}

// UpdateClientList updates the connected clients list
func (m *Model) UpdateClientList(clients []*ClientInfo) {
	m.clientList.Update(clients)
}

// UpdateMessageStats updates the message statistics view
func (m *Model) UpdateMessageStats(totalMessages int, channelCounts map[string]int, peakRate float64, avgRate float64) {
	m.messageStats.Update(totalMessages, channelCounts, peakRate, avgRate)
}

// AddActivityEvent adds an event to the activity feed
func (m *Model) AddActivityEvent(level, component, message string) {
	m.activityFeed.AddEvent(level, component, message)
}

// ClientInfo represents information about a connected client
type ClientInfo struct {
	Username      string
	Discriminator string
	Status        string // online, idle, dnd, offline
	Activity      string // "Typing in #general", "Idle", etc.
	LastSeen      time.Time
}
