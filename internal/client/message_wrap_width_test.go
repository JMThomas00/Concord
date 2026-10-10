package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// TestStyleMessageLinesDoesNotReWrapLinesAtTheBoundary is a regression test
// for a real, live-reported bug (2026-09-13): a long, multi-line pasted
// message wrapped "weirdly" -- stray single words dangling alone on their
// own line -- at moderate terminal widths, looking fine only once the
// terminal was widened a lot.
//
// Root cause: renderMessageContent/renderChatMarkdownBody already word-wrap
// a message to the viewport width via glamour. The caller then used to hand
// that whole multi-line result to ONE lipgloss
// Width(viewportWidth).PaddingRight(2).Render() call -- lipgloss doesn't
// just pad/align pre-wrapped content, it performs its OWN word-wrap on any
// line that doesn't fit, and PaddingRight/PaddingLeft(2) silently shrinks
// the usable width by 2 columns from what the content was originally
// wrapped to. Any glamour-wrapped line within 2 columns of the full width
// (common in a long message) got silently re-wrapped a second time.
//
// This proves the fix directly: a line already wrapped to messageWrapWidth
// -- exactly what a real caller always does before reaching this point --
// must come back unchanged (one line in, one line out) once the gutter
// padding is applied, not split further.
//
// Deliberately does NOT test styleMessageLines with a line at the full,
// un-shrunk viewport width: that's not how it's ever actually called in
// production (messageWrapWidth's shrinkage and styleMessageLines' padding
// are two halves of the same fix and are only ever used together -- see
// TestRenderMessageContentEndToEndSurvivesGutterPaddingUnchanged for the
// full real pipeline).
func TestStyleMessageLinesDoesNotReWrapLinesAtTheBoundary(t *testing.T) {
	const viewportWidth = 20
	line := strings.Repeat("x", messageWrapWidth(viewportWidth)) // exactly at the boundary messageWrapWidth guarantees, no slack at all
	style := lipgloss.NewStyle().Width(viewportWidth).Align(lipgloss.Right).PaddingRight(messageGutterWidth)

	got := styleMessageLines(line, style)

	gotLines := strings.Split(got, "\n")
	if len(gotLines) != 1 {
		t.Fatalf("expected a single boundary-width line to stay one line after styling, got %d lines: %q", len(gotLines), got)
	}
}

// TestMessageWrapWidthReservesTheGutterPaddingUsesLater confirms
// messageWrapWidth's output leaves exactly enough room for the
// messageGutterWidth padding the caller applies afterward -- the other
// half of the fix: it's not enough for styleMessageLines to avoid
// re-wrapping multiple lines as one blob if glamour was still asked to
// wrap all the way out to the full, un-padded viewport width in the first
// place.
func TestMessageWrapWidthReservesTheGutterPaddingUsesLater(t *testing.T) {
	viewportWidth := 60
	got := messageWrapWidth(viewportWidth)
	want := viewportWidth - messageGutterWidth
	if got != want {
		t.Errorf("messageWrapWidth(%d) = %d, want %d (viewportWidth - messageGutterWidth)", viewportWidth, got, want)
	}
}

// TestRenderMessageContentEndToEndSurvivesGutterPaddingUnchanged drives the
// real renderMessageContent + messageWrapWidth + styleMessageLines
// pipeline together (the exact sequence renderMainView's message loop
// uses) with a long, multi-line, own-message (right-aligned) chat message
// -- the same shape that triggered the live bug -- and confirms the final,
// fully-styled output has exactly as many lines as the raw markdown render
// did. Extra lines appearing only after the gutter-padding pass is applied
// would mean the old double-wrap bug regressed.
func TestRenderMessageContentEndToEndSurvivesGutterPaddingUnchanged(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)

	const viewportWidth = 70
	longMessage := "This is a fairly long line of chat text meant to word-wrap across several lines at a moderate terminal width, exactly the kind of message that exposed the double-wrap bug when combined with the two columns of gutter padding every message reserves on its aligned side."

	msgID := uuid.New().String()
	raw := a.renderMessageContent(msgID, longMessage, messageWrapWidth(viewportWidth), true)
	rawLineCount := len(strings.Split(raw, "\n"))
	if rawLineCount < 2 {
		t.Fatalf("test setup bug: expected the long message to wrap across multiple lines already, got %d line(s)", rawLineCount)
	}

	contentStyle := lipgloss.NewStyle().
		Width(viewportWidth).
		Align(lipgloss.Right).
		PaddingRight(messageGutterWidth)
	styled := styleMessageLines(raw, contentStyle)
	styledLineCount := len(strings.Split(styled, "\n"))

	if styledLineCount != rawLineCount {
		t.Errorf("gutter-padding pass changed the line count from %d to %d -- a glamour-wrapped line got re-wrapped again by the outer Width/Padding style", rawLineCount, styledLineCount)
	}
}
