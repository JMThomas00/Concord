package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestFxGridRoundTrip(t *testing.T) {
	in := "\x1b[1;35mhi\x1b[0m there\n🍇 grape\n\x1b]8337;3\x07x"
	g := parseFrame(in, 12, 3)
	out := g.String()
	if ansi.Strip(out) != "hi there    \n🍇 grape    \nx           " {
		t.Fatalf("plain text %q", ansi.Strip(out))
	}
	if !strings.Contains(out, "\x1b[1;35mhi") {
		t.Fatalf("styling lost: %q", out)
	}
	if g.rows[1][0].w != 2 || g.rows[1][1].w != 0 {
		t.Fatal("the emoji isn't one wide cell")
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w != 12 {
			t.Fatalf("line width %d", w)
		}
	}
}

// A background goes only where the page shows nothing, clear of its text.
func TestUnderlayKeepsClearOfText(t *testing.T) {
	page := parseFrame("\n      form      \n", 16, 3)
	bg := newGrid(16, 3)
	for r := 0; r < 3; r++ {
		for c := 0; c < 16; c++ {
			bg.set(r, c, "·", "", 1)
		}
	}
	page.underlay(bg, 2, 0)
	got := ansi.Strip(page.String())
	want := "················\n····  form  ····\n················"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSetKeepsWideCharactersWhole(t *testing.T) {
	g := newGrid(4, 1)
	g.set(0, 0, "🍇", "", 2)
	g.set(0, 1, "x", "", 1) // overwrites the emoji's second half
	if got := ansi.Strip(g.String()); got != " x  " {
		t.Fatalf("got %q", got)
	}
}
