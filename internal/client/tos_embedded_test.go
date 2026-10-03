package client

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Release downloads and installs have no legal folder beside the client:
// the terms still show, from the copy built in (they once showed an error
// on every new user's first screen).
func TestTermsShowWithoutTheLegalFolder(t *testing.T) {
	out, err := renderToSMarkdown(filepath.Join(t.TempDir(), "no", "Client Terms.md"), 100)
	if err != nil {
		t.Fatal(err)
	}
	if text := ansi.Strip(out); !strings.Contains(text, "Terms") || strings.Contains(text, "cannot read") || len(text) < 500 {
		t.Fatalf("terms not shown:\n%s", text)
	}
}
