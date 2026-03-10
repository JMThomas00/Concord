package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

// setupModel is a minimal bubbletea model for first-run server configuration.
type setupModel struct {
	inputs  []textinput.Model
	focused int
	done    bool
	err     string
	width   int
	height  int
}

const (
	fieldName = iota
	fieldHost
	fieldPort
	fieldDB
	fieldAdminEmail
	numFields
)

func newSetupModel(existingConfig *server.Config) setupModel {
	hostname, _ := os.Hostname()

	inputs := make([]textinput.Model, numFields)

	// Server Name
	inputs[fieldName] = textinput.New()
	inputs[fieldName].Placeholder = "Concord Server"
	if existingConfig != nil && existingConfig.ServerName != "" {
		inputs[fieldName].SetValue(existingConfig.ServerName)
	} else {
		inputs[fieldName].SetValue(hostname)
	}
	inputs[fieldName].Focus()
	inputs[fieldName].CharLimit = 64

	// Bind Host
	inputs[fieldHost] = textinput.New()
	inputs[fieldHost].Placeholder = "0.0.0.0"
	if existingConfig != nil && existingConfig.Host != "" {
		inputs[fieldHost].SetValue(existingConfig.Host)
	} else {
		inputs[fieldHost].SetValue("0.0.0.0")
	}
	inputs[fieldHost].CharLimit = 64

	// Port
	inputs[fieldPort] = textinput.New()
	inputs[fieldPort].Placeholder = "8080"
	if existingConfig != nil && existingConfig.Port != 0 {
		inputs[fieldPort].SetValue(fmt.Sprintf("%d", existingConfig.Port))
	} else {
		inputs[fieldPort].SetValue("8080")
	}
	inputs[fieldPort].CharLimit = 5

	// Database Path
	inputs[fieldDB] = textinput.New()
	inputs[fieldDB].Placeholder = "concord.db"
	if existingConfig != nil && existingConfig.DatabasePath != "" {
		inputs[fieldDB].SetValue(existingConfig.DatabasePath)
	} else {
		inputs[fieldDB].SetValue("concord.db")
	}
	inputs[fieldDB].CharLimit = 128

	// Admin Email
	inputs[fieldAdminEmail] = textinput.New()
	inputs[fieldAdminEmail].Placeholder = "admin@example.com (optional)"
	if existingConfig != nil && existingConfig.AdminEmail != "" {
		inputs[fieldAdminEmail].SetValue(existingConfig.AdminEmail)
	}
	inputs[fieldAdminEmail].CharLimit = 255

	return setupModel{inputs: inputs}
}

func (m setupModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			fmt.Fprintln(os.Stderr, "Setup cancelled.")
			os.Exit(1)

		case "tab", "down", "enter":
			if msg.String() == "enter" && m.focused == numFields-1 {
				// Validate port
				port := strings.TrimSpace(m.inputs[fieldPort].Value())
				if _, err := strconv.Atoi(port); err != nil {
					m.err = "Port must be a number."
					return m, nil
				}
				m.done = true
				return m, tea.Quit
			}
			m.inputs[m.focused].Blur()
			m.focused = (m.focused + 1) % numFields
			m.inputs[m.focused].Focus()

		case "shift+tab", "up":
			m.inputs[m.focused].Blur()
			m.focused = (m.focused - 1 + numFields) % numFields
			m.inputs[m.focused].Focus()
		}
	}

	// Forward key events to focused input
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}


func (m setupModel) View() string {
	var content strings.Builder

	width := 100 // Fixed width for consistent layout

	// Banner
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(width)

	content.WriteString(bannerStyle.Render("CONCORD SERVER"))
	content.WriteString("\n\n")

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(width)

	// Dynamic title based on whether we're editing or creating
	title := "First-Run Setup"
	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")

	// Instructions
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(width)

	content.WriteString(instructionStyle.Render("Configure your Concord server"))
	content.WriteString("\n\n")

	// Form container
	formStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#BD93F9")).
		Padding(2, 4).
		Width(width - 10)

	var formContent strings.Builder

	labels := []string{"Server Name", "Bind Host", "Port", "Database Path", "Admin Email"}
	descriptions := []string{
		"The name of your server (visible to users)",
		"IP address to bind to (0.0.0.0 for all interfaces)",
		"Port number for the server (default: 8080)",
		"Path to SQLite database file",
		"Admin email for recovery (optional)",
	}

	for i, label := range labels {
		// Label with description
		labelStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#bd93f9")).
			Bold(true)

		descStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6272a4")).
			Italic(true)

		formContent.WriteString(labelStyled.Render(label))
		formContent.WriteString("\n")
		formContent.WriteString(descStyle.Render(descriptions[i]))
		formContent.WriteString("\n")

		// Input field
		inputStyle := lipgloss.NewStyle().
			Padding(0, 1)

		formContent.WriteString(inputStyle.Render(m.inputs[i].View()))

		if i < len(labels)-1 {
			formContent.WriteString("\n\n")
		}
	}

	content.WriteString(lipgloss.PlaceHorizontal(
		width,
		lipgloss.Center,
		formStyle.Render(formContent.String()),
	))
	content.WriteString("\n\n")

	// Error message if any
	if m.err != "" {
		errStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Bold(true).
			Align(lipgloss.Center).
			Width(width)

		content.WriteString(errStyled.Render("⚠ " + m.err))
		content.WriteString("\n\n")
	}

	// Footer hints
	hintStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(width)

	content.WriteString(hintStyled.Render("[Tab] Next Field · [Shift+Tab] Previous Field · [Enter] Confirm · [Esc] Cancel"))

	// Center the entire content both horizontally and vertically
	finalContent := content.String()

	// Use terminal dimensions if available, otherwise use defaults
	termWidth := m.width
	termHeight := m.height
	if termWidth == 0 {
		termWidth = 120
	}
	if termHeight == 0 {
		termHeight = 30
	}

	// Center horizontally and vertically
	centered := lipgloss.Place(
		termWidth,
		termHeight,
		lipgloss.Center,
		lipgloss.Center,
		finalContent,
	)

	return centered
}

// configFilename is the default config file written by setup.
const configFilename = "concord-server.toml"

// runFirstRunSetup runs the interactive TUI setup and returns the resulting Config.
// It also writes concord-server.toml to the working directory.
// If existingConfig is provided, it pre-populates the form with existing values.
func runFirstRunSetup(existingConfig *server.Config) *server.Config {
	m := newSetupModel(existingConfig)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Setup error: %v\n", err)
		os.Exit(1)
	}

	final := result.(setupModel)
	if !final.done {
		os.Exit(0)
	}

	port, _ := strconv.Atoi(strings.TrimSpace(final.inputs[fieldPort].Value()))
	if port == 0 {
		port = 8080
	}

	serverName := strings.TrimSpace(final.inputs[fieldName].Value())
	if serverName == "" {
		serverName = "Concord Server"
	}

	adminEmail := strings.TrimSpace(final.inputs[fieldAdminEmail].Value())

	cfg := &server.Config{
		Host:           strings.TrimSpace(final.inputs[fieldHost].Value()),
		Port:           port,
		ServerName:     serverName,
		DatabasePath:   strings.TrimSpace(final.inputs[fieldDB].Value()),
		MaxConnections: 1000,
		Debug:          false,
		MessagePruning: server.MessagePruningConfig{
			Enabled:       true,
			IntervalHours: 24,
		},
		AdminEmail: adminEmail,
	}

	// Write config file
	data, err := toml.Marshal(cfg)
	if err == nil {
		_ = os.WriteFile(configFilename, data, 0644)
		fmt.Printf("\nConfig written to %s\n", configFilename)
	}

	fmt.Printf("Share this address: %s:%d\n\n", cfg.Host, cfg.Port)
	return cfg
}
