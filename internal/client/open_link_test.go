package client

import (
	"strings"
	"testing"
)

// Finding 2 (security audit 2026-11-05): on Windows a link was opened with
// "cmd /c start <url>", and links may contain &, so clicking
// https://example.com/&calc ran calc.
func TestOpeningALinkNeverUsesAShell(t *testing.T) {
	link := "https://example.com/&calc"
	for _, goos := range []string{"windows", "darwin", "linux"} {
		argv := browserCommand(goos, link)
		for _, a := range argv {
			if l := strings.ToLower(a); l == "cmd" || l == "cmd.exe" || l == "/c" || l == "sh" || l == "powershell" {
				t.Errorf("%s opens links through a shell: %q", goos, argv)
			}
		}
		if argv[len(argv)-1] != link {
			t.Errorf("%s: the link isn't passed as its own argument: %q", goos, argv)
		}
	}
}

func TestOnlyWebLinksOpen(t *testing.T) {
	good := []string{"https://example.com/a?b=c&d=e", "http://192.168.1.66:8090/x", "HTTPS://Example.com"}
	for _, l := range good {
		if _, err := safeLink(l); err != nil {
			t.Errorf("%q refused: %v", l, err)
		}
	}
	bad := []string{
		"file:///etc/passwd", "javascript:alert(1)", "ms-settings:", "\\\\host\\share",
		"https://", "https://example.com/\x1b]52;c;aGk=\x07", "https://example.com/\nx",
		"-https://example.com", "https://user@evil.example/",
	}
	for _, l := range bad {
		if got, err := safeLink(l); err == nil {
			t.Errorf("%q allowed as %q", l, got)
		}
	}
}
