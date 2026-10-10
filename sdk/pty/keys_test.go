package pty

import "testing"

func TestEncode(t *testing.T) {
	for key, want := range map[string]string{
		"a": "a", "enter": "\r", "backspace": "\x7f", "esc": "\x1b", "tab": "\t",
		"up": "\x1b[A", "alt+left": "\x1b\x1b[D", "ctrl+c": "\x03", "ctrl+a": "\x01",
		"alt+x": "\x1bx", "pgdown": "\x1b[6~", "f5": "\x1b[15~", " ": " ",
		"ctrl+]": "\x1d", "nonsense-key": "",
	} {
		var runes []rune
		if len([]rune(key)) == 1 {
			runes = []rune(key)
		}
		if got := Encode(key, runes); got != want {
			t.Errorf("Encode(%q) = %q, want %q", key, got, want)
		}
	}
	if got := Encode("[héllo]", nil); got != "héllo" {
		t.Errorf("paste = %q", got)
	}
}
