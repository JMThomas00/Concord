package table

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// countGame: players alternately add 1-3 to a total; whoever reaches 10 wins.
type countGame struct{ total, moves int }

func (g *countGame) Turn() int {
	if g.total >= 10 {
		return -1
	}
	return g.moves % 2
}
func (g *countGame) Play(m string) error {
	n, err := strconv.Atoi(m)
	if err != nil || n < 1 || n > 3 {
		return fmt.Errorf("add 1, 2 or 3")
	}
	g.total += n
	g.moves++
	return nil
}
func (g *countGame) Outcome() Outcome {
	if g.total >= 10 {
		return Outcome{Over: true, Winner: (g.moves - 1) % 2}
	}
	return Outcome{}
}

type countBoard struct{ seat *Seat }

func (b *countBoard) Init() tea.Cmd                       { return nil }
func (b *countBoard) Update(tea.Msg) (tea.Model, tea.Cmd) { return b, nil }
func (b *countBoard) View() string {
	return fmt.Sprintf("total %d", b.seat.Game().(*countGame).total)
}

var countRules = Rules{
	Name:      "Count",
	SeatNames: []string{"A", "B"},
	New:       func(map[string]string) Game { return &countGame{} },
	NewBoard:  func(s *Seat) tea.Model { return &countBoard{seat: s} },
}

// run executes a command synchronously, with a timeout.
func run(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	select {
	case m := <-ch:
		return m
	case <-time.After(5 * time.Second):
		t.Fatal("command didn't finish")
		return nil
	}
}

func TestNetworkPlay(t *testing.T) {
	host, guest := newLocal(countRules, map[string]string{"x": "1"}), newLocal(countRules, nil)
	host.me, guest.me = "hannah", "gus"
	host.net = &netState{hosting: true}
	listening := run(t, hostCmd(0)).(netListeningMsg)
	_, accept := host.Update(listening)
	port := host.net.port

	// The wrong code is refused; the host keeps waiting.
	accepted := make(chan tea.Msg, 1)
	go func() { accepted <- accept() }()
	bad := run(t, joinCmd(fmt.Sprintf("127.0.0.1:%d WRONG1", port), "Count", "gus"))
	if e, ok := bad.(netErrMsg); !ok || !strings.Contains(e.err.Error(), "wrong join code") {
		t.Fatalf("a wrong code got %#v", bad)
	}

	guest.net = &netState{joining: true}
	joined := run(t, joinCmd(fmt.Sprintf("127.0.0.1:%d %s", port, strings.ToLower(host.net.code)), "Count", "gus"))
	gc, ok := joined.(netConnectedMsg)
	if !ok {
		t.Fatalf("join failed: %#v", joined)
	}
	if gc.opponent != "hannah" || gc.mySeat != 1 || gc.options["x"] != "1" {
		t.Fatalf("guest connected as %+v", gc)
	}
	guest.Update(gc)
	var hc netConnectedMsg
	select {
	case m := <-accepted:
		hc = m.(netConnectedMsg)
	case <-time.After(5 * time.Second):
		t.Fatal("host never accepted")
	}
	host.Update(hc)
	if hc.opponent != "gus" || hc.mySeat != 0 {
		t.Fatalf("host connected as %+v", hc)
	}

	// The host moves; it arrives at the guest.
	if err := host.seat.Play("3"); err != nil {
		t.Fatal(err)
	}
	if err := guest.seat.Play("2"); err == nil {
		t.Fatal("the guest moved out of turn")
	}
	guest.Update(run(t, guest.net.peer.next()))
	if got := guest.t.Moves; len(got) != 1 || got[0] != "3" {
		t.Fatalf("guest's moves = %v", got)
	}
	if err := guest.seat.Play("2"); err != nil {
		t.Fatal(err)
	}
	host.Update(run(t, host.net.peer.next()))
	if host.t.game.(*countGame).total != 5 {
		t.Fatalf("host's total = %d, want 5", host.t.game.(*countGame).total)
	}

	// A move with the wrong index means the games drifted: disconnect.
	_ = guest.net.peer.send(netMsg{Type: "move", Move: "1", Index: 7})
	host.Update(run(t, host.net.peer.next()))
	if !host.net.gone || !strings.Contains(host.notice, "out of step") {
		t.Fatalf("drift wasn't caught: gone=%v notice=%q", host.net.gone, host.notice)
	}
}

// Standalone against the computer: the computer's seat refuses a person's
// move, and the computer's own moves go through.
func TestLocalComputerPlays(t *testing.T) {
	rules := countRules
	rules.AI = func(Game, int) string { return "1" }
	m := newLocal(rules, nil)
	m.start([]Player{{Name: "me"}, {Name: "Computer", Computer: true, Level: 1}}, 0)
	if err := m.seat.Play("3"); err != nil {
		t.Fatal(err)
	}
	if err := m.playHuman(1, "2"); err == nil {
		t.Fatal("a person moved for the computer")
	}
	m.Update(run(t, m.computerTurn()))
	if got := m.t.Moves; len(got) != 2 || got[1] != "1" || m.notice != "" {
		t.Fatalf("moves %v, notice %q", got, m.notice)
	}
}
