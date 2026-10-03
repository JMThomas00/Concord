package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

func TestShouldToast(t *testing.T) {
	for _, c := range []struct {
		mode, scope               string
		mention, here, thisServer bool
		want                      bool
	}{
		{"", "", false, false, true, true},                                // all, by default
		{"", "", false, true, true, false},                                // not the channel you're in
		{ToastModeMentions, "", false, false, true, false},                // mentions only
		{ToastModeMentions, "", true, false, true, true},                  // ...and this is one
		{ToastModeOff, "", true, false, true, false},                      // off
		{"", DesktopNotifyScopeCurrentServer, false, false, false, false}, // another server
	} {
		if got := shouldToast(c.mode, c.scope, c.mention, c.here, c.thisServer); got != c.want {
			t.Errorf("%+v: %v", c, got)
		}
	}
}

// Toasts stack in the bottom left, newest lowest, and a click on a
// message's toast opens its channel.
func TestMessageToastsStackAndClick(t *testing.T) {
	a := newLayoutTestApp(t, 140, 40)
	a.view = ViewMain
	server, chanA, chanB := uuid.New(), uuid.New(), uuid.New()
	a.toastMessage(server, chanA, "amy", "Sequoia", "general", "hello   there", false)
	a.toastMessage(server, chanB, "ben", "Sequoia", "dev", "@gh0st look", true)
	frame := strings.Repeat(strings.Repeat(" ", 140)+"\n", 39) + strings.Repeat(" ", 140)
	a.paintToasts(frame, time.Now()) // they appear, sliding in
	out := ansi.Strip(a.paintToasts(frame, time.Now().Add(time.Second)))
	lines := strings.Split(out, "\n")
	amy, ben := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "amy: hello there") {
			amy = i
		}
		if strings.Contains(l, "ben: @gh0st look") {
			ben = i
		}
	}
	if amy < 0 || ben < 0 || ben <= amy || !strings.HasPrefix(strings.TrimLeft(lines[ben], " "), "▎") {
		t.Fatalf("amy at %d, ben at %d:\n%s", amy, ben, out)
	}
	if ben < 30 {
		t.Fatal("the stack isn't at the bottom")
	}
	if !strings.Contains(out, "Mention in #dev") {
		t.Fatal("the mention isn't marked")
	}
	r := a.toastRects[0] // the newest, at the bottom
	if !a.clickToast(tea.MouseMsg{X: r.col + 2, Y: r.row + 1, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}) {
		t.Fatal("the click missed")
	}
	if len(a.toasts) != 1 || a.toasts[0].channelID != chanA {
		t.Fatal("the clicked toast didn't go away")
	}
}
