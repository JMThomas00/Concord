package table_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/arcade"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/table"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// relay is a game for two to four: each player in turn says "go", and after
// eight turns the one who went last wins -- with their partner across the
// table when the "partners" option is on.
type relay struct {
	n, moves int
	partners bool
	seed     string
}

func (g *relay) Turn() int {
	if g.moves >= 8 {
		return -1
	}
	return g.moves % g.n
}
func (g *relay) Play(m string) error {
	if m != "go" || g.moves >= 8 {
		return fmt.Errorf("say go")
	}
	g.moves++
	return nil
}
func (g *relay) Outcome() table.Outcome {
	if g.moves < 8 {
		return table.Outcome{}
	}
	w := (g.moves - 1) % g.n
	o := table.Outcome{Over: true, Winner: w, Reason: "last"}
	if g.partners && g.n == 4 {
		o.Team = []int{w, (w + 2) % 4}
	}
	return o
}

type relayBoard struct{ seat *table.Seat }

func (b relayBoard) Init() tea.Cmd { return nil }
func (b relayBoard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "g" {
		_ = b.seat.Play("go")
	}
	return b, nil
}
func (b relayBoard) View() string {
	g := b.seat.Game().(*relay)
	return fmt.Sprintf("SEATS=%d SEED=%s MOVES=%d", g.n, g.seed, g.moves)
}
func (b relayBoard) Draw(c *arcade.Canvas, x, y, w, h int)            { c.Text(x, y, b.View(), "fg", "", false) }
func (b relayBoard) DrawSeat(*arcade.Canvas, int, int, int, int, int) {}
func (b relayBoard) Status() string                                   { return "" }

func relayRules() table.Rules {
	return table.Rules{
		Name: "Relay", SeatNames: []string{"North", "East", "South", "West"}, MinSeats: 2, Seeded: true,
		New: func(o map[string]string) table.Game {
			n := 4
			fmt.Sscan(o[table.OptionPlayers], &n)
			return &relay{n: n, partners: o["partners"] == "yes", seed: o[table.OptionSeed]}
		},
		NewBoard: func(s *table.Seat) tea.Model { return relayBoard{s} },
		AI:       func(table.Game, int) string { return "go" },
		Arcade: &table.Arcade{
			Title: "RELAY",
			Options: []table.Option{{Key: "partners", Label: "PARTNERS", Values: []string{"no", "yes"}, Default: "no",
				Show: func(o map[string]string) bool { return o[table.OptionPlayers] == "4" }}},
		},
	}
}

func startRelay(t *testing.T, seating string) (*plugintest.Server, uuid.UUID) {
	t.Helper()
	srv := plugintest.NewServer(t)
	cfg := srv.Config()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { plugin.Run(ctx, cfg, table.New(relayRules()).Handler()); close(done) }()
	t.Cleanup(func() { cancel(); <-done })
	srv.WaitReady()
	ch := uuid.New()
	srv.Channel(wire.Channel{ID: ch, Name: "relay", PluginConfig: map[string]string{table.SettingSeating: seating}})
	return srv, ch
}

func TestManySeatsAgainstTheComputer(t *testing.T) {
	srv, ch := startRelay(t, table.ModeSeats)
	v := srv.Enter(ch, "alice", 100, 30)
	srv.FrameContaining(v, "PRESS ENTER")
	srv.Key(v, "enter")
	srv.FrameContaining(v, "1 PLAYER VS CPU")
	srv.Key(v, "enter")
	frame := srv.FrameContaining(v, "◂ 4 PLAYERS ▸")
	for _, want := range []string{"SEAT 2", "SEAT 3", "SEAT 4", "◂ CPU · NORMAL ▸"} {
		if !strings.Contains(frame, want) {
			t.Fatalf("NEW GAME is missing %q:\n%s", want, frame)
		}
	}
	// Rows: PLAYERS, SEAT 2-4, PARTNERS, START (the cursor). Three players:
	// SEAT 4 and PARTNERS go, and no seat offers a person.
	for _, k := range []string{"up", "up", "up", "up", "up", "left"} {
		srv.Key(v, k)
	}
	frame = srv.FrameContaining(v, "◂ 3 PLAYERS ▸")
	if strings.Contains(frame, "SEAT 4") || strings.Contains(frame, "PARTNERS") {
		t.Fatalf("three players shows too much:\n%s", frame)
	}
	srv.Key(v, "down")
	srv.Key(v, "right")
	srv.Key(v, "right")
	if f := srv.FrameContaining(v, "◂ CPU · HARD ▸"); strings.Contains(f, "PERSON") {
		t.Fatalf("a person seat against the computer:\n%s", f)
	}
	srv.Key(v, "down") // SEAT 3
	srv.Key(v, "down") // START
	srv.Key(v, "enter")
	frame = srv.FrameContaining(v, "SEATS=3 SEED=")
	if !strings.Contains(frame, "3 PLAYERS") || strings.Contains(frame, "SEED= ") {
		t.Fatalf("the table:\n%s", frame)
	}
}

func TestManySeatsAPersonSitsDown(t *testing.T) {
	srv, ch := startRelay(t, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 100, 30)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "TAKE A SEAT")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "◂ 4 PLAYERS ▸")
	for _, k := range []string{"up", "up", "up"} { // START → PARTNERS → SEAT 4 → SEAT 3
		srv.Key(alice, k)
	}
	srv.Key(alice, "right") // CPU · HARD
	srv.Key(alice, "right") // A PERSON
	srv.FrameContaining(alice, "◂ A PERSON ▸")
	srv.Key(alice, "down") // SEAT 4
	srv.Key(alice, "down") // PARTNERS
	srv.Key(alice, "right")
	srv.FrameContaining(alice, "◂ yes ▸")
	srv.Key(alice, "down") // START
	srv.Key(alice, "enter")
	frame := srv.FrameContaining(alice, "SEATS=4")
	if !strings.Contains(frame, "OPEN") {
		t.Fatalf("seat 3 should wait for a person:\n%s", frame)
	}
	bob := srv.Enter(ch, "bob", 100, 30)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "TAKE A SEAT")
	srv.Key(bob, "down")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "SEATS=4")
	// The table is full: alice (North) goes, the computer goes, bob goes...
	for i := 0; i < 2; i++ {
		srv.Key(alice, "g")
		srv.FrameContaining(bob, fmt.Sprintf("MOVES=%d", 4*i+2))
		srv.Key(bob, "g")
		srv.FrameContaining(alice, fmt.Sprintf("MOVES=%d", 4*i+4))
	}
	// The last to go was the computer in West; its partner (East) won with it.
	for deadline := time.Now().Add(plugintest.Timeout); !strings.Contains(alice.LastFrame, "YOU LOSE THIS ONE"); time.Sleep(20 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("alice should have lost with the others winning together:\n%s", alice.LastFrame)
		}
	}
	if strings.Contains(alice.LastFrame, "4 PLAYERS · 4 PLAYERS") {
		t.Fatalf("the top bar says it twice:\n%s", alice.LastFrame)
	}
}
