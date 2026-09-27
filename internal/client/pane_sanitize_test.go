package client

import (
	"testing"

	"github.com/concord-chat/concord/internal/protocol"
)

func TestSanitizePaneFrame(t *testing.T) {
	link := "\x1b]8;;https://example.com\x1b\\site\x1b]8;;\x1b\\"
	cases := []struct{ name, in, want string }{
		{"plain text untouched", "hello\n\tworld ♟", "hello\n\tworld ♟"},
		{"SGR kept", "\x1b[1;38;2;255;0;0mred\x1b[0m", "\x1b[1;38;2;255;0;0mred\x1b[0m"},
		{"OSC 8 link kept", link, link},
		{"OSC 8 with BEL kept", "\x1b]8;;u\x07x\x1b]8;;\x07", "\x1b]8;;u\x07x\x1b]8;;\x07"},
		{"OSC 52 clipboard dropped", "a\x1b]52;c;aGk=\x07b", "ab"},
		{"window title dropped", "a\x1b]0;pwned\x1b\\b", "ab"},
		{"cursor movement dropped", "a\x1b[2J\x1b[10;10Hb", "ab"},
		{"alt screen dropped", "\x1b[?1049hx", "x"},
		{"DCS dropped", "a\x1bPq#0;2;0;0;0\x1b\\b", "ab"},
		{"two-byte escape dropped", "a\x1bcb\x1b7c", "abc"},
		{"C0 controls dropped", "a\rb\x07c\x08d", "abcd"},
		{"8-bit CSI dropped", "a\u009b2Jb", "a2Jb"},
		{"truncated escape dropped", "abc\x1b[", "abc"},
		{"unterminated OSC dropped", "abc\x1b]52;c;aGk=", "abc"},
	}
	for _, tc := range cases {
		if got := sanitizePaneFrame(tc.in); got != tc.want {
			t.Errorf("%s: sanitizePaneFrame(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestApplyPluginPaneFrameAcceptsNewEpoch(t *testing.T) {
	a := &App{pluginPane: &PluginPaneState{}}
	ch := a.pluginPane.ChannelID
	frame := func(seq, epoch int64, body string) {
		a.applyPluginPaneFrame(protocol.PluginPaneFramePayload{ChannelID: ch, Seq: seq, Epoch: epoch, Frame: body})
	}
	frame(40, 1, "old stream")
	frame(39, 1, "stale")
	if a.pluginPane.Frame != "old stream" {
		t.Fatalf("stale frame applied: %q", a.pluginPane.Frame)
	}
	// The plugin restarted: its Seq starts over under a new epoch.
	frame(1, 2, "restarted")
	if a.pluginPane.Frame != "restarted" {
		t.Fatalf("frame from a restarted plugin was dropped; pane shows %q", a.pluginPane.Frame)
	}
	frame(2, 2, "\x1b]52;c;aGk=\x07next")
	if a.pluginPane.Frame != "next" {
		t.Fatalf("frame not sanitized on arrival: %q", a.pluginPane.Frame)
	}
}
