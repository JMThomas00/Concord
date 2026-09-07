package client

import (
	"reflect"
	"runtime"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/bubbles/textarea"
)

// TestParseCommandQuotedArgs covers the exact bug found in manual testing
// 2026-09-05: /attach "D:\Tucan.jpg" Tucan failed with "failed to open
// file: open D:\Concord\build\"D:\Tucan.jpg\"..." because the old
// strings.Fields-based split had no idea about quotes, so the literal
// quote characters ended up inside args[0] and filepath.Abs no longer saw
// a real drive-letter path. splitArgs (used by ParseCommand) fixes this by
// treating a double-quoted span as one token with the quotes stripped.
func TestParseCommandQuotedArgs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		{"quoted path with no spaces", `/attach "D:\Tucan.jpg" Tucan`, []string{"D:\\Tucan.jpg", "Tucan"}},
		{"quoted path containing spaces", `/attach "D:\My Pictures\Tucan.jpg" a caption here`, []string{"D:\\My Pictures\\Tucan.jpg", "a", "caption", "here"}},
		{"unquoted path, unaffected", `/attach D:\Tucan.jpg Tucan`, []string{"D:\\Tucan.jpg", "Tucan"}},
		{"multiple quoted tokens", `/move-voice "@Some User" "General Voice"`, []string{"@Some User", "General Voice"}},
		{"no args", `/help`, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd, err := ParseCommand(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(cmd.Args, tc.want) {
				t.Errorf("expected args %#v, got %#v", tc.want, cmd.Args)
			}
		})
	}
}

// TestAltLeftOnEmptyInputDoesNotHang is the regression test for the
// 2026-09-06 hang report. Root cause fully identified via a live goroutine
// dump (delve attach) of the actually-hung process: the main goroutine was
// stuck in bubbles/textarea.(*Model).wordLeft, an upstream bug (still present
// in the latest v1.0.0 release, not just the pinned v0.20.0) where calling
// word-left navigation on a completely empty textarea spins forever --
// characterLeft is a no-op at row 0/col 0, so wordLeft's break condition can
// never be reached. alt+left (or alt+b) right after sending a command --
// which clears the input, leaving it empty -- is exactly the natural
// sequence that triggers it; Ctrl+S in the original report was just the next
// thing tried, not the actual cause.
func TestAltLeftOnEmptyInputDoesNotHang(t *testing.T) {
	a, _ := newTestAppForRendering(80, nil)
	a.view = ViewMain
	a.focus = FocusInput
	a.input = textarea.New() // a properly-initialized, empty textarea -- the exact precondition that matters
	a.input.Focus()          // textarea.Update ignores key input entirely unless focused

	keys := []tea.KeyMsg{
		{Type: tea.KeyLeft, Alt: true},                     // alt+left
		{Type: tea.KeyRunes, Runes: []rune{'b'}, Alt: true}, // alt+b
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		for _, key := range keys {
			model, _ := a.Update(key)
			a = model.(*App)
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		buf := make([]byte, 1<<20)
		n := runtime.Stack(buf, true)
		t.Fatalf("alt+left/alt+b on an empty input box hung the whole app -- goroutine dump:\n%s", buf[:n])
	}
}

func TestSplitArgsUnterminatedQuote(t *testing.T) {
	// A dangling quote shouldn't panic or drop the token -- it should just
	// behave as if the quote ran to the end of the input.
	got := splitArgs(`"D:\Tucan.jpg`)
	want := []string{`D:\Tucan.jpg`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("expected %#v, got %#v", want, got)
	}
}
