package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/hub"
)

// hubSetupPhase tracks which screen of the wizard is active.
type hubSetupPhase int

const (
	hubPhaseForm     hubSetupPhase = iota // main hub settings form
	hubPhaseAdminAsk                      // "generate an admin token?" yes/no
)

const (
	hubFieldName = iota
	hubFieldHost
	hubFieldPort
	hubFieldDB
	numHubFields
)

// hubSetupModel is the bubbletea model for first-run hub configuration.
// Mirrors the style of the concord-server setup wizard.
type hubSetupModel struct {
	inputs    []textinput.Model
	focused   int
	done      bool
	cancelled bool
	err       string
	width     int
	height    int

	phase      hubSetupPhase
	adminOptIn bool
}

func newHubSetupModel(existing *hub.Config) hubSetupModel {
	defaults := hub.DefaultConfig()
	if existing == nil {
		existing = defaults
	}

	inputs := make([]textinput.Model, numHubFields)

	inputs[hubFieldName] = textinput.New()
	inputs[hubFieldName].Placeholder = defaults.HubName
	inputs[hubFieldName].SetValue(existing.HubName)
	inputs[hubFieldName].Focus()
	inputs[hubFieldName].CharLimit = 64

	inputs[hubFieldHost] = textinput.New()
	inputs[hubFieldHost].Placeholder = defaults.Host
	inputs[hubFieldHost].SetValue(existing.Host)
	inputs[hubFieldHost].CharLimit = 64

	inputs[hubFieldPort] = textinput.New()
	inputs[hubFieldPort].Placeholder = fmt.Sprintf("%d", defaults.Port)
	inputs[hubFieldPort].SetValue(fmt.Sprintf("%d", existing.Port))
	inputs[hubFieldPort].CharLimit = 5

	inputs[hubFieldDB] = textinput.New()
	inputs[hubFieldDB].Placeholder = defaults.DatabasePath
	inputs[hubFieldDB].SetValue(existing.DatabasePath)
	inputs[hubFieldDB].CharLimit = 128

	return hubSetupModel{
		inputs:     inputs,
		adminOptIn: existing.AdminToken != "",
	}
}

func (m hubSetupModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m hubSetupModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			// Quit through bubbletea (not os.Exit) so terminal modes are
			// restored; runSetupWizard exits after p.Run() returns.
			m.cancelled = true
			return m, tea.Quit
		}

		switch m.phase {
		case hubPhaseForm:
			switch msg.String() {
			case "tab", "down", "enter":
				if msg.String() == "enter" && m.focused == numHubFields-1 {
					port := strings.TrimSpace(m.inputs[hubFieldPort].Value())
					if _, err := strconv.Atoi(port); err != nil {
						m.err = "Port must be a number."
						return m, nil
					}
					m.err = ""
					m.inputs[m.focused].Blur()
					m.phase = hubPhaseAdminAsk
					return m, nil
				}
				m.inputs[m.focused].Blur()
				m.focused = (m.focused + 1) % numHubFields
				m.inputs[m.focused].Focus()

			case "shift+tab", "up":
				m.inputs[m.focused].Blur()
				m.focused = (m.focused - 1 + numHubFields) % numHubFields
				m.inputs[m.focused].Focus()
			}

		case hubPhaseAdminAsk:
			switch msg.String() {
			case "y", "Y":
				m.adminOptIn = true
				m.done = true
				return m, tea.Quit
			case "n", "N":
				m.adminOptIn = false
				m.done = true
				return m, tea.Quit
			case "left", "right", "tab", "h", "l":
				m.adminOptIn = !m.adminOptIn
			case "enter":
				m.done = true
				return m, tea.Quit
			}
			return m, nil
		}
	}

	var cmd tea.Cmd
	if m.phase == hubPhaseForm {
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	}
	return m, cmd
}

func (m hubSetupModel) View() string {
	var content strings.Builder

	width := 100

	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(width)

	content.WriteString(bannerStyle.Render("GRAPEVINE HUB"))
	content.WriteString("\n\n")

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(width)

	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(width)

	formStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#BD93F9")).
		Padding(2, 4).
		Width(width - 10)

	var title, instruction, form string
	if m.phase == hubPhaseAdminAsk {
		title = "Hub Administration"
		instruction = "Optional: enable authenticated management endpoints"
		form = m.renderAdminAsk()
	} else {
		title = "First-Run Setup"
		instruction = "Configure your Grapevine discovery hub"
		form = renderHubFormFields(
			[]string{"Hub Name", "Bind Host", "Port", "Database Path"},
			[]string{
				"The name of your hub (shown to browsing clients)",
				"IP address to bind to (0.0.0.0 for all interfaces)",
				"Port number for the hub API (default: 7777)",
				"Path to the hub's SQLite database file",
			},
			m.inputs,
		)
	}

	content.WriteString(titleStyle.Render(title))
	content.WriteString("\n\n")
	content.WriteString(instructionStyle.Render(instruction))
	content.WriteString("\n\n")

	content.WriteString(lipgloss.PlaceHorizontal(
		width,
		lipgloss.Center,
		formStyle.Render(form),
	))
	content.WriteString("\n\n")

	if m.err != "" {
		errStyled := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#ff5555")).
			Bold(true).
			Align(lipgloss.Center).
			Width(width)

		content.WriteString(errStyled.Render("⚠ " + m.err))
		content.WriteString("\n\n")
	}

	hintStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(width)

	hint := "[Tab] Next Field · [Shift+Tab] Previous Field · [Enter] Confirm · [Esc] Cancel"
	if m.phase == hubPhaseAdminAsk {
		hint = "[Y] Yes · [N] No · [←/→] Toggle · [Enter] Confirm · [Esc] Cancel"
	}
	content.WriteString(hintStyled.Render(hint))

	termWidth := m.width
	termHeight := m.height
	if termWidth == 0 {
		termWidth = 120
	}
	if termHeight == 0 {
		termHeight = 30
	}

	return lipgloss.Place(
		termWidth,
		termHeight,
		lipgloss.Center,
		lipgloss.Center,
		content.String(),
	)
}

// renderHubFormFields renders a labeled column of text inputs in the wizard style.
func renderHubFormFields(labels, descriptions []string, inputs []textinput.Model) string {
	labelStyled := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#bd93f9")).
		Bold(true)

	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Italic(true)

	inputStyle := lipgloss.NewStyle().
		Padding(0, 1)

	var b strings.Builder
	for i, label := range labels {
		b.WriteString(labelStyled.Render(label))
		b.WriteString("\n")
		b.WriteString(descStyle.Render(descriptions[i]))
		b.WriteString("\n")
		b.WriteString(inputStyle.Render(inputs[i].View()))
		if i < len(labels)-1 {
			b.WriteString("\n\n")
		}
	}
	return b.String()
}

// renderAdminAsk renders the yes/no admin-token screen content.
func (m hubSetupModel) renderAdminAsk() string {
	questionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8f8f2")).
		Bold(true)

	bodyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4"))

	choice := lipgloss.NewStyle().Padding(0, 3)
	selected := choice.
		Foreground(lipgloss.Color("#282a36")).
		Background(lipgloss.Color("#BD93F9")).
		Bold(true)

	yes, no := choice.Render("Yes"), selected.Render("No")
	if m.adminOptIn {
		yes, no = selected.Render("Yes"), choice.Render("No")
	}

	var b strings.Builder
	b.WriteString(questionStyle.Render("Generate an admin token?"))
	b.WriteString("\n\n")
	b.WriteString(bodyStyle.Render("The admin token authorizes hub management via the REST API, such as\n" +
		"adding peer hubs to federate with (POST /v1/hubs). Without it, peers\n" +
		"can only be configured in grapevine-hub.toml.\n\n" +
		"The token is shown once after setup — save it somewhere safe."))
	b.WriteString("\n\n")
	b.WriteString(yes + "   " + no)
	return b.String()
}

// runSetupWizard runs the interactive TUI setup, saves the config, and returns it.
func runSetupWizard() (*hub.Config, error) {
	// Pre-populate from an existing config when re-running with --setup.
	existing, _ := hub.LoadConfig(configPath)

	m := newHubSetupModel(existing)
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return nil, fmt.Errorf("setup error: %w", err)
	}

	final := result.(hubSetupModel)
	if final.cancelled {
		fmt.Fprintln(os.Stderr, "Setup cancelled.")
		os.Exit(1)
	}
	if !final.done {
		os.Exit(0)
	}

	cfg := hub.DefaultConfig()
	if existing != nil {
		cfg = existing // keep heartbeat/federation/peer-hub settings not covered by the wizard
	}

	if name := strings.TrimSpace(final.inputs[hubFieldName].Value()); name != "" {
		cfg.HubName = name
	}
	if host := strings.TrimSpace(final.inputs[hubFieldHost].Value()); host != "" {
		cfg.Host = host
	}
	if port, err := strconv.Atoi(strings.TrimSpace(final.inputs[hubFieldPort].Value())); err == nil && port > 0 {
		cfg.Port = port
	}
	if db := strings.TrimSpace(final.inputs[hubFieldDB].Value()); db != "" {
		cfg.DatabasePath = db
	}

	var newToken string
	if final.adminOptIn && cfg.AdminToken == "" {
		token, err := hub.GenerateSecret()
		if err != nil {
			return nil, fmt.Errorf("generate admin token: %w", err)
		}
		cfg.AdminToken = token
		newToken = token
	} else if !final.adminOptIn {
		cfg.AdminToken = ""
	}

	if err := cfg.Save(configPath); err != nil {
		return nil, fmt.Errorf("save config: %w", err)
	}

	fmt.Printf("\nConfig written to %s\n", configPath)
	if newToken != "" {
		fmt.Printf("Admin token (save this — it won't be shown again):\n  %s\n", newToken)
	}
	fmt.Println()
	return cfg, nil
}
