package table_test

import (
	"context"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/examples/tictactoe"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/table"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// startClocked runs tic-tac-toe with a clock of base plus inc per move, at
// a seats table that alice (X) and bob (O) sit down at.
func startClocked(t *testing.T, base, inc time.Duration) (*plugintest.Server, *plugintest.Viewer, *plugintest.Viewer) {
	t.Helper()
	rules := tictactoe.Rules
	rules.Clock = func(map[string]string) (time.Duration, time.Duration) { return base, inc }
	srv := plugintest.NewServer(t)
	cfg := srv.Config()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { plugin.Run(ctx, cfg, table.New(rules).Handler()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv.WaitReady()
	ch := uuid.New()
	srv.Channel(wire.Channel{ID: ch, Name: "games", PluginConfig: map[string]string{table.SettingSeating: table.ModeSeats}})
	alice := srv.Enter(ch, "alice", 70, 12)
	menu(srv, alice, 0)
	bob := srv.Enter(ch, "bob", 70, 12)
	menu(srv, bob, 0)
	srv.FrameContaining(alice, "your move")
	return srv, alice, bob
}

func TestClockStartsWithTheFirstMoveAndFlags(t *testing.T) {
	srv, alice, bob := startClocked(t, 400*time.Millisecond, 0)
	// Nobody's clock runs before the first move.
	time.Sleep(600 * time.Millisecond)
	srv.Key(alice, "5")
	srv.FrameContaining(bob, "your move")
	// Now bob's runs out, without anyone pressing anything.
	srv.FrameContaining(alice, "alice wins — time")
	srv.Key(bob, "1") // too late
	srv.FrameContaining(bob, "bob 0:00.0")
}

func TestClockChargesEachMoveAndAddsTheIncrement(t *testing.T) {
	srv, alice, bob := startClocked(t, 30*time.Second, 5*time.Second)
	srv.Key(alice, "5") // the first move is free and earns nothing
	srv.FrameContaining(bob, "alice 0:30")
	time.Sleep(1200 * time.Millisecond)
	srv.Key(bob, "1") // about 1.2 s used, 5 s back: 33.8 s, shown rounded up
	srv.FrameContaining(alice, "bob 0:34")
}

func TestClockText(t *testing.T) {
	for d, want := range map[time.Duration]string{
		5 * time.Minute:                      "5:00",
		4*time.Minute + 58*time.Second:       "4:58",
		10*time.Second + time.Millisecond:    "0:11",
		9*time.Second + 400*time.Millisecond: "0:09.4",
		-time.Second:                         "0:00.0",
		time.Hour + 30*time.Second:           "1:00:30",
	} {
		if got := table.ClockText(d); got != want {
			t.Errorf("ClockText(%v) = %q, want %q", d, got, want)
		}
	}
}
