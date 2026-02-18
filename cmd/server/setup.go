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
}

const (
	fieldName = iota
	fieldHost
	fieldPort
	fieldDB
	numFields
)

func newSetupModel() setupModel {
	hostname, _ := os.Hostname()

	inputs := make([]textinput.Model, numFields)

	inputs[fieldName] = textinput.New()
	inputs[fieldName].Placeholder = "Concord Server"
	inputs[fieldName].SetValue(hostname)
	inputs[fieldName].Focus()
	inputs[fieldName].CharLimit = 64

	inputs[fieldHost] = textinput.New()
	inputs[fieldHost].Placeholder = "0.0.0.0"
	inputs[fieldHost].SetValue("0.0.0.0")
	inputs[fieldHost].CharLimit = 64

	inputs[fieldPort] = textinput.New()
	inputs[fieldPort].Placeholder = "8080"
	inputs[fieldPort].SetValue("8080")
	inputs[fieldPort].CharLimit = 5

	inputs[fieldDB] = textinput.New()
	inputs[fieldDB].Placeholder = "concord.db"
	inputs[fieldDB].SetValue("concord.db")
	inputs[fieldDB].CharLimit = 128

	return setupModel{inputs: inputs}
}

func (m setupModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m setupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

var (
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#bd93f9")).Bold(true)
	hintStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6272a4"))
	errStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#ff5555"))
	titleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#f8f8f2")).
			Background(lipgloss.Color("#bd93f9")).
			Bold(true).
			Padding(0, 2)
)

func (m setupModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("  Concord First-Run Setup  "))
	b.WriteString("\n\n")
	b.WriteString(hintStyle.Render("Tab/↑↓ to navigate · Enter on last field to confirm · Esc to cancel"))
	b.WriteString("\n\n")

	labels := []string{"Server Name", "Bind Host", "Port", "Database Path"}
	for i, label := range labels {
		b.WriteString(labelStyle.Render(label))
		b.WriteString("\n")
		b.WriteString("  " + m.inputs[i].View())
		b.WriteString("\n\n")
	}

	if m.err != "" {
		b.WriteString(errStyle.Render("  ⚠ " + m.err))
		b.WriteString("\n")
	}

	return b.String()
}

// configFilename is the default config file written by setup.
const configFilename = "concord-server.toml"

// runFirstRunSetup runs the interactive TUI setup and returns the resulting Config.
// It also writes concord-server.toml to the working directory.
func runFirstRunSetup() *server.Config {
	m := newSetupModel()
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

	cfg := &server.Config{
		Host:           strings.TrimSpace(final.inputs[fieldHost].Value()),
		Port:           port,
		DatabasePath:   strings.TrimSpace(final.inputs[fieldDB].Value()),
		MaxConnections: 1000,
		Debug:          false,
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
