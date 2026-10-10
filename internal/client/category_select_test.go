package client

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// Highlighting a channel group used to show it as an empty text channel
// ("# GENERAL", "No messages yet. Say hello!") and let you post into it.
func TestACategoryShowsItsChannelsNotAChat(t *testing.T) {
	a := newLayoutTestApp(t, 140, 40)
	cat := a.channelTree.FlatList[0].Channel
	a.currentChannel = cat
	a.selectChannel(0)
	view := ansi.Strip(a.renderMainView())
	if strings.Contains(view, "# GENERAL") || strings.Contains(view, "Say hello") {
		t.Fatalf("the group is shown as a channel:\n%s", view)
	}
	for _, want := range []string{"A channel group with 3 channels", "# off-topic", "pick a channel"} {
		if !strings.Contains(view, want) {
			t.Errorf("%q missing:\n%s", want, view)
		}
	}

	a.input.SetValue("hello")
	a.handleSendMessage()
	if a.input.Value() != "hello" || !strings.Contains(a.statusMessage, "channel group") {
		t.Errorf("posting into a group: input %q, status %q", a.input.Value(), a.statusMessage)
	}
}
