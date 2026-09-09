package client

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/muesli/termenv"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
	zone "github.com/lrstanley/bubblezone"
)

// stripSGR removes only ANSI SGR ("set graphics rendition", the \x1b[...m
// color/bold/underline codes) sequences, leaving OSC 8 hyperlink escapes
// (\x1b]8;;url\x1b\\...) untouched. lipgloss's Underline(true) renders
// each rune of a styled string with its own individual SGR wrap/reset
// (confirmed empirically, present in both the old and new message
// rendering code -- not a Phase 5 regression), so a literal contiguous
// substring search across styled text needs the SGR noise stripped first
// to reassemble the original characters.
var sgrPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripSGR(s string) string { return sgrPattern.ReplaceAllString(s, "") }

func newMarkdownTestApp() *App {
	return &App{theme: themes.GetDefaultTheme()}
}

// Lipgloss's default renderer auto-detects terminal color support from
// stdout at first use; under `go test` stdout isn't a real TTY, so it
// silently downgrades to no styling at all (mentionStyle/linkStyle would
// render as plain unstyled text, unlike glamour's own ansi rendering,
// which always defaults to termenv.TrueColor regardless of TTY detection
// -- see ansi.Options.ColorProfile's zero value). Force TrueColor so tests
// asserting on lipgloss-rendered ANSI codes reflect how the shipped app
// actually behaves in a real terminal, not this package's test harness.
func init() {
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// waitForZone polls zone.Get briefly -- zone.Scan queues updates to an
// async worker rather than applying them synchronously (see bubblezone's
// own doc comment on Scan), so an immediate Get can race it. Mirrors the
// same poll settings_mouse_test.go already uses for this.
func waitForZone(t *testing.T, id string) *zone.ZoneInfo {
	t.Helper()
	var z *zone.ZoneInfo
	for i := 0; i < 100; i++ {
		if z = zone.Get(id); z != nil {
			return z
		}
		time.Sleep(time.Millisecond)
	}
	return nil
}

// TestRenderMessageContentBareURLDoesNotDoubleRenderViaGlamour is a
// regression test for the real bug TestProbeChatGlamourPipeline found:
// goldmark's GFM/Linkify extension auto-links bare URLs, and glamour's
// LinkElement renderer then prints both the link text and the href as two
// separate VISIBLE text runs -- for a bare autolink where text==href, the
// URL appeared twice as plain rendered text. newChatMarkdownRenderer
// excludes Linkify and Concord substitutes/restores URLs itself instead,
// producing a single OSC 8 hyperlink -- which legitimately embeds the URL
// twice (once in the invisible escape sequence, once as the visible link
// text; see osc8Link), so the count to guard against here is 3+ (glamour's
// own duplication stacked on top), not the OSC 8 format's own expected 2.
func TestRenderMessageContentBareURLDoesNotDoubleRenderViaGlamour(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "check this out https://example.com/path nice", 80, false)
	stripped := stripSGR(out)

	if n := strings.Count(stripped, "https://example.com/path"); n != 2 {
		t.Errorf("expected the URL to appear exactly twice (OSC 8's own escape-sequence + visible-text embedding), got %d\noutput: %q", n, stripped)
	}
}

// TestRenderMessageContentPreservesLinkZoneMarking confirms a bare-URL
// message still produces the same "link:<msgID>:<n>" bubblezone marker
// mouse.go's handleMainViewMouse looks up (fmt.Sprintf("link:%s:%d", ...)),
// now that URLs flow through the placeholder-substitution pipeline instead
// of the old direct regex-and-wrap pass. zone.Mark embeds an opaque
// internal marker, not the literal ID string, so this scans the rendered
// output (as the real render loop does) and looks the zone up by ID
// afterward rather than substring-searching for it.
func TestRenderMessageContentPreservesLinkZoneMarking(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "see https://example.com for details", 80, false)
	zone.Scan(out)

	wantZone := fmt.Sprintf("link:%s:0", msgID)
	if z := waitForZone(t, wantZone); z == nil {
		t.Errorf("expected a registered zone %q after scanning the rendered output", wantZone)
	}
	if !strings.Contains(out, "https://example.com") {
		t.Errorf("expected the raw URL text to survive into rendered output, got: %q", out)
	}
}

// TestRenderMessageContentMultipleLinksGetDistinctZones confirms linkIndex
// increments correctly across multiple URLs in one message, matching the
// pre-existing per-message numbering scheme.
func TestRenderMessageContentMultipleLinksGetDistinctZones(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "first https://a.example second https://b.example", 80, false)
	zone.Scan(out)

	for i := 0; i < 2; i++ {
		want := fmt.Sprintf("link:%s:%d", msgID, i)
		if z := waitForZone(t, want); z == nil {
			t.Errorf("expected a registered zone %q after scanning the rendered output", want)
		}
	}
}

// TestRenderMessageContentMentionHighlighting confirms a personal @mention
// still gets highlighted after the pipeline rewrite -- mentions survive
// glamour untouched (confirmed empirically in TestProbeChatGlamourPipeline)
// and are highlighted in a post-glamour pass exactly as before.
func TestRenderMessageContentMentionHighlighting(t *testing.T) {
	a := newMarkdownTestApp()
	a.activeConn = &ServerConnection{User: &models.User{Username: "jordan"}}
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "hey @jordan check this out", 80, false)

	if !strings.Contains(out, "@jordan") {
		t.Fatalf("expected @jordan to survive into rendered output, got: %q", out)
	}
	// The mention style applies Bold + a distinct color -- lipgloss emits
	// both in one combined SGR sequence ("\x1b[1;<color-params>m"), so
	// check for the bold parameter as either the sole code ("[1m") or the
	// leading parameter in a combined one ("[1;").
	if !strings.Contains(out, "\x1b[1m") && !strings.Contains(out, "\x1b[1;") {
		t.Errorf("expected the mention to carry bold styling, got: %q", out)
	}
}

// TestRenderMessageContentEveryoneHighlighting covers the @everyone/@here
// branch of the same mention pass.
func TestRenderMessageContentEveryoneHighlighting(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "@everyone please read this", 80, false)

	if !strings.Contains(out, "@everyone") {
		t.Errorf("expected @everyone to survive into rendered output, got: %q", out)
	}
}

// TestRenderMessageContentReplyQuotePreserved confirms the permanently-
// embedded "↩ Author: quote\n" first line (see handleSendMessage's
// quotedContent) keeps its own fixed dim-italic treatment and is not run
// through markdown rendering itself, while the rest of the message still
// gets full markdown treatment.
func TestRenderMessageContentReplyQuotePreserved(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "↩ Alice: original message\n**bold reply**", 80, false)

	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d: %q", len(lines), out)
	}
	if !strings.Contains(lines[0], "Alice") || !strings.Contains(lines[0], "original message") {
		t.Errorf("expected the quote line to survive verbatim, got: %q", lines[0])
	}
	rest := strings.Join(lines[1:], "\n")
	if !strings.Contains(rest, "bold reply") {
		t.Errorf("expected the reply body text to survive, got: %q", rest)
	}
}

// TestRenderMessageContentBasicFormatting is a baseline smoke test for
// bold/italic/inline-code rendering actually producing styled (not plain)
// output.
func TestRenderMessageContentBasicFormatting(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "**bold** _italic_ `code`", 80, false)

	if !strings.Contains(out, "bold") || !strings.Contains(out, "italic") || !strings.Contains(out, "code") {
		t.Fatalf("expected the literal words to survive rendering, got: %q", out)
	}
	if !strings.Contains(out, "\x1b[") {
		t.Errorf("expected at least some ANSI styling in the output, got plain text: %q", out)
	}
}

// TestRenderMessageContentDoesNotPadToFullWidth is a regression test for a
// real bug found live 2026-09-08: a short own-message ("Yo! Anyone out
// there?") rendered stuck at the left edge instead of right-aligned.
// glamour pads every rendered line with individually ANSI-wrapped trailing
// spaces out to the full word-wrap width it was given -- harmless for
// Help & Guide (always left-aligned), but it meant renderMessageContent's
// output already visually measured as the full viewport width by the time
// app.go's own Width(viewportWidth).Align(lipgloss.Right) wrap ran for an
// own message, leaving no room left to redistribute. Confirms the fix
// (trimTrailingRenderedPadding) strips that padding so a short message's
// rendered width reflects its real content, not the wrap width it was
// rendered at.
func TestRenderMessageContentDoesNotPadToFullWidth(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	const viewportWidth = 80
	out := a.renderMessageContent(msgID, "Yo! Anyone out there?", viewportWidth, true)

	if w := lipgloss.Width(out); w >= viewportWidth {
		t.Errorf("expected a short message's rendered width to be much less than the %d-column viewport, got %d\noutput: %q", viewportWidth, w, out)
	}
}

// TestRenderMessageContentRightAlignsCorrectly is the end-to-end version:
// apply the exact same Width().Align(lipgloss.Right) wrap app.go uses for
// an own message, and confirm the text actually lands against the right
// edge rather than the left.
func TestRenderMessageContentRightAlignsCorrectly(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	const viewportWidth = 80
	rendered := a.renderMessageContent(msgID, "Yo! Anyone out there?", viewportWidth, true)
	aligned := lipgloss.NewStyle().Width(viewportWidth).Align(lipgloss.Right).PaddingRight(2).Render(rendered)

	plain := stripSGR(aligned)
	trimmedRight := strings.TrimRight(plain, " ")
	if !strings.HasSuffix(trimmedRight, "there?") {
		t.Fatalf("expected the message text near the end of the line, got: %q", plain)
	}
	// Right-aligned means real leading whitespace before the text -- a
	// left-aligned (broken) render would have none.
	leadingSpaces := len(plain) - len(strings.TrimLeft(plain, " "))
	if leadingSpaces < viewportWidth/2 {
		t.Errorf("expected substantial leading whitespace (right-aligned), got only %d leading spaces in: %q", leadingSpaces, plain)
	}
}

// TestRenderMessageContentLongMessageWrapsAndEachLineTrims confirms the
// padding trim (per rendered line) doesn't interfere with real word-wrap.
// A line that legitimately fills the full wrap width by chance (a real
// word boundary happens to land exactly there) is correct, expected
// behavior, not a bug -- so this checks the message's last line
// specifically, deliberately picked short so it can't plausibly hit the
// wrap width by chance, and checks the message wrapped across multiple
// lines at all.
func TestRenderMessageContentLongMessageWrapsAndEachLineTrims(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	const viewportWidth = 40
	long := "This is a fairly long message that should wrap across more than one line at a narrow viewport width, tail."
	out := a.renderMessageContent(msgID, long, viewportWidth, false)

	lines := strings.Split(out, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected the long message to wrap across multiple lines at width %d, got %d line(s): %q", viewportWidth, len(lines), out)
	}
	last := lines[len(lines)-1]
	if w := lipgloss.Width(last); w >= viewportWidth {
		t.Errorf("expected the short final line to trim well under %d, got %d: %q", viewportWidth, w, last)
	}
}

// TestRenderMessageContentNoTableSyntaxLeaksThrough confirms a GFM pipe
// table in a chat message doesn't render as an actual table (Table is
// deliberately excluded from newChatMarkdownRenderer's extension set) --
// it should fall back to plain paragraph text with the literal pipe
// characters intact, matching item 3a(b)'s "avoid tables" decision.
func TestRenderMessageContentNoTableSyntaxLeaksThrough(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	out := a.renderMessageContent(msgID, "| a | b |\n|---|---|\n| 1 | 2 |", 80, false)

	// A real rendered table would draw box-drawing separator characters
	// (─, │, ┼) via ansi.StyleTable's separators; confirm none of those
	// appear, i.e. goldmark didn't parse this as a table block at all.
	for _, boxChar := range []string{"─", "┼"} {
		if strings.Contains(out, boxChar) {
			t.Errorf("expected no table box-drawing characters in output (tables should be disabled), found %q in: %q", boxChar, out)
		}
	}
}

// TestMessageRenderCacheHitReturnsIdenticalOutput confirms a second call
// with identical (msgID, text, width, theme) returns the cached string
// rather than recomputing -- verified indirectly by checking the cache map
// actually gets populated and a changed input produces a different cached
// entry.
func TestMessageRenderCacheHitReturnsIdenticalOutput(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New()

	first := a.renderMessageContent(msgID.String(), "hello world", 80, false)
	if len(a.messageRenderCache) != 1 {
		t.Fatalf("expected 1 cache entry after first render, got %d", len(a.messageRenderCache))
	}
	second := a.renderMessageContent(msgID.String(), "hello world", 80, false)
	if first != second {
		t.Errorf("expected identical output from cache hit, got different strings")
	}
}

// TestMessageRenderCacheInvalidatesOnContentChange confirms editing a
// message's text (same ID, different content -- e.g. after a message
// edit) produces a fresh render, not a stale cached one.
func TestMessageRenderCacheInvalidatesOnContentChange(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	first := a.renderMessageContent(msgID, "original text", 80, false)
	second := a.renderMessageContent(msgID, "edited text", 80, false)

	if first == second {
		t.Error("expected different output after content changed, got identical rendered strings")
	}
	if !strings.Contains(second, "edited") {
		t.Errorf("expected the new content to appear in the re-rendered output, got: %q", second)
	}
}

// TestMessageRenderCacheInvalidatesOnWidthChange confirms a width change
// (e.g. terminal resize) bypasses a same-ID same-text cache entry.
func TestMessageRenderCacheInvalidatesOnWidthChange(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	a.renderMessageContent(msgID, "hello world", 80, false)
	entry80 := a.messageRenderCache[uuid.MustParse(msgID)]

	a.renderMessageContent(msgID, "hello world", 40, false)
	entry40 := a.messageRenderCache[uuid.MustParse(msgID)]

	if entry80.Width == entry40.Width {
		t.Fatal("test setup bug: expected two different widths to actually be recorded")
	}
	if entry40.Rendered == entry80.Rendered {
		// Not a hard requirement (a short message might wrap identically at
		// both widths), so this is a soft check via width field only above;
		// keep this as documentation, no assertion here.
		t.Log("rendered output happened to be identical at both widths -- fine, the cache key (width) itself is what's under test")
	}
}

// TestAnsiOpenCodeExtractsStylePrefix confirms the NUL-sentinel extraction
// trick actually isolates just the "turn styling on" escape prefix, not
// leftover content or the trailing reset.
func TestAnsiOpenCodeExtractsStylePrefix(t *testing.T) {
	open := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color("#333333")))
	if open == "" {
		t.Fatal("expected a non-empty ANSI open prefix")
	}
	if !strings.HasPrefix(open, "\x1b[") {
		t.Errorf("expected the prefix to start with an ANSI escape, got: %q", open)
	}
	if strings.Contains(open, "\x00") {
		t.Errorf("expected the sentinel byte to be stripped out, got: %q", open)
	}
}

// TestReassertBackgroundAfterResetsClosesEveryGap is a regression test for
// a real bug found live 2026-09-08: selecting a markdown-rendered message
// (Alt+M) showed the highlight background only in the plain-text gaps
// between styled spans -- disconnected boxes instead of one solid
// highlight -- because lipgloss's outer Background().Render() only opens
// the background once, and every ANSI reset already embedded in the
// glamour-rendered content (one after every heading/bold/code span) wiped
// it with nothing to reopen it. This proves the fix: after patching,
// every reset in the content is immediately followed by the background's
// own open code, so nothing after a reset can render without it.
func TestReassertBackgroundAfterResetsClosesEveryGap(t *testing.T) {
	// Simulates real glamour output shape: styled span + reset + plain text
	// + styled span + reset -- i.e. multiple independent reset boundaries,
	// not just one.
	boldStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff0000"))
	content := boldStyle.Render("heading") + " plain text " + boldStyle.Render("more bold")

	if !strings.Contains(content, ansiResetSeq) {
		t.Fatal("test setup bug: expected the simulated content to actually contain reset sequences")
	}

	patched := reassertBackgroundAfterResets(content, "#333333")
	bgOpen := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color("#333333")))

	// Every reset in the patched content must be immediately followed by
	// the background's own open code -- i.e. no reset is left "bare". The
	// reset+reopen pair should appear exactly as many times as there were
	// resets in the original content.
	wantCount := strings.Count(content, ansiResetSeq)
	gotCount := strings.Count(patched, ansiResetSeq+bgOpen)
	if gotCount != wantCount {
		t.Errorf("expected every one of the %d resets to be immediately followed by the background's open code, got %d matches\npatched: %q", wantCount, gotCount, patched)
	}
}

// TestRenderMessageContentHighlightHasNoBackgroundGaps is the end-to-end
// version of the same regression: render a real multi-span markdown
// message through renderMessageContent, apply the exact same
// reassertBackgroundAfterResets + Background().Width().Render() sequence
// app.go's message-select highlight path uses, and confirm no reset is
// ever immediately followed by *visible* unstyled content. A reset right
// before a newline or the end of the string is fine -- lipgloss's own
// per-line Width() wrap legitimately closes out styling at each line
// boundary, and the next line independently reopens its own background at
// its own start (nothing is visibly unstyled there); the actual bug this
// guards is a reset followed by real characters with no reopen in between.
func TestRenderMessageContentHighlightHasNoBackgroundGaps(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	rendered := a.renderMessageContent(msgID, "# Heading\n\nSome **bold** and _italic_ text with `code` too.", 80, false)

	bg := a.theme.Colors.Selection
	patched := reassertBackgroundAfterResets(rendered, bg)
	highlighted := lipgloss.NewStyle().Background(lipgloss.Color(bg)).Width(80).PaddingLeft(2).Render(patched)

	bgOpen := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(bg)))

	for _, resetSeq := range []string{ansiResetSeq, ansiResetSeqShort} {
		pos := 0
		for {
			idx := strings.Index(highlighted[pos:], resetSeq)
			if idx == -1 {
				break
			}
			after := pos + idx + len(resetSeq)
			pos = after
			if after >= len(highlighted) {
				continue // end of string -- fine
			}
			rest := highlighted[after:]
			if strings.HasPrefix(rest, bgOpen) {
				continue // reopened -- fine
			}
			if rest[0] == '\n' {
				continue // line boundary, nothing visible follows on this line -- fine
			}
			if strings.HasPrefix(rest, ansiResetSeq) || strings.HasPrefix(rest, ansiResetSeqShort) {
				continue // a second, redundant reset immediately follows -- harmless, no visible content in between
			}
			end := len(rest)
			if end > 20 {
				end = 20
			}
			t.Fatalf("found a reset at byte %d followed by unstyled visible content (byte %q), not the highlight reopen or a newline\ncontext: %q", after, rest[0], rest[:end])
		}
	}
}

// TestMessageRenderCacheClearedOnThemeChange confirms SetTheme wipes the
// whole cache (see App.messageRenderCache's doc comment) rather than
// leaving stale entries that would show the wrong theme's colors.
func TestMessageRenderCacheClearedOnThemeChange(t *testing.T) {
	a := newMarkdownTestApp()
	msgID := uuid.New().String()

	a.renderMessageContent(msgID, "hello world", 80, false)
	if len(a.messageRenderCache) != 1 {
		t.Fatalf("expected 1 cache entry, got %d", len(a.messageRenderCache))
	}

	other, err := themes.GetTheme("nord")
	if err != nil {
		t.Fatalf("GetTheme(nord): %v", err)
	}
	a.SetTheme(other)

	if len(a.messageRenderCache) != 0 {
		t.Errorf("expected SetTheme to clear the message render cache, still has %d entries", len(a.messageRenderCache))
	}
}
