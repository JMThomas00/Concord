package server

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/log"
)

// TestInitLoggerRoutesComponentLoggersToGivenWriter is a regression test
// for a real bug: --dashboard mode's full-screen TUI (tea.WithAltScreen())
// was getting corrupted by log lines bleeding onto the same alt-screen
// buffer, because InitLogger always wrote to os.Stderr regardless of mode.
// The fix threads a writer through InitLogger -- this test proves it
// actually reaches every component logger (HubLog, AuthLog, MsgLog, DBLog,
// ClientLog, PluginLog), not just the root Logger. That distinction matters
// because Logger.With() (used to derive each component logger) copies the
// Logger struct by value, capturing whatever writer was set at that exact
// moment -- calling Logger.SetOutput() afterward would silently miss every
// already-derived component logger, which is exactly the trap a less
// careful fix (redirect after the fact, instead of at InitLogger's own
// construction) would have fallen into.
func TestInitLoggerRoutesComponentLoggersToGivenWriter(t *testing.T) {
	var buf bytes.Buffer
	InitLogger(&buf, log.InfoLevel)

	Logger.Info("root logger line")
	HubLog.Info("hub logger line")
	AuthLog.Info("auth logger line")
	MsgLog.Info("msg logger line")
	DBLog.Info("db logger line")
	ClientLog.Info("client logger line")
	PluginLog.Info("plugin logger line")

	out := buf.String()
	for _, want := range []string{
		"root logger line", "hub logger line", "auth logger line",
		"msg logger line", "db logger line", "client logger line", "plugin logger line",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected buffer to contain %q, got: %q", want, out)
		}
	}
}

// TestInitLoggerSwitchingWriterStopsOldOneReceivingOutput confirms
// re-calling InitLogger with a new writer (what runWithDashboard does
// around the TUI's lifetime: redirect before starting it, restore after)
// actually stops the previous writer from receiving anything more --
// across every component logger, not just the root. This is the exact
// mechanism the --dashboard fix depends on: a stray line landing on the
// old writer after the "redirect" would mean the alt-screen corruption
// bug is still live.
func TestInitLoggerSwitchingWriterStopsOldOneReceivingOutput(t *testing.T) {
	var first, second bytes.Buffer

	InitLogger(&first, log.InfoLevel)
	Logger.Info("goes to first")
	HubLog.Info("also goes to first")
	if first.Len() == 0 {
		t.Fatal("test setup bug: expected output before switching writers")
	}
	firstLenBeforeSwitch := first.Len()

	InitLogger(&second, log.InfoLevel)
	Logger.Info("goes to second")
	HubLog.Info("also goes to second")
	AuthLog.Info("also goes to second")

	if first.Len() != firstLenBeforeSwitch {
		t.Errorf("expected the first writer to receive nothing after switching, but it grew from %d to %d bytes", firstLenBeforeSwitch, first.Len())
	}
	if second.Len() == 0 {
		t.Error("expected the second writer to receive the post-switch log lines")
	}
}
