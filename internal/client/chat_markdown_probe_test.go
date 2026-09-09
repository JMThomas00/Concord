package client

import (
	"fmt"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/concord-chat/concord/internal/themes"
)

// TestProbeChatGlamourPipeline is the Phase 5 empirical design test the
// plan calls for: render a bare URL, an @mention, and a literal
// "[selected text]" (mimicking insertCursorIntoMessage's bracket-wrapping)
// through the actual configured chat-glamour renderer, and inspect the
// real output. This determines the pipeline sequencing decision -- does
// glamour auto-style bare URLs (via goldmark's GFM/Linkify extension), and
// does a literal "[...]" survive as plain text or get mangled -- before any
// pipeline code gets written. Not a pass/fail assertion; it's a discovery
// tool, kept as a test so `go test -v` reproduces the finding on demand.
func TestProbeChatGlamourPipeline(t *testing.T) {
	dracula, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(buildThemedGlamourStyle(dracula)),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		t.Fatalf("NewTermRenderer: %v", err)
	}

	samples := []string{
		"check this out https://example.com/path?x=1 nice",
		"hey @someone are you there",
		"here is a [selected text] fragment",
		"**bold** and _italic_ and `code`",
	}

	for _, s := range samples {
		out, err := r.Render(s)
		if err != nil {
			t.Fatalf("Render(%q): %v", s, err)
		}
		t.Logf("input:  %q", s)
		t.Logf("output: %q", out)
		fmt.Println("----")
		fmt.Printf("input:  %q\n", s)
		fmt.Printf("output: %q\n", out)
	}
}
