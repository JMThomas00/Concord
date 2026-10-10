package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
	"github.com/concord-chat/concord/internal/themes"
)

// TestThemedGlamourStyleHeadingsHaveNoLiteralHashMarks is a regression test
// for a real, live-reported UX defect (2026-09-11): buildThemedGlamourStyle
// set H1-H6's Prefix to "# "/"## "/etc, matching glamour's own bundled
// "dark" style convention of keeping literal markdown hash marks as a
// heading-level indicator in the rendered output. On a genuinely custom,
// theme-derived style (unlike glamour's stock palettes) there's no reason
// to keep that -- it read as unrendered markdown syntax rather than an
// actual header. Renders isolated H2/H3 headings (the only two levels the
// real Help & Guide doc uses) directly, rather than the full help doc,
// since the doc's fenced ```bash code blocks legitimately contain lines
// like "# Windows (MSYS2 MinGW)" as real shell comments -- those must NOT
// be mistaken for a heading-prefix regression.
func TestThemedGlamourStyleHeadingsHaveNoLiteralHashMarks(t *testing.T) {
	theme, err := themes.GetTheme("dracula")
	if err != nil {
		t.Fatalf("GetTheme(dracula): %v", err)
	}

	r, err := glamour.NewTermRenderer(
		glamour.WithStyles(buildThemedGlamourStyle(theme)),
		glamour.WithWordWrap(80),
	)
	if err != nil {
		t.Fatalf("NewTermRenderer: %v", err)
	}

	rendered, err := r.Render("## Section Heading\n\nSome text.\n\n### Subsection Heading\n\nMore text.\n")
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	if strings.Contains(rendered, "##") {
		t.Errorf("expected no literal '##' in rendered heading output, got:\n%s", rendered)
	}

	// Strip ANSI SGR codes (sgrSeq, defined in help_markdown_ansi_test.go)
	// before checking the heading text survived -- glamour styles "Section"
	// and " Heading" as separate colored runs with an SGR sequence between
	// them, so the raw string doesn't contain "Section Heading" contiguously
	// even though it renders as one visual phrase.
	plain := sgrSeq.ReplaceAllString(rendered, "")
	if !strings.Contains(plain, "Section Heading") || !strings.Contains(plain, "Subsection Heading") {
		t.Errorf("expected the heading text itself to still render, got plain text:\n%s", plain)
	}
}
