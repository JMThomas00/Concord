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
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

// setupPhase tracks which screen of the wizard is active.
type setupPhase int

const (
	phaseServer        setupPhase = iota // main server settings form
	phaseGrapevineAsk                    // "list on Grapevine?" yes/no
	phaseGrapevineForm                   // Grapevine listing details form
	phaseHubAsk                          // "host a Grapevine hub?" 3-way choice
)

// hubChoice is the user's hub-hosting preference from phaseHubAsk.
type hubChoice int

const (
	hubChoiceMirror        hubChoice = 0 // run a failover mirror synced from the official hub
	hubChoiceCustom        hubChoice = 1 // run a standalone hub (no official peer pre-configured)
	hubChoiceNotInterested hubChoice = 2 // skip hub hosting entirely
)

// setupModel is a minimal bubbletea model for first-run server configuration.
type setupModel struct {
	inputs  []textinput.Model
	focused int
	done    bool
	err     string
	width   int
	height  int

	// Grapevine opt-in step
	phase     setupPhase
	gvOptIn   bool // highlighted choice on the ask screen
	gvInputs  []textinput.Model
	gvFocused int

	// Hub hosting step
	hubChoice hubChoice // default: hubChoiceNotInterested
}

const (
	fieldName = iota
	fieldHost
	fieldPort
	fieldDB
	fieldAdminEmail
	numFields
)

const (
	gvFieldHubURL = iota
	gvFieldDescription
	gvFieldCategory
	gvFieldTags
	gvFieldPublicHost
	gvFieldPublicPort
	numGvFields
)

const defaultHubURL = "http://grapevine.concord.chat"

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

	// Grapevine listing details
	var gv *server.GrapevineConfig
	if existingConfig != nil {
		gv = &existingConfig.Grapevine
	}

	gvInputs := make([]textinput.Model, numGvFields)

	gvInputs[gvFieldHubURL] = textinput.New()
	gvInputs[gvFieldHubURL].Placeholder = defaultHubURL
	gvInputs[gvFieldHubURL].SetValue(defaultHubURL)
	if gv != nil && gv.HubURL != "" {
		gvInputs[gvFieldHubURL].SetValue(gv.HubURL)
	}
	gvInputs[gvFieldHubURL].CharLimit = 255

	gvInputs[gvFieldDescription] = textinput.New()
	gvInputs[gvFieldDescription].Placeholder = "A place to chat."
	if gv != nil && gv.Description != "" {
		gvInputs[gvFieldDescription].SetValue(gv.Description)
	}
	gvInputs[gvFieldDescription].CharLimit = 255

	gvInputs[gvFieldCategory] = textinput.New()
	gvInputs[gvFieldCategory].Placeholder = "Gaming, Technology, Art, Tabletop, General…"
	if gv != nil && gv.Category != "" {
		gvInputs[gvFieldCategory].SetValue(gv.Category)
	}
	gvInputs[gvFieldCategory].CharLimit = 64

	gvInputs[gvFieldTags] = textinput.New()
	gvInputs[gvFieldTags].Placeholder = "friendly, english, 18+ (comma-separated)"
	if gv != nil && len(gv.Tags) > 0 {
		gvInputs[gvFieldTags].SetValue(strings.Join(gv.Tags, ", "))
	}
	gvInputs[gvFieldTags].CharLimit = 255

	gvInputs[gvFieldPublicHost] = textinput.New()
	gvInputs[gvFieldPublicHost].Placeholder = "myserver.example.com (blank = bind host)"
	if gv != nil && gv.PublicHost != "" {
		gvInputs[gvFieldPublicHost].SetValue(gv.PublicHost)
	}
	gvInputs[gvFieldPublicHost].CharLimit = 255

	gvInputs[gvFieldPublicPort] = textinput.New()
	gvInputs[gvFieldPublicPort].Placeholder = "blank = server port"
	if gv != nil && gv.PublicPort != 0 {
		gvInputs[gvFieldPublicPort].SetValue(fmt.Sprintf("%d", gv.PublicPort))
	}
	gvInputs[gvFieldPublicPort].CharLimit = 5

	return setupModel{
		inputs:    inputs,
		gvInputs:  gvInputs,
		gvOptIn:   gv != nil && gv.Enabled,
		hubChoice: hubChoiceNotInterested,
	}
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
		if msg.String() == "ctrl+c" || msg.String() == "esc" {
			fmt.Fprintln(os.Stderr, "Setup cancelled.")
			os.Exit(1)
		}

		switch m.phase {
		case phaseServer:
			switch msg.String() {
			case "tab", "down", "enter":
				if msg.String() == "enter" && m.focused == numFields-1 {
					// Validate port
					port := strings.TrimSpace(m.inputs[fieldPort].Value())
					if _, err := strconv.Atoi(port); err != nil {
						m.err = "Port must be a number."
						return m, nil
					}
					m.err = ""
					m.inputs[m.focused].Blur()
					m.phase = phaseGrapevineAsk
					return m, nil
				}
				m.inputs[m.focused].Blur()
				m.focused = (m.focused + 1) % numFields
				m.inputs[m.focused].Focus()

			case "shift+tab", "up":
				m.inputs[m.focused].Blur()
				m.focused = (m.focused - 1 + numFields) % numFields
				m.inputs[m.focused].Focus()
			}

		case phaseGrapevineAsk:
			switch msg.String() {
			case "y", "Y":
				m.gvOptIn = true
				m.phase = phaseGrapevineForm
				m.gvInputs[m.gvFocused].Focus()
				return m, textinput.Blink
			case "n", "N":
				m.gvOptIn = false
				m.phase = phaseHubAsk
				return m, nil
			case "left", "right", "tab", "h", "l":
				m.gvOptIn = !m.gvOptIn
			case "enter":
				if m.gvOptIn {
					m.phase = phaseGrapevineForm
					m.gvInputs[m.gvFocused].Focus()
					return m, textinput.Blink
				}
				m.phase = phaseHubAsk
				return m, nil
			}
			return m, nil

		case phaseGrapevineForm:
			switch msg.String() {
			case "tab", "down", "enter":
				if msg.String() == "enter" && m.gvFocused == numGvFields-1 {
					// Validate public port (optional)
					port := strings.TrimSpace(m.gvInputs[gvFieldPublicPort].Value())
					if port != "" {
						if _, err := strconv.Atoi(port); err != nil {
							m.err = "Public port must be a number (or blank)."
							return m, nil
						}
					}
					if strings.TrimSpace(m.gvInputs[gvFieldHubURL].Value()) == "" {
						m.err = "Hub URL is required."
						return m, nil
					}
					m.err = ""
					m.phase = phaseHubAsk
					return m, nil
				}
				m.gvInputs[m.gvFocused].Blur()
				m.gvFocused = (m.gvFocused + 1) % numGvFields
				m.gvInputs[m.gvFocused].Focus()

			case "shift+tab", "up":
				m.gvInputs[m.gvFocused].Blur()
				m.gvFocused = (m.gvFocused - 1 + numGvFields) % numGvFields
				m.gvInputs[m.gvFocused].Focus()
			}

		case phaseHubAsk:
			switch msg.String() {
			case "left", "shift+tab":
				if m.hubChoice > hubChoiceMirror {
					m.hubChoice--
				}
			case "right", "tab":
				if m.hubChoice < hubChoiceNotInterested {
					m.hubChoice++
				}
			case "1":
				m.hubChoice = hubChoiceMirror
			case "2":
				m.hubChoice = hubChoiceCustom
			case "3":
				m.hubChoice = hubChoiceNotInterested
			case "enter":
				m.done = true
				return m, tea.Quit
			}
			return m, nil
		}
	}

	// Forward key events to the focused input of the active form
	var cmd tea.Cmd
	switch m.phase {
	case phaseServer:
		m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	case phaseGrapevineForm:
		m.gvInputs[m.gvFocused], cmd = m.gvInputs[m.gvFocused].Update(msg)
	}
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

	// Instructions
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(width)

	// Form container
	formStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#BD93F9")).
		Padding(2, 4).
		Width(width - 10)

	var title, instruction, form string
	switch m.phase {
	case phaseGrapevineAsk:
		title = "Grapevine Discovery"
		instruction = "Optional: list your server publicly so others can find it"
		form = m.renderGrapevineAsk()

	case phaseGrapevineForm:
		title = "Grapevine Listing"
		instruction = "How your server appears in the public directory"
		form = renderFormFields(
			[]string{"Hub URL", "Description", "Category", "Tags", "Public Host", "Public Port"},
			[]string{
				"The discovery hub to register with (default: official Grapevine hub)",
				"A short description shown in the server browser",
				"e.g. Gaming, Technology, Art, Tabletop, General",
				"Comma-separated keywords for search",
				"Your externally reachable address (needed if bind host is 0.0.0.0 or private)",
				"Externally reachable port (blank to use the server port)",
			},
			m.gvInputs,
		)

	case phaseHubAsk:
		title = "Host a Grapevine Hub"
		instruction = "Optional: run a discovery hub alongside your server to help the network"
		form = m.renderHubAsk()

	default:
		title = "First-Run Setup"
		instruction = "Configure your Concord server"
		form = renderFormFields(
			[]string{"Server Name", "Bind Host", "Port", "Database Path", "Admin Email"},
			[]string{
				"The name of your server (visible to users)",
				"IP address to bind to (0.0.0.0 for all interfaces)",
				"Port number for the server (default: 8080)",
				"Path to SQLite database file",
				"Admin email for recovery (optional)",
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

	hint := "[Tab] Next Field · [Shift+Tab] Previous Field · [Enter] Confirm · [Esc] Cancel"
	if m.phase == phaseGrapevineAsk {
		hint = "[Y] Yes · [N] No · [←/→] Toggle · [Enter] Confirm · [Esc] Cancel"
	} else if m.phase == phaseHubAsk {
		hint = "[1] Mirror  [2] Custom  [3] Skip · [←/→] Navigate · [Enter] Confirm · [Esc] Cancel"
	}
	content.WriteString(hintStyled.Render(hint))

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

// renderFormFields renders a labeled column of text inputs in the wizard style.
func renderFormFields(labels, descriptions []string, inputs []textinput.Model) string {
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

// renderGrapevineAsk renders the yes/no opt-in screen content.
func (m setupModel) renderGrapevineAsk() string {
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
	if m.gvOptIn {
		yes, no = selected.Render("Yes"), choice.Render("No")
	}

	var b strings.Builder
	b.WriteString(questionStyle.Render("List your server on Grapevine?"))
	b.WriteString("\n\n")
	b.WriteString(bodyStyle.Render("Grapevine is Concord's public server directory. Opting in registers your\n" +
		"server with a discovery hub so people can find and join it from the\n" +
		"in-app Hub Browser. Your address is never shown in the public listing.\n\n" +
		"You can change this later with --reconfigure or in concord-server.toml."))
	b.WriteString("\n\n")
	b.WriteString(yes + "   " + no)
	return b.String()
}

// renderHubAsk renders the 3-way hub-hosting choice screen.
func (m setupModel) renderHubAsk() string {
	questionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8f8f2")).
		Bold(true)

	bodyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4"))

	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#ffb86c"))

	pill := lipgloss.NewStyle().Padding(0, 2).MarginRight(1)
	selectedPill := pill.
		Foreground(lipgloss.Color("#282a36")).
		Background(lipgloss.Color("#BD93F9")).
		Bold(true)

	labels := []string{"[1] Mirror Hub", "[2] Custom Hub", "[3] Not Interested"}
	var rendered []string
	for i, label := range labels {
		if hubChoice(i) == m.hubChoice {
			rendered = append(rendered, selectedPill.Render(label))
		} else {
			rendered = append(rendered, pill.Render(label))
		}
	}

	var b strings.Builder
	b.WriteString(questionStyle.Render("Host a Grapevine discovery hub?"))
	b.WriteString("\n\n")
	b.WriteString(bodyStyle.Render(
		"A Grapevine hub lets Concord clients discover servers. Hosting one\n"+
			"alongside your server helps the network stay resilient.\n\n"+
			"Mirror Hub      — syncs the official Grapevine listing. Acts as a\n"+
			"                  failover for clients who add your hub URL. Requires\n"+
			"                  a public IP or Cloudflare Tunnel (see Obsidian note).\n\n"+
			"Custom Hub      — standalone hub; no official sync by default.\n\n"+
			"Not Interested  — skip. You can run concord-hub separately any time.",
	))
	b.WriteString("\n\n")

	if m.hubChoice == hubChoiceMirror {
		b.WriteString(warnStyle.Render(
			"! Your hub URL will be publicly visible — your server's IP address\n"+
				"  or domain name will be discoverable. On a home server, use a\n"+
				"  Cloudflare Tunnel to hide your real IP (see CLOUDFLARE TUNNEL SETUP.md)."))
		b.WriteString("\n\n")
	}

	b.WriteString(strings.Join(rendered, ""))
	return b.String()
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

	// Preserve settings the wizard doesn't cover when reconfiguring.
	if existingConfig != nil {
		cfg.MaxConnections = existingConfig.MaxConnections
		cfg.Debug = existingConfig.Debug
		cfg.MessagePruning = existingConfig.MessagePruning
		cfg.Grapevine = existingConfig.Grapevine // keeps server_id + registration_secret
	}

	cfg.Grapevine.Enabled = final.gvOptIn
	if final.gvOptIn {
		hubURL := strings.TrimSpace(final.gvInputs[gvFieldHubURL].Value())
		if hubURL == "" {
			hubURL = defaultHubURL
		}
		// Registration credentials belong to a specific hub — switching hubs
		// invalidates them (the heartbeat 404 recovery would handle it, but a
		// clean re-register is faster and clearer).
		if cfg.Grapevine.HubURL != "" && cfg.Grapevine.HubURL != hubURL {
			cfg.Grapevine.ServerID = ""
			cfg.Grapevine.RegistrationSecret = ""
		}
		cfg.Grapevine.HubURL = hubURL
		cfg.Grapevine.Description = strings.TrimSpace(final.gvInputs[gvFieldDescription].Value())
		cfg.Grapevine.Category = strings.TrimSpace(final.gvInputs[gvFieldCategory].Value())
		cfg.Grapevine.Tags = parseTags(final.gvInputs[gvFieldTags].Value())
		cfg.Grapevine.PublicHost = strings.TrimSpace(final.gvInputs[gvFieldPublicHost].Value())
		cfg.Grapevine.PublicPort, _ = strconv.Atoi(strings.TrimSpace(final.gvInputs[gvFieldPublicPort].Value()))
		if cfg.Grapevine.HeartbeatInterval == 0 {
			cfg.Grapevine.HeartbeatInterval = 30
		}
		if cfg.Grapevine.MaxMembers == 0 {
			cfg.Grapevine.MaxMembers = 1000
		}
	}

	// Write config file
	data, err := toml.Marshal(cfg)
	if err == nil {
		_ = os.WriteFile(configFilename, data, 0644)
		fmt.Printf("\nConfig written to %s\n", configFilename)
	}

	fmt.Printf("Share this address: %s:%d\n", cfg.Host, cfg.Port)
	if cfg.Grapevine.Enabled {
		fmt.Printf("Grapevine: your server will register with %s on startup.\n", cfg.Grapevine.HubURL)
	} else {
		fmt.Println("Grapevine listing skipped — opt in later with --reconfigure.")
	}

	// Write grapevine-hub.toml if the user opted into hosting a hub.
	if final.hubChoice != hubChoiceNotInterested {
		writeHubConfig(final.hubChoice, serverName)
	}

	fmt.Println()
	return cfg
}

const hubConfigFilename = "grapevine-hub.toml"

// writeHubConfig writes grapevine-hub.toml based on the chosen hub mode.
func writeHubConfig(choice hubChoice, serverName string) {
	hubCfg := hub.DefaultConfig()
	hubCfg.HubName = serverName + " Hub"

	if choice == hubChoiceMirror {
		hubCfg.PeerHubs = []hub.PeerHubConfig{
			{Name: "Official Grapevine Hub", URL: defaultHubURL},
		}
	}

	if err := hubCfg.Save(hubConfigFilename); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: could not write hub config: %v\n", err)
		return
	}

	fmt.Printf("\nHub config written to %s\n", hubConfigFilename)
	fmt.Printf("Start your hub:  ./concord-hub\n")

	if choice == hubChoiceMirror {
		fmt.Println()
		fmt.Println("MIRROR HUB — IP ADDRESS NOTICE")
		fmt.Println("-------------------------------")
		fmt.Println("Your hub URL will be publicly listed so clients can discover it")
		fmt.Println("as a fallback when the official Grapevine hub is unreachable.")
		fmt.Println("This means your server's IP address or domain will be visible.")
		fmt.Println()
		fmt.Println("On a home server or LXC container, use a Cloudflare Tunnel to")
		fmt.Println("hide your real IP address. See: CONCORD - CLOUDFLARE TUNNEL SETUP.md")
		fmt.Println()
		fmt.Printf("Federation sync: every %d minutes from %s\n", hubCfg.FederationSync, defaultHubURL)
		fmt.Println("Share your hub URL with users, or contact the official hub operator")
		fmt.Println("to register it as a discoverable peer in the network.")
	}
}

// parseTags splits a comma-separated tag string into trimmed, non-empty tags.
func parseTags(s string) []string {
	var tags []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	return tags
}
