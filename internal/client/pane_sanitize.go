package client

import (
	"strings"
	"unicode/utf8"
)

// sanitizePaneFrame strips everything from a plugin-rendered frame except
// printable text, newlines/tabs, SGR styling (ESC[...m) and OSC 8
// hyperlinks. A frame is written straight into the viewer's terminal, so
// without this a plugin could set the clipboard (OSC 52), retitle the
// window, move the cursor to scribble over Concord's own UI, or switch
// screen modes -- none of which a pane ever needs.
func sanitizePaneFrame(s string) string {
	if !strings.ContainsAny(s, "\x1b\r\x07\x08\x0b\x0c\x7f") && !hasOtherControls(s) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0x1b {
			i = copyEscape(&b, s, i)
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			// C0/C1 controls (C1 includes the 8-bit CSI/OSC introducers).
		case r == utf8.RuneError && size == 1:
			// Invalid UTF-8 byte; drop rather than pass through raw.
		default:
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}

// hasOtherControls reports whether s contains a control character the
// ContainsAny fast path above doesn't list (other C0 bytes, or C1 runes).
func hasOtherControls(s string) bool {
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\t') || (r >= 0x80 && r <= 0x9f) {
			return true
		}
	}
	return false
}

// copyEscape handles the escape sequence starting at s[i] (an ESC byte),
// writing it to b only if it's allowed, and returns the index just past it.
func copyEscape(b *strings.Builder, s string, i int) int {
	if i+1 >= len(s) {
		return len(s)
	}
	switch s[i+1] {
	case '[': // CSI: parameters 0x30-0x3F, intermediates 0x20-0x2F, final 0x40-0x7E
		j := i + 2
		for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
			j++
		}
		if j >= len(s) || s[j] < 0x40 || s[j] > 0x7e {
			return j // malformed or truncated: drop what we consumed
		}
		if s[j] == 'm' {
			b.WriteString(s[i : j+1])
		}
		return j + 1
	case ']', 'P', 'X', '^', '_': // OSC / DCS / SOS / PM / APC, ended by BEL or ESC \
		body := i + 2
		end, next := len(s), len(s)
		for j := body; j < len(s); j++ {
			if s[j] == 0x07 {
				end, next = j, j+1
				break
			}
			if s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\' {
				end, next = j, j+2
				break
			}
		}
		if s[i+1] == ']' && strings.HasPrefix(s[body:end], "8;") && next <= len(s) && end < len(s) {
			b.WriteString(s[i:next])
		}
		return next
	default: // two-byte escapes (ESC 7, ESC c, ...): all dropped
		_, size := utf8.DecodeRuneInString(s[i+1:])
		return i + 1 + size
	}
}
