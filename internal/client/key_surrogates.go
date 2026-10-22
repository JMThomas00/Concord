package client

import (
	"unicode/utf16"

	tea "github.com/charmbracelet/bubbletea"
)

// InputFilter is the program's tea.WithFilter (main.go): Bubbletea takes
// only one, so this runs each filter in turn.
func InputFilter() func(tea.Model, tea.Msg) tea.Msg {
	join := surrogateJoiner()
	return func(model tea.Model, msg tea.Msg) tea.Msg {
		if msg = join(msg); msg == nil {
			return nil
		}
		return MouseHoverFilter(model, msg)
	}
}

// surrogateJoiner puts emoji typed on Windows back together. Windows sends
// a character outside the Basic Multilingual Plane (most emoji: from the
// Win+; picker, or pasted, which Windows Terminal also types in as key
// events) as two UTF-16 halves in two key events, and Bubbletea v1.3.4's
// Windows input makes a rune of each half (key_windows.go:
// runes = []rune{e.Char}). Neither half is a character on its own, so the
// message box got "◆◆" instead of 😄. A high half is held back until the
// low half arrives and the two become one rune key; a half without its
// partner is dropped rather than typed as garbage. Other terminals send
// UTF-8 and never produce halves, so this changes nothing there.
func surrogateJoiner() func(tea.Msg) tea.Msg {
	var high rune // a held-back high half, or 0
	return func(msg tea.Msg) tea.Msg {
		k, ok := msg.(tea.KeyMsg)
		if !ok {
			return msg // (a tick between the halves doesn't separate them)
		}
		if k.Type != tea.KeyRunes {
			high = 0
			return msg
		}
		var out []rune
		changed := false
		for _, r := range k.Runes {
			switch {
			case r >= 0xD800 && r <= 0xDBFF: // high half: wait for its partner
				high, changed = r, true
			case r >= 0xDC00 && r <= 0xDFFF: // low half
				if high != 0 {
					out = append(out, utf16.DecodeRune(high, r))
				}
				high, changed = 0, true
			default:
				if high != 0 {
					high, changed = 0, true // its partner never came
				}
				out = append(out, r)
			}
		}
		if !changed {
			return msg
		}
		if len(out) == 0 {
			return nil
		}
		k.Runes = out
		return k
	}
}
