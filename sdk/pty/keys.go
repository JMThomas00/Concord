package pty

import "strings"

// Encode turns a viewer's keypress -- Bubble Tea's key name, as Concord
// relays it ("a", "enter", "ctrl+c", "alt+left", "pgup") -- into the bytes
// a terminal would send a program for it. Unknown keys return "".
func Encode(key string, runes []rune) string {
	alt := ""
	if strings.HasPrefix(key, "alt+") && key != "alt+" {
		alt, key = "\x1b", strings.TrimPrefix(key, "alt+")
	}
	if seq, ok := keySeqs[key]; ok {
		return alt + seq
	}
	if strings.HasPrefix(key, "ctrl+") {
		k := strings.TrimPrefix(key, "ctrl+")
		if len(k) == 1 && k[0] >= 'a' && k[0] <= 'z' {
			return alt + string(rune(k[0]-'a'+1))
		}
		switch k {
		case "@", " ", "space":
			return alt + "\x00"
		case "[":
			return alt + "\x1b"
		case `\`:
			return alt + "\x1c"
		case "]":
			return alt + "\x1d"
		case "^":
			return alt + "\x1e"
		case "_":
			return alt + "\x1f"
		}
		return ""
	}
	if len(runes) > 0 {
		return alt + string(runes)
	}
	if strings.HasPrefix(key, "[") && strings.HasSuffix(key, "]") { // Bubble Tea's paste form
		return alt + key[1:len(key)-1]
	}
	if len([]rune(key)) == 1 {
		return alt + key
	}
	return ""
}

var keySeqs = map[string]string{
	"enter": "\r", "tab": "\t", "shift+tab": "\x1b[Z", "backspace": "\x7f", "esc": "\x1b", " ": " ", "space": " ",
	"up": "\x1b[A", "down": "\x1b[B", "right": "\x1b[C", "left": "\x1b[D",
	"shift+up": "\x1b[1;2A", "shift+down": "\x1b[1;2B", "shift+right": "\x1b[1;2C", "shift+left": "\x1b[1;2D",
	"ctrl+up": "\x1b[1;5A", "ctrl+down": "\x1b[1;5B", "ctrl+right": "\x1b[1;5C", "ctrl+left": "\x1b[1;5D",
	"home": "\x1b[H", "end": "\x1b[F", "pgup": "\x1b[5~", "pgdown": "\x1b[6~", "insert": "\x1b[2~", "delete": "\x1b[3~",
	"f1": "\x1bOP", "f2": "\x1bOQ", "f3": "\x1bOR", "f4": "\x1bOS", "f5": "\x1b[15~", "f6": "\x1b[17~",
	"f7": "\x1b[18~", "f8": "\x1b[19~", "f9": "\x1b[20~", "f10": "\x1b[21~", "f11": "\x1b[23~", "f12": "\x1b[24~",
}
