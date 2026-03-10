package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

// ToSState tracks the Terms of Service acceptance UI state
type ToSState struct {
	viewport            viewport.Model
	renderedContent     string
	contentHeight       int
	hasScrolledToBottom bool
	skipClicked         bool
	focusedButton       int // 0=Skip to End, 1=Accept, 2=Decline
}

// findToSFile searches for the ToS file in multiple locations
func findToSFile(filename string) string {
	// Try these paths in order:
	// 1. ./legal/filename (running from root)
	// 2. ../legal/filename (running from build/)
	// 3. ../../legal/filename (running from nested folder)

	searchPaths := []string{
		filepath.Join("legal", filename),
		filepath.Join("..", "legal", filename),
		filepath.Join("..", "..", "legal", filename),
	}

	for _, path := range searchPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Default to first path (will error when trying to read)
	return searchPaths[0]
}

// initToSView initializes the ToS acceptance view
func (a *App) initToSView() error {
	viewportWidth := a.width - 20
	viewportHeight := a.height - 20 // Reserve space for banner, title, buttons, and hints

	if viewportHeight < 10 {
		viewportHeight = 10
	}

	vp := viewport.New(viewportWidth, viewportHeight)

	// Load and render markdown - try multiple locations
	tosPath := findToSFile("Client Terms.md")
	rendered, err := renderToSMarkdown(tosPath, viewportWidth)
	if err != nil {
		// Fallback: show error
		rendered = lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Red)).
			Render("Error loading Terms of Service:\n" + err.Error() +
				"\n\nPlease ensure legal/Client Terms.md exists.")
	}

	vp.SetContent(rendered)

	a.tosState = &ToSState{
		viewport:        vp,
		renderedContent: rendered,
		contentHeight:   lipgloss.Height(rendered),
		focusedButton:   0,
	}

	// Auto-enable if content fits in one screen
	if a.tosState.contentHeight <= vp.Height {
		a.tosState.hasScrolledToBottom = true
	}

	return nil
}

// renderToSMarkdown renders markdown with Glamour
func renderToSMarkdown(mdPath string, width int) (string, error) {
	mdContent, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("cannot read ToS file: %w", err)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		// Fallback: return raw markdown if glamour fails
		return string(mdContent), nil
	}

	rendered, err := r.Render(string(mdContent))
	if err != nil {
		// Fallback: return raw markdown
		return string(mdContent), nil
	}

	return rendered, nil
}

// handleToSKey handles key events in the ToS view
func (a *App) handleToSKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		a.tosState.viewport.LineUp(1)
		a.tosState.updateScrollState()
		return nil

	case "down", "j":
		a.tosState.viewport.LineDown(1)
		a.tosState.updateScrollState()
		return nil

	case "pgup":
		a.tosState.viewport.HalfViewUp()
		a.tosState.updateScrollState()
		return nil

	case "pgdown":
		a.tosState.viewport.HalfViewDown()
		a.tosState.updateScrollState()
		return nil

	case "home":
		a.tosState.viewport.GotoTop()
		return nil

	case "end":
		a.tosState.viewport.GotoBottom()
		a.tosState.updateScrollState()
		return nil

	case "tab", "right", "l":
		// Cycle button focus forward
		a.tosState.focusedButton = (a.tosState.focusedButton + 1) % 3
		return nil

	case "shift+tab", "left", "h":
		// Cycle button focus backward
		a.tosState.focusedButton = (a.tosState.focusedButton - 1 + 3) % 3
		return nil

	case "enter", " ":
		return a.handleToSButtonPress()

	case "ctrl+c", "ctrl+q":
		// Allow quit even during ToS
		return tea.Quit
	}

	return nil
}

// handleToSButtonPress handles button activation
func (a *App) handleToSButtonPress() tea.Cmd {
	switch a.tosState.focusedButton {
	case 0: // Skip to End
		a.tosState.viewport.GotoBottom()
		a.tosState.skipClicked = true
		a.tosState.hasScrolledToBottom = true
		// Auto-focus Accept button
		a.tosState.focusedButton = 1
		return nil

	case 1: // Accept
		if !a.tosState.canAcceptDecline() {
			return nil // Can't accept yet
		}
		return a.acceptToS()

	case 2: // Decline
		if !a.tosState.canAcceptDecline() {
			return nil // Can't decline yet
		}
		return a.declineToS()
	}

	return nil
}

// acceptToS saves acceptance and transitions to next view
func (a *App) acceptToS() tea.Cmd {
	return func() tea.Msg {
		// Load config
		config, err := a.configMgr.LoadAppConfig()
		if err != nil {
			// Create default config if load fails
			config = &AppConfig{
				Version: 1,
				UI:      UIConfig{Theme: "dracula"},
			}
		}

		// Set acceptance flag
		config.TermsAccepted = true

		// Save config
		if err := a.configMgr.SaveAppConfig(config); err != nil {
			a.statusMessage = "Failed to save config: " + err.Error()
			a.statusError = true
			return nil
		}

		// Transition to next view (identity setup)
		a.view = ViewIdentitySetup
		a.initIdentitySetupForm()

		return nil
	}
}

// declineToS exits the application
func (a *App) declineToS() tea.Cmd {
	a.statusMessage = "You must accept the Terms of Service to use Concord."
	a.statusError = true
	return tea.Quit
}

// updateScrollState checks if user has scrolled to bottom
func (s *ToSState) updateScrollState() {
	if s.viewport.AtBottom() {
		s.hasScrolledToBottom = true
	}
}

// canAcceptDecline returns true if Accept/Decline buttons should be enabled
func (s *ToSState) canAcceptDecline() bool {
	return s.hasScrolledToBottom || s.skipClicked
}

// renderToSView renders the full ToS acceptance screen
func (a *App) renderToSView() string {
	var content strings.Builder

	// Banner
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(a.width)
	content.WriteString(bannerStyle.Render(a.banner.Art))
	content.WriteString("\n\n")

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Align(lipgloss.Center).
		Width(a.width)
	content.WriteString(titleStyle.Render("Terms of Service"))
	content.WriteString("\n\n")

	// Viewport
	viewportStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Padding(1, 2).
		Width(a.width - 18).
		Align(lipgloss.Center)

	content.WriteString(lipgloss.PlaceHorizontal(
		a.width,
		lipgloss.Center,
		viewportStyle.Render(a.tosState.viewport.View()),
	))
	content.WriteString("\n")

	// Scroll hint
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Italic(true).
		Align(lipgloss.Center).
		Width(a.width)

	// Calculate scroll percentage
	scrollPercentage := 0
	if a.tosState.contentHeight > a.tosState.viewport.Height {
		scrollPercentage = int(float64(a.tosState.viewport.YOffset) / float64(a.tosState.contentHeight-a.tosState.viewport.Height) * 100)
		if scrollPercentage > 100 {
			scrollPercentage = 100
		}
	} else {
		scrollPercentage = 100
	}

	hint := fmt.Sprintf("[Scroll: ↑/↓/PgUp/PgDn · %d%%]", scrollPercentage)
	content.WriteString(hintStyle.Render(hint))
	content.WriteString("\n\n")

	// Buttons
	content.WriteString(lipgloss.PlaceHorizontal(
		a.width,
		lipgloss.Center,
		a.renderToSButtons(),
	))
	content.WriteString("\n\n")

	// Instruction text
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Align(lipgloss.Center).
		Width(a.width)

	if !a.tosState.canAcceptDecline() {
		content.WriteString(instructionStyle.Render(
			"Please scroll to the bottom to enable Accept/Decline buttons",
		))
	} else {
		content.WriteString(instructionStyle.Render(
			"[Tab] Switch Button · [Enter] Confirm",
		))
	}

	return lipgloss.PlaceVertical(
		a.height,
		lipgloss.Center,
		content.String(),
	)
}

// renderToSButtons renders the three action buttons
func (a *App) renderToSButtons() string {
	canInteract := a.tosState.canAcceptDecline()

	// No borders - just highlighted background
	focusedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	disabledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	buttons := []string{"Skip to End", "Accept", "Decline"}
	var renderedButtons []string
	buttonTextWidth := 11 // "Skip to End" length

	for i, label := range buttons {
		var style lipgloss.Style
		if i == 0 {
			// Skip to End always enabled
			if a.tosState.focusedButton == i {
				style = focusedStyle
			} else {
				style = normalStyle
			}
		} else {
			// Accept/Decline depend on scroll state
			if !canInteract {
				style = disabledStyle
			} else if a.tosState.focusedButton == i {
				style = focusedStyle
			} else {
				style = normalStyle
			}
		}
		// Pad text to consistent width before styling
		paddedLabel := lipgloss.PlaceHorizontal(buttonTextWidth, lipgloss.Center, label)
		renderedButtons = append(renderedButtons, style.Render(paddedLabel))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, renderedButtons...)
}
