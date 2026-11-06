package protocol

import "strconv"

// Control characters in what people type (security audit 2026-11-05,
// finding 3). A username, a status or a message's code span could carry
// terminal escape sequences, which the client drew raw: OSC 52 writes the
// clipboard in kitty, WezTerm, foot and Alacritty, so someone could swap what
// you're about to paste. The server strips them from what it accepts and the
// client from what it receives, so an old or hostile server can't send them
// either. Newline and tab stay; everything else in C0, DEL and C1 goes.

// IsControl reports whether r is a control character Concord drops from
// text: C0 except newline and tab, DEL, and C1.
func IsControl(r rune) bool {
	return (r < 0x20 && r != '\n' && r != '\t') || r == 0x7f || (r >= 0x80 && r < 0xa0)
}

// HasControl reports whether s contains a control character IsControl drops.
func HasControl(s string) bool {
	for _, r := range s {
		if IsControl(r) {
			return true
		}
	}
	return false
}

// StripControl removes the control characters IsControl drops from s.
func StripControl(s string) string {
	if !HasControl(s) {
		return s
	}
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if !IsControl(r) {
			out = append(out, r)
		}
	}
	return string(out)
}

// StripControlJSON removes control characters from every string in a JSON
// document without decoding it: \uXXXX escapes and \r \b \f for the ones
// IsControl drops, and raw DEL and C1 (UTF-8 C2 80..9F). Anything that isn't
// valid JSON stays as invalid as it was.
func StripControlJSON(b []byte) []byte {
	if !mayHaveControl(b) {
		return b
	}
	out := make([]byte, 0, len(b))
	inStr := false
	for i := 0; i < len(b); i++ {
		c := b[i]
		if !inStr {
			out = append(out, c)
			if c == '"' {
				inStr = true
			}
			continue
		}
		switch {
		case c == '"':
			inStr = false
			out = append(out, c)
		case c == '\\' && i+1 < len(b):
			n := b[i+1]
			switch n {
			case 'u':
				if i+5 < len(b) {
					if v, err := strconv.ParseUint(string(b[i+2:i+6]), 16, 32); err == nil && IsControl(rune(v)) {
						i += 5
						continue
					}
				}
				out = append(out, c, n)
				i++
			case 'r', 'b', 'f':
				i++
			default:
				out = append(out, c, n)
				i++
			}
		case c == 0x7f:
		case c == 0xc2 && i+1 < len(b) && b[i+1] >= 0x80 && b[i+1] <= 0x9f:
			i++
		default:
			out = append(out, c)
		}
	}
	return out
}

// mayHaveControl is StripControlJSON's quick check: no backslash, DEL or
// 0xC2 byte means nothing to strip.
func mayHaveControl(b []byte) bool {
	for _, c := range b {
		if c == '\\' || c == 0x7f || c == 0xc2 {
			return true
		}
	}
	return false
}

// KeepsControl reports whether a message's payload is left as it is: plugin
// frames and pane input (terminal output and keys, cleaned by the client's
// sanitizePaneFrame), and voice and file signals (SDP needs \r\n; nothing
// in them is drawn).
func KeepsControl(op OpCode, event EventType) bool {
	switch op {
	case OpPluginPaneFrame, OpPluginPaneInput, OpVoiceSignal, OpFileTransferSignal:
		return true
	}
	switch event {
	case EventPluginPaneFrame, EventPluginPaneInput, EventVoiceSignal, EventFileTransferSignal:
		return true
	}
	return false
}

// StripControl removes control characters from the message's payload,
// unless it's one KeepsControl leaves alone. Both read loops call it on
// every message.
func (m *Message) StripControl() {
	if m != nil && !KeepsControl(m.Op, m.Type) {
		m.Data = StripControlJSON(m.Data)
	}
}
