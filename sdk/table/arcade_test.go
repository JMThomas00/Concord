package table_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/arcade"
	"github.com/JMThomas00/Concord/sdk/examples/tictactoe"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/table"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// arcadeRules is the example tic-tac-toe behind the arcade front door,
// with one starter and one set to unlock. Its board doesn't draw on the
// arcade canvas, so games use the plain table view.
func arcadeRules() table.Rules {
	r := tictactoe.Rules
	r.Arcade = &table.Arcade{
		Title: "TIC-TAC-TOE", Tagline: "THREE IN A ROW",
		HowTo:      []string{"Three in a row wins."},
		Reward:     "GOLD STAR",
		Collection: "SETS",
		Unlockables: []arcade.Unlockable{
			{ID: "plain", Name: "PLAIN", Tier: arcade.Starter, Kind: "pieces"},
			{ID: "fancy", Name: "FANCY", Tier: arcade.Common, Kind: "pieces", Blurb: "Very fancy."},
		},
		Kinds: []table.Kind{{ID: "pieces", Label: "PIECES"}},
	}
	return r
}

func startArcade(t *testing.T, seating string) (*plugintest.Server, uuid.UUID) {
	t.Helper()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cfg := srv.Config() // before the cleanup below, so the plugin stops before its data folder goes
	go func() { plugin.Run(ctx, cfg, table.New(arcadeRules()).Handler()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv.WaitReady()
	channel := uuid.New()
	srv.Channel(wire.Channel{ID: channel, Name: "games", PluginConfig: map[string]string{table.SettingSeating: seating}})
	return srv, channel
}

func claims(srv *plugintest.Server, v *plugintest.Viewer) string {
	return strings.Join(srv.Claimed(v), ",")
}

// The front door, a game, a Gold Star, and spending it.
func TestArcadeFrontDoorAndRewards(t *testing.T) {
	srv, ch := startArcade(t, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 80, 24)
	bob := srv.Enter(ch, "bob", 80, 24)
	srv.FrameContaining(alice, "PRESS ENTER")
	if c := claims(srv, alice); c != "" {
		t.Fatalf("the title claims %q: Esc must leave the pane there", c)
	}
	for _, v := range []*plugintest.Viewer{alice, bob} {
		srv.FrameContaining(v, "PRESS ENTER")
		srv.Key(v, "enter")
		srv.FrameContaining(v, "TAKE A SEAT")
		srv.Key(v, "down")
		srv.Key(v, "enter") // sits in the first open seat
	}
	srv.FrameContaining(alice, "O: bob")
	if c := claims(srv, alice); c != wire.PaneKeyEsc {
		t.Fatalf("the table claims %q: Esc goes back to the menu", c)
	}
	for i, mv := range []string{"1", "4", "2", "5", "3"} {
		v := alice
		if i%2 == 1 {
			v = bob
		}
		srv.Key(v, mv)
	}
	srv.FrameContaining(alice, "alice wins")

	// First win: a new achievement, so a Gold Star.
	srv.Key(alice, "esc")
	srv.FrameContaining(alice, "★ 1 GOLD STAR · NEW SETS!")
	srv.Key(alice, "down")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "◂ PLAIN ▸")
	srv.Key(alice, "right")
	srv.FrameContaining(alice, "? ? ?")
	srv.Key(alice, "enter")
	frame := srv.FrameContaining(alice, "SPENDS 1 GOLD STAR")
	if !strings.Contains(frame, "FANCY") || !strings.Contains(frame, "Very fancy.") {
		t.Fatalf("the offer:\n%s", frame)
	}
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "FANCY IS YOURS.")
	frame = srv.FrameContaining(alice, "✓ IN USE") // the reveal moves on by itself
	if !strings.Contains(frame, "◂ FANCY ▸") || !strings.Contains(frame, "2 OF 2") {
		t.Fatalf("after the draft:\n%s", frame)
	}

	// The Hall of Fame has alice first.
	srv.Key(alice, "esc")
	srv.Key(alice, "down")
	srv.Key(alice, "down")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	frame = srv.FrameContaining(alice, "RANK")
	if !strings.Contains(frame, "1ST") || !strings.Contains(frame, "ALICE") {
		t.Fatalf("hall of fame:\n%s", frame)
	}
}

// 1 PLAYER VS CPU in a seats channel is a game of your own: the channel's
// table stays free for others.
func TestArcadeComputerGameIsSeparate(t *testing.T) {
	srv, ch := startArcade(t, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 80, 24)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "1 PLAYER VS CPU")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "O: Computer")

	bob := srv.Enter(ch, "bob", 80, 24)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "X OPEN · O OPEN")

	// Back in the menu, alice can pick her game up again.
	srv.Key(alice, "1")
	srv.Key(alice, "esc")
	srv.FrameContaining(alice, "RESUME · MOVE 3")
}

// In a challenge channel the menu has 2 PLAYERS and WATCH, and an
// invitation shows on WATCH.
func TestArcadeChallengeMenu(t *testing.T) {
	srv, ch := startArcade(t, table.ModeChallenge)
	alice := srv.Enter(ch, "alice", 80, 24)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	frame := srv.FrameContaining(alice, "2 PLAYERS")
	if !strings.Contains(frame, "WATCH") || strings.Contains(frame, "TAKE A SEAT") {
		t.Fatalf("challenge menu:\n%s", frame)
	}
	srv.Key(alice, "down")
	srv.Key(alice, "enter") // 2 PLAYERS: the picker asks Concord who's here
	req := srv.NextEvent()
	for req.Kind != wire.PluginEventMembers {
		req = srv.NextEvent()
	}
	bobID := srv.UserID("bob")
	srv.AnswerMembers(req, []wire.PluginMember{
		{UserID: alice.ID, DisplayName: "alice", Online: true},
		{UserID: bobID, DisplayName: "bob", Online: true},
	})
	srv.FrameContaining(alice, "[ CHALLENGE ]")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "Challenge sent to bob.")

	bob := srv.EnterAs(ch, bobID, "bob", 80, 24)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "★ 1 CHALLENGE FOR YOU!")
}

// A pane too small for the arcade gets a plain front door.
func TestArcadeSmallPane(t *testing.T) {
	srv, ch := startArcade(t, table.ModeSeats)
	v := srv.Enter(ch, "carol", 50, 14)
	srv.FrameContaining(v, "Enter to play")
	srv.Key(v, "enter")
	srv.FrameContaining(v, "M: sit down")
	srv.Resize(v, 80, 24) // bigger now: the table stays, drawn plainly
	for deadline := time.Now().Add(plugintest.Timeout); claims(srv, v) != wire.PaneKeyEsc; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("Esc isn't claimed on the table in a big pane")
		}
	}
	srv.Key(v, "esc")
	srv.FrameContaining(v, "TAKE A SEAT")
}

// Options are each player's own, and saved.
func TestArcadeOptions(t *testing.T) {
	srv, ch := startArcade(t, table.ModeSeats)
	v := srv.Enter(ch, "alice", 80, 24)
	srv.FrameContaining(v, "PRESS ENTER")
	srv.Key(v, "enter")
	srv.FrameContaining(v, "EFFECTS FULL")
	for _, k := range []string{"up", "enter"} { // OPTIONS is last
		srv.Key(v, k)
	}
	srv.FrameContaining(v, "◂ FULL ▸")
	srv.Key(v, "right")
	srv.FrameContaining(v, "◂ CALM ▸")
	srv.Key(v, "esc")
	srv.FrameContaining(v, "EFFECTS CALM")
}

// stubBoard is the example board with an arcade face that shows what the
// kit gives it: the viewer's pieces, and a callout for a moment after each
// move (an Animator).
type stubBoard struct {
	tea.Model
	seat  *table.Seat
	moved time.Time
}

func (b *stubBoard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(table.ChangedMsg); ok {
		b.moved = time.Now()
	}
	var cmd tea.Cmd
	b.Model, cmd = b.Model.Update(msg)
	return b, cmd
}
func (b *stubBoard) Draw(c *arcade.Canvas, x, y, w, h int) {
	c.Text(x, y, "PIECES="+b.seat.Equipped("pieces"), "fg", "", false)
	if b.Animating() {
		c.Text(x, y+1, "CALLOUT", "yellow", "", true)
	}
}
func (b *stubBoard) DrawSeat(*arcade.Canvas, int, int, int, int, int) {}
func (b *stubBoard) Status() string                                   { return "" }
func (b *stubBoard) Animating() bool {
	return b.seat.Effects() == "" && time.Since(b.moved) < 600*time.Millisecond
}

func TestArcadeBoardGetsChoicesAndAnimates(t *testing.T) {
	rules := arcadeRules()
	plain := rules.NewBoard
	rules.NewBoard = func(s *table.Seat) tea.Model { return &stubBoard{Model: plain(s), seat: s} }
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cfg := srv.Config() // before the cleanup below, so the plugin stops before its data folder goes
	go func() { plugin.Run(ctx, cfg, table.New(rules).Handler()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv.WaitReady()
	ch := uuid.New()
	srv.Channel(wire.Channel{ID: ch, Name: "games", PluginConfig: map[string]string{table.SettingSeating: table.ModeSeats}})

	alice, bob := srv.Enter(ch, "alice", 80, 24), srv.Enter(ch, "bob", 80, 24)
	for _, v := range []*plugintest.Viewer{alice, bob} {
		srv.FrameContaining(v, "PRESS ENTER")
		srv.Key(v, "enter")
		srv.FrameContaining(v, "TAKE A SEAT")
		srv.Key(v, "down")
		srv.Key(v, "enter")
	}
	srv.FrameContaining(alice, "PIECES=plain")
	srv.FrameContaining(bob, "PIECES=plain")

	// A move: bob's board animates, then a last frame clears the callout.
	srv.Key(alice, "1")
	srv.FrameContaining(bob, "CALLOUT")
	for deadline := time.Now().Add(plugintest.Timeout); ; {
		if f := srv.NextFrame(bob); !strings.Contains(f, "CALLOUT") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the callout never cleared")
		}
	}

	// Win, unlock FANCY, and the board shows it.
	for i, mv := range []string{"4", "2", "5", "3"} {
		v := bob
		if i%2 == 1 {
			v = alice
		}
		srv.Key(v, mv)
	}
	srv.FrameContaining(alice, "YOU WIN!")
	srv.Key(alice, "esc")
	srv.FrameContaining(alice, "NEW SETS!")
	for _, k := range []string{"down", "down", "enter", "right", "enter"} {
		srv.Key(alice, k)
	}
	srv.FrameContaining(alice, "SPENDS 1 GOLD STAR")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "✓ IN USE")
	srv.Key(alice, "esc")
	srv.FrameContaining(alice, "TAKE A SEAT")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "PIECES=fancy")
}

// panelBoard is stubBoard with a panel of its own.
type panelBoard struct{ *stubBoard }

func (b panelBoard) DrawPanel(c *arcade.Canvas, x, y, w, h int) {
	c.Text(x, y, "MY PANEL", "fg", "", false)
}

func (b panelBoard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	_, cmd := b.stubBoard.Update(msg)
	return b, cmd // stay wrapped
}

// optionsRules is the stub arcade game with an option chosen per game.
func optionsRules() table.Rules {
	rules := arcadeRules()
	plain := rules.NewBoard
	rules.NewBoard = func(s *table.Seat) tea.Model { return panelBoard{&stubBoard{Model: plain(s), seat: s}} }
	rules.Arcade.Options = []table.Option{{Key: "size", Label: "BOARD", Values: []string{"3", "4"}, Names: []string{"3x3", "4x4"}, Default: "3",
		Describe: func(v string) string { return v + " IN A ROW" }}}
	return rules
}

func startOptions(t *testing.T, seating string) (*plugintest.Server, uuid.UUID) {
	t.Helper()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	cfg := srv.Config() // before the cleanup below, so the plugin stops before its data folder goes
	go func() { plugin.Run(ctx, cfg, table.New(optionsRules()).Handler()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv.WaitReady()
	ch := uuid.New()
	srv.Channel(wire.Channel{ID: ch, Name: "games", PluginConfig: map[string]string{table.SettingSeating: seating}})
	return srv, ch
}

// Each new game's options are chosen on NEW GAME, remembered for next time,
// and shown with the game; a board's panel replaces the score.
func TestNewGameOptions(t *testing.T) {
	srv, ch := startOptions(t, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 80, 24)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "1 PLAYER VS CPU")
	srv.Key(alice, "enter")
	frame := srv.FrameContaining(alice, "◂ 3x3 ▸")
	if !strings.Contains(frame, "3 IN A ROW") || !strings.Contains(frame, "VS CPU · NORMAL") {
		t.Fatalf("NEW GAME:\n%s", frame)
	}
	srv.Key(alice, "up") // START -> BOARD
	srv.Key(alice, "right")
	srv.FrameContaining(alice, "◂ 4x4 ▸")
	srv.Key(alice, "enter")
	frame = srv.FrameContaining(alice, "VS CPU · NORMAL · 4x4")
	if !strings.Contains(frame, "MY PANEL") || strings.Contains(frame, "SCORE") {
		t.Fatalf("the board's panel should replace the score:\n%s", frame)
	}

	// A seats table: the first to sit chooses (remembered: 4x4)...
	srv.Key(alice, "esc")
	srv.FrameContaining(alice, "TAKE A SEAT")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	if f := srv.FrameContaining(alice, "TAKE THE X SEAT"); !strings.Contains(f, "◂ 4x4 ▸") {
		t.Fatalf("the last choice wasn't remembered:\n%s", f)
	}
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "MY PANEL")
	// ...and the second just sits.
	bob := srv.Enter(ch, "bob", 80, 24)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "TAKE A SEAT")
	srv.Key(bob, "down")
	srv.Key(bob, "enter")
	frame = srv.FrameContaining(bob, "MY PANEL")
	if !strings.Contains(frame, "VS ALICE · 4x4") {
		t.Fatalf("bob's table:\n%s", frame)
	}
}

// A challenge carries the challenger's choices, and the invitation says so.
func TestChallengeCarriesOptions(t *testing.T) {
	srv, ch := startOptions(t, table.ModeChallenge)
	alice := srv.Enter(ch, "alice", 80, 24)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "2 PLAYERS")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	req := srv.NextEvent()
	for req.Kind != wire.PluginEventMembers {
		req = srv.NextEvent()
	}
	bobID := srv.UserID("bob")
	srv.AnswerMembers(req, []wire.PluginMember{{UserID: bobID, DisplayName: "bob", Online: true}})
	srv.FrameContaining(alice, "[ CHALLENGE ]")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "CHALLENGE BOB")
	srv.Key(alice, "up")
	srv.Key(alice, "right")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "(4X4)") // the lobby is in capitals

	bob := srv.EnterAs(ch, bobID, "bob", 80, 24)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.Key(bob, "down")
	srv.Key(bob, "down")
	srv.Key(bob, "enter") // WATCH: the invitation
	srv.FrameContaining(bob, "CHALLENGED YOU (4X4)")
	srv.Key(bob, "enter") // accept
	srv.FrameContaining(bob, "VS ALICE · 4x4")
}
