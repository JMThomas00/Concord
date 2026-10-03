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

// findToSFile searches for the ToS file relative to the running binary's own
// location first (so it works no matter what the caller's cwd is — e.g. a
// PATH symlink launched from an arbitrary directory), then falls back to the
// old cwd-relative guesses for `go run`/dev use where the executable lives in
// a temp build dir unrelated to the repo.
func findToSFile(filename string) string {
	var searchPaths []string

	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		exeDir := filepath.Dir(exe)
		searchPaths = append(searchPaths,
			filepath.Join(exeDir, "legal", filename),
			filepath.Join(exeDir, "..", "legal", filename),
			filepath.Join(exeDir, "..", "..", "legal", filename),
		)
	}

	searchPaths = append(searchPaths,
		filepath.Join("legal", filename),
		filepath.Join("..", "legal", filename),
		filepath.Join("..", "..", "legal", filename),
	)

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

// tosViewportSize is the terms' reading area on the login stage.
func (a *App) tosViewportSize() (int, int) {
	// As tall as it can be while the grapes keep their 21 rows; on short
	// terminals the grapes give way first, then the banner.
	return stageWidth, max(5, min(12, a.height-33))
}

// resizeToS fits the terms to the window, keeping how far you've read.
func (a *App) resizeToS() {
	s := a.tosState
	if s == nil {
		return
	}
	w, h := a.tosViewportSize()
	if s.viewport.Width == w && s.viewport.Height == h {
		return
	}
	if s.viewport.Width != w {
		if rendered, err := renderToSMarkdown(findToSFile("Client Terms.md"), w); err == nil {
			s.renderedContent = rendered
			s.contentHeight = lipgloss.Height(rendered)
			s.viewport.SetContent(rendered)
		}
	}
	s.viewport.Width, s.viewport.Height = w, h
	if s.contentHeight <= h {
		s.hasScrolledToBottom = true
	}
	s.updateScrollState()
}

// renderToSView renders the terms on the login stage: the text, a
// reading-progress bar, and Accept, which lights up once you've reached
// the end.
func (a *App) renderToSView() string {
	a.resizeToS()
	s := a.tosState
	c := a.theme.Colors
	read := 1.0
	if s.contentHeight > s.viewport.Height {
		read = min(1, float64(s.viewport.YOffset)/float64(s.contentHeight-s.viewport.Height))
	}
	if s.canAcceptDecline() {
		read = 1
	}
	const barW = 40
	filled := int(read * barW)
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Render(strings.Repeat("━", filled)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Render(strings.Repeat("─", barW-filled))
	status := a.dim(fmt.Sprintf("  %d%% read", int(read*100)))
	if s.canAcceptDecline() {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green)).Render("  ✓ all read")
	}

	var b strings.Builder
	b.WriteString(s.viewport.View() + "\n\n")
	b.WriteString(bar + status + "\n\n")
	b.WriteString(a.renderToSButtons() + "\n")
	hints := []keyHint{{"↑↓ PgUp PgDn", "Read"}, {"Tab", "Switch"}, {"Enter", "Choose"}}
	return a.stagePage("Before we start", "The fine print", "fine print", b.String(), hints)
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
