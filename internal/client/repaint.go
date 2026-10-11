package client

import tea "github.com/charmbracelet/bubbletea"

// repaintAll makes the next frame rewrite every line of the screen, in
// place, even lines the renderer thinks are unchanged: for the rare
// terminals that miss a change.
//
// Never use tea.ClearScreen for this. It erases the screen at once and the
// new frame follows a frame later, so the window shows black in between: a
// visible flash (reported 2026-10-11 on every message sent). Bubble Tea v1
// has no public "repaint" command; ClearScrollArea is the one that clears
// the renderer's cache without erasing anything (it's deprecated with the
// scroll-region feature it belongs to, which Concord doesn't use).
func repaintAll() tea.Msg {
	return tea.ClearScrollArea() //nolint:staticcheck // see above: the only flash-free repaint in Bubble Tea v1
}
