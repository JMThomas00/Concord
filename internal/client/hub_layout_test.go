package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The hub browser fills the screen exactly: a line more and the terminal
// scrolls, cutting off its top or bottom border.
func TestHubBrowserFitsTheScreen(t *testing.T) {
	for _, size := range [][2]int{{190, 49}, {190, 50}, {120, 40}, {100, 24}} {
		a := newLayoutTestApp(t, size[0], size[1])
		a.hubBrowser = newHubBrowserState([]string{"http://grapevine.concord.chat", "https://grapevine.concordchat.cc"}, a.width, a.height)
		a.hubBrowser.hubHealth["http://grapevine.concord.chat"] = -1
		lines := strings.Split(a.renderHubBrowserView(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%dx%d: %d lines", size[0], size[1], len(lines))
		}
		if !strings.HasPrefix(ansi.Strip(lines[0]), "╭") || !strings.HasPrefix(ansi.Strip(lines[len(lines)-1]), "╰") {
			t.Fatalf("%dx%d: the border isn't whole", size[0], size[1])
		}
	}
}
