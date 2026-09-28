package table_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JMThomas00/Concord/sdk/examples/tictactoe"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/table"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// startKit runs tic-tac-toe as a plugin against a fake server. seating is
// the channel's seating mode ("" = default). The returned stop ends Run.
func startKit(t *testing.T, cfg *plugin.Config, seating string) (*plugintest.Server, uuid.UUID, context.CancelFunc) {
	t.Helper()
	srv := plugintest.NewServer(t)
	c := srv.Config()
	if cfg != nil {
		c.DataDir = cfg.DataDir
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go plugin.Run(ctx, c, table.New(tictactoe.Rules).Handler())
	srv.WaitReady()
	channel := uuid.New()
	settings := map[string]string{}
	if seating != "" {
		settings[table.SettingSeating] = seating
	}
	srv.Channel(wire.Channel{ID: channel, Name: "games", PluginConfig: settings})
	return srv, channel, cancel
}

// menu opens the table menu and picks the nth item (0-based).
func menu(srv *plugintest.Server, v *plugintest.Viewer, n int) {
	srv.Key(v, "tab")
	for i := 0; i < n; i++ {
		srv.Key(v, "right")
	}
	srv.Key(v, "enter")
}

func TestSeatsModeSitPlayAndSpectate(t *testing.T) {
	srv, ch, _ := startKit(t, nil, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	srv.FrameContaining(alice, "Tab: sit down")

	menu(srv, alice, 0) // Sit as X
	srv.FrameContaining(alice, "X: alice")

	bob := srv.Enter(ch, "bob", 60, 12)
	srv.FrameContaining(bob, "X: alice")
	menu(srv, bob, 0) // the only open seat: Sit as O
	srv.FrameContaining(alice, "your move")

	carol := srv.Enter(ch, "carol", 60, 12)
	srv.FrameContaining(carol, "Spectating")
	srv.Key(carol, "5") // spectators can't move
	srv.Key(alice, "5")
	frame := srv.FrameContaining(bob, "your move")
	if !strings.Contains(frame, "X") {
		t.Fatalf("bob's board doesn't show alice's X:\n%s", frame)
	}
	srv.FrameContaining(carol, "▸ O: bob")

	// alice can't move twice.
	srv.Key(alice, "1")
	srv.FrameContaining(alice, "not your turn")
}

func TestComputerTakesTheOpenSeatAndPlays(t *testing.T) {
	srv, ch, _ := startKit(t, nil, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	srv.FrameContaining(alice, "Tab: sit down")
	menu(srv, alice, 0) // Sit as X
	srv.FrameContaining(alice, "X: alice")
	menu(srv, alice, 0) // Computer plays O
	srv.FrameContaining(alice, "O: Computer")

	srv.Key(alice, "1")
	srv.FrameContaining(alice, "your move") // the computer answered, so it's alice's move again
	frame := alice.LastFrame
	if strings.Count(frame, "O") < 2 { // "O: Computer" in the header plus its mark on the board
		t.Fatalf("no computer move on the board:\n%s", frame)
	}
}

func TestResignAndRematchSwapsSides(t *testing.T) {
	srv, ch, _ := startKit(t, nil, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	bob := srv.Enter(ch, "bob", 60, 12)
	srv.FrameContaining(alice, "Tab: sit down")
	srv.FrameContaining(bob, "Tab: sit down")
	menu(srv, alice, 0)
	srv.FrameContaining(bob, "X: alice")
	menu(srv, bob, 0)
	srv.FrameContaining(alice, "your move")

	menu(srv, alice, 0) // Resign
	srv.FrameContaining(bob, "bob wins — alice resigned")
	menu(srv, bob, 1) // Stand up, [Rematch], Close
	srv.FrameContaining(alice, "X: bob")
}

func TestChallengeModeFlow(t *testing.T) {
	srv, ch, _ := startKit(t, nil, table.ModeChallenge)
	alice := srv.Enter(ch, "alice", 70, 14)
	srv.FrameContaining(alice, "lobby")
	srv.Key(alice, "enter") // "+ Challenge someone…" (first item: nothing else yet)

	req := srv.NextEvent()
	if req.Kind != wire.PluginEventMembers {
		t.Fatalf("expected a members lookup, got %+v", req)
	}
	bobID := uuid.New()
	srv.AnswerMembers(req, []wire.PluginMember{
		{UserID: alice.ID, Username: "alice", DisplayName: "alice", Online: true},
		{UserID: bobID, Username: "bob", DisplayName: "bob", Online: true},
	})
	srv.FrameContaining(alice, "● bob")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "Challenge sent to bob")

	note := srv.NextEvent()
	var p wire.PluginNotifyUserPayload
	_ = json.Unmarshal(note.Payload, &p)
	if note.Kind != wire.PluginEventNotifyUser || p.UserID != bobID || !strings.Contains(p.Content, "alice challenged you") {
		t.Fatalf("bob wasn't notified: %+v %+v", note, p)
	}

	// bob opens the channel (same user ID the challenge named) and accepts.
	bob := srv.EnterAs(ch, bobID, "bob", 70, 14)
	srv.FrameContaining(bob, "alice challenged you")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "X: alice")
	accepted := srv.NextEvent()
	if !strings.Contains(string(accepted.Payload), "bob accepted") {
		t.Fatalf("alice wasn't told: %+v", accepted)
	}
	srv.FrameContaining(alice, "your move") // the new game is listed for alice
}

func TestPrivateGamesAreOnlyVisibleToTheirPlayers(t *testing.T) {
	srv, ch, _ := startKit(t, nil, table.ModePrivate)
	alice := srv.Enter(ch, "alice", 70, 14)
	srv.FrameContaining(alice, "your games")
	srv.Key(alice, "enter") // New game with…
	req := srv.NextEvent()
	bobID := uuid.New()
	srv.AnswerMembers(req, []wire.PluginMember{{UserID: bobID, Username: "bob", DisplayName: "bob", Online: false}})
	srv.FrameContaining(alice, "○ bob")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "X: alice")
	if e := srv.NextEvent(); !strings.Contains(string(e.Payload), "started a game") {
		t.Fatalf("bob wasn't told about the new game: %+v", e)
	}

	srv.Key(alice, "5")
	if e := srv.NextEvent(); !strings.Contains(string(e.Payload), "your turn") {
		t.Fatalf("bob (not watching) wasn't told it's his turn: %+v", e)
	}

	carol := srv.Enter(ch, "carol", 70, 14)
	frame := srv.FrameContaining(carol, "your games")
	if strings.Contains(frame, "alice") {
		t.Fatalf("carol can see alice and bob's private game:\n%s", frame)
	}
}

func TestGamesSurviveARestart(t *testing.T) {
	srv, _, stop := startKit(t, nil, table.ModeSeats)
	cfg := srv.Config()
	stop()

	// Run again with a known data folder, play, then restart on it.
	srv, ch, stop := startKit(t, &cfg, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	srv.FrameContaining(alice, "Tab: sit down")
	menu(srv, alice, 0)
	srv.FrameContaining(alice, "X: alice")
	stop()

	srv2 := plugintest.NewServer(t)
	c2 := srv2.Config()
	c2.DataDir = cfg.DataDir
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go plugin.Run(ctx, c2, table.New(tictactoe.Rules).Handler())
	srv2.WaitReady()
	srv2.Channel(wire.Channel{ID: ch, Name: "games", PluginConfig: map[string]string{table.SettingSeating: table.ModeSeats}})
	bob := srv2.Enter(ch, "bob", 60, 12)
	srv2.FrameContaining(bob, "X: alice")
}
