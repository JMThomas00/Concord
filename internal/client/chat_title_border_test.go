package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	zone "github.com/lrstanley/bubblezone"
)

// The channel name lives in the chat box's top border ("╭─ # general ──╮"),
// and that border sits on the same row as the other panels' top borders --
// previously a separate header bar plus a spacer row pushed it two rows down.
func TestChatTitleSitsInTopBorderAlignedWithOtherPanels(t *testing.T) {
	a := newLayoutTestApp(t, 160, 40)
	a.currentChannel = &models.Channel{Name: "general", Topic: "say hi"}

	lines := strings.Split(ansi.Strip(zone.Scan(a.renderMainView())), "\n")

	titleRow, firstBorderRow := -1, -1
	for i, l := range lines {
		if firstBorderRow < 0 && strings.Contains(l, "╭") {
			firstBorderRow = i
		}
		if titleRow < 0 && strings.Contains(l, "╭─ # general — say hi ") {
			titleRow = i
		}
	}
	if titleRow < 0 {
		t.Fatalf("chat title not found in a top border; first rows:\n%s", strings.Join(lines[:4], "\n"))
	}
	if titleRow != firstBorderRow {
		t.Errorf("chat box top border is on row %d, other panels' top borders on row %d; first rows:\n%s",
			titleRow, firstBorderRow, strings.Join(lines[:titleRow+1], "\n"))
	}
	if strings.Count(lines[titleRow], "╭") < 3 {
		t.Errorf("expected the chat title row to also carry the neighbouring panels' top borders, got:\n%s", lines[titleRow])
	}
}

// Every panel names itself in its own top border, all on the same row, and
// the old in-panel CHANNELS/MEMBERS heading lines are gone (no duplicates).
func TestPanelsLabelTheirTopBorders(t *testing.T) {
	a := newLayoutTestApp(t, 170, 30) // wide enough that no panel collapses
	a.currentChannel = &models.Channel{Name: "general"}
	view := ansi.Strip(zone.Scan(a.renderMainView()))
	lines := strings.Split(view, "\n")

	row := -1
	for i, l := range lines {
		if strings.Contains(l, "╭─ SERVERS ") {
			row = i
			break
		}
	}
	if row < 0 {
		t.Fatalf("SERVERS label not found in a top border:\n%s", strings.Join(lines[:4], "\n"))
	}
	for _, label := range []string{"╭─ CHANNELS ", "╭─ # general ", "╭─ MEMBERS "} {
		if !strings.Contains(lines[row], label) {
			t.Errorf("expected %q on the same top-border row as SERVERS, got:\n%s", label, lines[row])
		}
	}
	for _, word := range []string{"CHANNELS", "MEMBERS", "SERVERS"} {
		if n := strings.Count(view, word); n != 1 {
			t.Errorf("%q appears %d times; want only the border label", word, n)
		}
	}
}

// Collapsed columns are too narrow for a whole label: plain border, never a
// clipped fragment like "SE…".
func TestNarrowPanelsKeepAPlainBorder(t *testing.T) {
	a := newLayoutTestApp(t, 170, 30)
	box := lipgloss.NewStyle().Width(8).Border(lipgloss.RoundedBorder()).Render("x")
	if got := a.labelPanelBorder(box, "MEMBERS", false); got != box {
		t.Errorf("expected the narrow box unchanged, got top %q", strings.Split(ansi.Strip(got), "\n")[0])
	}
}

func TestEmbedBorderTitleKeepsBoxWidth(t *testing.T) {
	box := lipgloss.NewStyle().Width(18).Border(lipgloss.RoundedBorder()).Render("hi")
	want := lipgloss.Width(strings.Split(box, "\n")[0])

	for _, title := range []string{"# general", "# a-very-long-channel-name-that-cannot-fit"} {
		top := strings.Split(embedBorderTitle(box, title, lipgloss.NewStyle()), "\n")[0]
		plain := ansi.Strip(top)
		if got := lipgloss.Width(top); got != want {
			t.Errorf("title %q: top border width %d, want %d (%q)", title, got, want, plain)
		}
		if !strings.HasPrefix(plain, "╭─ ") || !strings.HasSuffix(plain, "╮") {
			t.Errorf("title %q: malformed top border %q", title, plain)
		}
		if len(title) > want && !strings.Contains(plain, "…") {
			t.Errorf("title %q: expected truncation with an ellipsis, got %q", title, plain)
		}
	}
}
