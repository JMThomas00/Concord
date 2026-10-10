package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// TestPaneFrameCentresAsABlock: a frame whose lines differ in length keeps
// its own left edge when centred in a wider pane (it used to zig-zag,
// because lipgloss centres each line on its own).
func TestPaneFrameCentresAsABlock(t *testing.T) {
	frame := "\x1b[31mRACE\x1b[0m\nPRACTICE        x\nGARAGE\n"
	out := lipgloss.NewStyle().Width(40).Height(5).Align(lipgloss.Center, lipgloss.Center).Render(squareFrame(frame))
	edge := -1
	for _, l := range strings.Split(ansi.Strip(out), "\n") {
		if strings.TrimSpace(l) == "" {
			continue
		}
		at := len(l) - len(strings.TrimLeft(l, " "))
		if edge == -1 {
			edge = at
		}
		if at != edge {
			t.Fatalf("lines start at %d and %d:\n%s", edge, at, ansi.Strip(out))
		}
	}
	if edge <= 0 {
		t.Fatal("the frame should still be centred")
	}
}
