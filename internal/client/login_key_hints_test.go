package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func newLoginTestApp(t *testing.T, width, height int, withIdentity bool) *App {
	t.Helper()
	a := newLayoutTestApp(t, width, height)
	a.loginEmail = textinput.New()
	a.loginPassword = textinput.New()
	if withIdentity {
		a.localIdentity = &LocalIdentity{Alias: "gh0st", Email: "gh0st@example.invalid"}
	}
	return a
}

func linesContaining(view string, needles ...string) []int {
	var rows []int
	for i, l := range strings.Split(ansi.Strip(view), "\n") {
		for _, n := range needles {
			if strings.Contains(l, n) {
				rows = append(rows, i)
				break
			}
		}
	}
	return rows
}

// The login shortcut hints used to render inside the 50-column form, so
// they wrapped onto two faint rows. They now sit below the form on one row.
func TestLoginKeyHintsStayOnOneRow(t *testing.T) {
	for _, withIdentity := range []bool{true, false} {
		a := newLoginTestApp(t, 180, 45, withIdentity)
		view := a.renderLoginView()
		rows := linesContaining(view, "Ctrl+S", "Ctrl+Q")
		if len(rows) != 1 || !strings.Contains(strings.Split(ansi.Strip(view), "\n")[rows[0]], "Ctrl+Q") {
			t.Errorf("identity=%v: expected every hint on one row, found hint rows %v in:\n%s", withIdentity, rows, ansi.Strip(view))
		}
	}
}

// On a terminal too narrow for one row, hints wrap whole (a key never
// separated from its description) and no row exceeds the screen.
func TestLoginKeyHintsWrapWholeOnNarrowTerminals(t *testing.T) {
	a := newLoginTestApp(t, 70, 45, false)
	hints := a.renderKeyHints([]keyHint{{"Tab", "Switch fields"}, {"Enter", "Login/Register"}, {"Ctrl+G", "Discover servers"}, {"Ctrl+S", "Settings"}, {"Ctrl+T", "Themes"}, {"Ctrl+Q", "Quit"}}, a.width-4)
	lines := strings.Split(ansi.Strip(hints), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected wrapping at width %d, got one row: %q", a.width, lines[0])
	}
	for _, l := range lines {
		if lipgloss.Width(l) > a.width-4 {
			t.Errorf("hint row wider than %d: %q", a.width-4, l)
		}
	}
	joined := strings.Join(lines, "\n")
	for _, pair := range []string{"Enter  Login/Register", "Ctrl+G  Discover servers", "Ctrl+Q  Quit"} {
		if !strings.Contains(joined, pair) {
			t.Errorf("key and description split across rows or missing: %q not in\n%s", pair, joined)
		}
	}
}
