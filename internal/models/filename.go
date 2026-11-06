package models

import (
	"path"
	"strings"
	"unicode"
)

// SafeFilename turns a shared file's name, which comes from another person,
// into a plain file name that stays in the folder it's saved to: no folders
// (either slash), no control characters, nothing Windows refuses, never "",
// "." or "..". A name like "../.bashrc" used to land outside Downloads when
// the save dialog couldn't open (security audit 2026-11-05, finding 4).
func SafeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	var b strings.Builder
	for _, r := range name {
		switch {
		case unicode.IsControl(r), r == 0x7f:
		case strings.ContainsRune(`<>:"|?*/`, r):
			b.WriteRune('_')
		default:
			b.WriteRune(r)
		}
	}
	name = strings.TrimRight(strings.TrimSpace(b.String()), ". ")
	name = strings.TrimLeft(name, " ")
	if name == "" || name == "." || name == ".." || strings.Trim(name, ".") == "" {
		return "download"
	}
	stem := strings.ToUpper(name)
	if i := strings.IndexByte(stem, '.'); i >= 0 {
		stem = stem[:i]
	}
	switch stem {
	case "CON", "PRN", "AUX", "NUL",
		"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		name = "_" + name
	}
	if len(name) > 200 {
		ext := path.Ext(name)
		if len(ext) > 20 {
			ext = ""
		}
		name = strings.ToValidUTF8(name[:200-len(ext)], "") + ext
	}
	return name
}
