package client

import (
	"strings"
	"testing"
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"
)

// windowsKeys is text as Windows' console input delivers it to Bubbletea
// v1.3.4: one key event per UTF-16 unit, so an emoji outside the Basic
// Multilingual Plane comes as two events, each holding half of it.
func windowsKeys(s string) []tea.Msg {
	var msgs []tea.Msg
	for _, u := range utf16.Encode([]rune(s)) {
		msgs = append(msgs, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune(u)}})
	}
	return msgs
}

func typedRunes(msgs []tea.Msg) string {
	var b strings.Builder
	for _, m := range msgs {
		if k, ok := m.(tea.KeyMsg); ok && k.Type == tea.KeyRunes {
			b.WriteString(string(k.Runes))
		}
	}
	return b.String()
}

// Emoji from the Windows picker (Win+;) arrived as "◆◆". Every kind of
// emoji, typed as Windows sends it, now comes through whole.
func TestEmojiTypedOnWindowsComeThroughWhole(t *testing.T) {
	for _, s := range []string{
		"Hey all! Anyone on? 😄",
		"🍇",
		"👍🏽",          // with a skin tone (two emoji-plane characters)
		"👨‍👩‍👧",       // a family: three emoji joined by zero-width joiners
		"🇺🇸",          // a flag: two regional indicators
		"❤️",          // in the Basic Multilingual Plane, with a variation selector
		"café ½ ✓ 中文", // nothing to join
	} {
		join := surrogateJoiner()
		var out []tea.Msg
		for _, m := range windowsKeys(s) {
			if m = join(m); m != nil {
				out = append(out, m)
			}
		}
		if got := typedRunes(out); got != s {
			t.Errorf("typed %q, got %q", s, got)
		}
	}
}

// A half whose partner never comes is dropped, not typed; anything else
// passes through untouched, including a message that arrives between the
// two halves.
func TestStrayEmojiHalvesAreDropped(t *testing.T) {
	join := surrogateJoiner()
	grape := windowsKeys("🍇")
	var out []tea.Msg
	for _, m := range []tea.Msg{
		grape[0], tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, // high half, then a letter
		grape[1],                                // a low half alone
		grape[0], tea.WindowSizeMsg{}, grape[1], // a resize between the halves
		grape[0], tea.KeyMsg{Type: tea.KeyEnter}, // a high half, then Enter
	} {
		if m = join(m); m != nil {
			out = append(out, m)
		}
	}
	if got := typedRunes(out); got != "a🍇" {
		t.Fatalf("got %q, want %q", got, "a🍇")
	}
	if _, ok := out[len(out)-1].(tea.KeyMsg); !ok || out[len(out)-1].(tea.KeyMsg).Type != tea.KeyEnter {
		t.Fatal("Enter didn't come through")
	}
}

// The whole path: the program's filter, then the app, then the message box.
func TestAnEmojiFromTheWindowsPickerReachesTheMessageBox(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()
	filter := InputFilter()
	for _, m := range windowsKeys("Anyone on? 😄🍇") {
		if m = filter(a, m); m != nil {
			a.Update(m)
		}
	}
	if got := a.input.Value(); got != "Anyone on? 😄🍇" {
		t.Fatalf("the message box has %q", got)
	}
}
