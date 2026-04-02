package main

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

// tosModel is a bubbletea model for ToS acceptance
type tosModel struct {
	viewport            viewport.Model
	renderedContent     string
	contentHeight       int
	hasScrolledToBottom bool
	skipClicked         bool
	focusedButton       int // 0=Skip to End, 1=Accept, 2=Decline
	accepted            bool
	done                bool
	width               int
	height              int
}

func newToSModel() (tosModel, error) {
	// Get terminal size (default 120x30)
	width := 120
	height := 30

	vp := viewport.New(width-20, 15)

	// Load and render markdown - try multiple locations
	tosPath := findToSFile("Server Terms.md")
	rendered, err := renderServerToS(tosPath, vp.Width)
	if err != nil {
		// Show error but continue
		rendered = "Error loading Terms of Service:\n" + err.Error()
	}

	vp.SetContent(rendered)

	m := tosModel{
		viewport:        vp,
		renderedContent: rendered,
		contentHeight:   lipgloss.Height(rendered),
		focusedButton:   0,
		width:           width,
		height:          height,
	}

	// Auto-enable if content fits in one screen
	if m.contentHeight <= vp.Height {
		m.hasScrolledToBottom = true
	}

	return m, nil
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

func renderServerToS(mdPath string, width int) (string, error) {
	mdContent, err := os.ReadFile(mdPath)
	if err != nil {
		return "", fmt.Errorf("cannot read ToS file: %w", err)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithAutoStyle(),
		glamour.WithWordWrap(width-4),
	)
	if err != nil {
		// Fallback: return raw markdown
		return string(mdContent), nil
	}

	rendered, err := r.Render(string(mdContent))
	if err != nil {
		// Fallback: return raw markdown
		return string(mdContent), nil
	}

	return rendered, nil
}

func (m tosModel) Init() tea.Cmd {
	return nil
}

func (m tosModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 20
		m.viewport.Height = msg.Height - 15
		m.viewport.YPosition = 0 // Reset viewport position after resize

	case tea.MouseMsg:
		// Let viewport handle mouse events (scrolling)
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		m.updateScrollState()
		return m, cmd

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "esc":
			// Decline on Ctrl+C/Esc
			m.accepted = false
			m.done = true
			return m, tea.Quit

		case "up", "k":
			m.viewport.LineUp(1)
			m.updateScrollState()

		case "down", "j":
			m.viewport.LineDown(1)
			m.updateScrollState()

		case "pgup":
			m.viewport.HalfViewUp()
			m.updateScrollState()

		case "pgdown":
			m.viewport.HalfViewDown()
			m.updateScrollState()

		case "home":
			m.viewport.GotoTop()

		case "end":
			m.viewport.GotoBottom()
			m.updateScrollState()

		case "tab", "right", "l":
			m.focusedButton = (m.focusedButton + 1) % 3

		case "shift+tab", "left", "h":
			m.focusedButton = (m.focusedButton - 1 + 3) % 3

		case "enter", " ":
			return m, m.handleButtonPress()
		}
	}

	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m *tosModel) updateScrollState() {
	if m.viewport.AtBottom() {
		m.hasScrolledToBottom = true
	}
}

func (m *tosModel) canAcceptDecline() bool {
	return m.hasScrolledToBottom || m.skipClicked
}

func (m *tosModel) handleButtonPress() tea.Cmd {
	switch m.focusedButton {
	case 0: // Skip to End
		m.viewport.GotoBottom()
		m.skipClicked = true
		m.hasScrolledToBottom = true
		m.focusedButton = 1 // Auto-focus Accept
		return nil

	case 1: // Accept
		if !m.canAcceptDecline() {
			return nil
		}
		m.accepted = true
		m.done = true
		return tea.Quit

	case 2: // Decline
		if !m.canAcceptDecline() {
			return nil
		}
		m.accepted = false
		m.done = true
		return tea.Quit
	}

	return nil
}

func (m tosModel) View() string {
	var content strings.Builder

	// Banner
	bannerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(m.width)

	content.WriteString(bannerStyle.Render("CONCORD SERVER"))
	content.WriteString("\n\n")

	// Title
	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")).
		Bold(true).
		Align(lipgloss.Center).
		Width(m.width)
	content.WriteString(titleStyle.Render("Terms of Service"))
	content.WriteString("\n\n")

	// Viewport
	viewportStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#BD93F9")).
		Padding(1, 2)

	content.WriteString(lipgloss.PlaceHorizontal(
		m.width,
		lipgloss.Center,
		viewportStyle.Render(m.viewport.View()),
	))
	content.WriteString("\n")

	// Scroll indicator
	hintStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Italic(true).
		Align(lipgloss.Center).
		Width(m.width)

	// Calculate scroll percentage
	scrollPercentage := 0
	if m.contentHeight > m.viewport.Height {
		scrollPercentage = int(float64(m.viewport.YOffset) / float64(m.contentHeight-m.viewport.Height) * 100)
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
		m.width,
		lipgloss.Center,
		m.renderButtons(),
	))
	content.WriteString("\n\n")

	// Instruction
	instructionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Align(lipgloss.Center).
		Width(m.width)

	if !m.canAcceptDecline() {
		content.WriteString(instructionStyle.Render(
			"Please scroll to the bottom to enable Accept/Decline",
		))
	} else {
		content.WriteString(instructionStyle.Render(
			"[Tab] Switch Button · [Enter] Confirm · [Esc] Cancel",
		))
	}

	return content.String()
}

func (m tosModel) renderButtons() string {
	canInteract := m.canAcceptDecline()

	// No borders - just highlighted background
	focusedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#282a36")).
		Background(lipgloss.Color("#BD93F9")).
		Bold(true).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	normalStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#f8f8f2")).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	disabledStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6272a4")).
		Padding(0, 2).
		MarginLeft(2).
		MarginRight(2)

	buttons := []string{"Skip to End", "Accept", "Decline"}
	var rendered []string
	buttonTextWidth := 11 // "Skip to End" length

	for i, label := range buttons {
		var style lipgloss.Style
		if i == 0 {
			if m.focusedButton == i {
				style = focusedStyle
			} else {
				style = normalStyle
			}
		} else {
			if !canInteract {
				style = disabledStyle
			} else if m.focusedButton == i {
				style = focusedStyle
			} else {
				style = normalStyle
			}
		}
		// Pad text to consistent width before styling
		paddedLabel := lipgloss.PlaceHorizontal(buttonTextWidth, lipgloss.Center, label)
		rendered = append(rendered, style.Render(paddedLabel))
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, rendered...)
}

// runToSSetup runs the ToS acceptance TUI and returns true if accepted
func runToSSetup() bool {
	m, err := newToSModel()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing ToS view: %v\n", err)
		return false
	}

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	result, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "ToS setup error: %v\n", err)
		return false
	}

	final := result.(tosModel)
	return final.done && final.accepted
}
