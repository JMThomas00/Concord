package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// newTestMessageInput builds a textarea matching production's actual
// keymap (app.go's real input construction removes plain "enter" from
// InsertNewline so the app-level "Enter sends" logic owns it instead) --
// a plain textarea.New() keeps bubbles' own library default, which still
// treats bare "enter" as InsertNewline and would silently insert a
// newline itself regardless of anything handleKeyPress decides, masking
// whatever these tests are actually trying to isolate.
func newTestMessageInput() textarea.Model {
	input := textarea.New()
	input.KeyMap.InsertNewline.SetKeys("ctrl+enter", "ctrl+j", "shift+enter")
	return input
}

// TestRapidEnterAfterPasteInsertsNewlineInsteadOfSending is a regression
// test for a real, live-reported bug (2026-09-13): pasting multi-line text
// into the message box sent each line as its own message in rapid-fire
// succession. Root cause is platform-specific with no platform-specific
// fix available -- see lastKeystrokeAt's doc comment in app.go for the
// full explanation (bubbletea's Windows console-input reader has no
// concept of "paste" at all; a POSIX terminal without bracketed-paste
// support hits the identical failure mode). The fix times the gap between
// keystrokes instead: an Enter arriving within pasteBurstKeyThreshold of
// the previous keystroke is treated as one line-ending of a machine-
// replayed paste, not a deliberate send.
//
// This drives the real a.Update() path (not handleKeyPress directly) so
// both of Update()'s two key-handling switches run, exactly as a real
// keystroke would -- matching typing_stop_e2e_test.go's own established
// pattern for this class of bug.
func TestRapidEnterAfterPasteInsertsNewlineInsteadOfSending(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()

	// Type "hi" then an Enter with no sleep in between -- on a real
	// machine-replayed paste burst, consecutive keystrokes land
	// microseconds to low-single-digit milliseconds apart; a tight Go loop
	// with no explicit sleep reproduces that same sub-millisecond spacing.
	// Deliberately NOT using invokeCmd here (unlike typing_stop_e2e_test.go's
	// pattern) -- one of the commands textarea.Update() can return is a real
	// cursor-blink tea.Tick, which invokeCmd would block on synchronously,
	// reintroducing exactly the kind of inter-keystroke delay this test
	// needs to rule out. These assertions only care about a.input.Value()'s
	// end state, not any follow-up message, so the returned cmds are safe
	// to discard.
	for _, ch := range "hi" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	a.Update(tea.KeyMsg{Type: tea.KeyEnter})

	got := a.input.Value()
	if got == "" {
		t.Fatal("expected the rapid-fire Enter to be treated as part of a paste burst and NOT send the message, but the input was cleared (message was sent)")
	}
	if !containsNewline(got) {
		t.Errorf("expected a literal newline to be inserted in place of sending, got input value %q with no newline", got)
	}
}

// TestSlowEnterStillSendsMessage is the regression guard alongside the fix
// above: a genuine, normally-paced Enter (a real gap since the previous
// keystroke, well above pasteBurstKeyThreshold) must still send the
// message exactly as before -- handleSendMessage unconditionally clears
// the input box as its first step, so an emptied input after Enter is
// sufficient proof the send path was taken rather than the newline-insert
// path.
func TestSlowEnterStillSendsMessage(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()

	for _, ch := range "hi" {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		invokeCmd(cmd)
	}

	// A real, deliberate pause before hitting Enter -- comfortably above
	// pasteBurstKeyThreshold (20ms), comfortably below anything a human
	// would notice.
	time.Sleep(50 * time.Millisecond)

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyEnter})
	invokeCmd(cmd)

	if got := a.input.Value(); got != "" {
		t.Errorf("expected a normally-paced Enter to send the message (clearing the input), but input still contains %q", got)
	}
}

// TestRapidTabDuringPasteInsertsLiteralTabInsteadOfCyclingFocus is a
// regression test for a real, serious bug found live (2026-09-13): pasting
// a code snippet with tab-indentation (extremely common) hijacked focus
// away from the compose box entirely mid-message. A plain Tab press means
// "cycle focus to the next panel" -- so a tab character arriving as part
// of a machine-replayed paste (see lastKeystrokeAt's doc comment) silently
// moved focus off Input, and every character typed after that landed
// wherever focus ended up instead. Observed live landing on the members
// panel, where the very next letter ("r", from mid-word in "return")
// triggered ITS OWN "assign role to selected member" shortcut, which
// overwrote the entire compose box with a "/role assign @user " template
// and swallowed the rest of the paste into that -- not just losing the
// tab, but silently discarding and replacing everything already typed.
//
// The fix inserts a literal tab (which textarea's own sanitizer turns into
// 4 spaces, see runeutil's default ReplaceTab) instead of cycling focus,
// whenever a Tab arrives as part of a detected paste burst while composing.
func TestRapidTabDuringPasteInsertsLiteralTabInsteadOfCyclingFocus(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()

	for _, ch := range "hi" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}
	a.Update(tea.KeyMsg{Type: tea.KeyTab})

	if a.focus != FocusInput {
		t.Fatalf("expected focus to stay on the compose box during a paste burst, but a rapid-fire Tab cycled it away to %v", a.focus)
	}
	if got := a.input.Value(); !strings.Contains(got, "hi") {
		t.Errorf("expected the compose box to still contain the text typed before the tab, got %q", got)
	}
}

// TestSlowTabStillCyclesFocus is the regression guard alongside the fix
// above: a genuine, normally-paced Tab press (a real gap since the
// previous keystroke) must still cycle focus away from the compose box
// exactly as before.
func TestSlowTabStillCyclesFocus(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()

	for _, ch := range "hi" {
		_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
		invokeCmd(cmd)
	}

	time.Sleep(50 * time.Millisecond)

	_, cmd := a.Update(tea.KeyMsg{Type: tea.KeyTab})
	invokeCmd(cmd)

	if a.focus == FocusInput {
		t.Error("expected a normally-paced Tab to cycle focus away from the compose box, but it stayed on FocusInput")
	}
}

func containsNewline(s string) bool {
	for _, r := range s {
		if r == '\n' {
			return true
		}
	}
	return false
}
