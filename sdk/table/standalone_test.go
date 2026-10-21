package table

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// keyBoard plays the count game from the keyboard: 1, 2 or 3.
type keyBoard struct{ seat *Seat }

func (b *keyBoard) Init() tea.Cmd { return nil }
func (b *keyBoard) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && strings.Contains("123", k.String()) {
		_ = b.seat.Play(k.String())
	}
	return b, nil
}
func (b *keyBoard) View() string {
	return "total " + strings.Repeat("|", b.seat.Game().(*countGame).total)
}

// countArcade is the count game behind the arcade front door, with a
// computer that always adds 1.
func countArcade() Rules {
	r := countRules
	r.NewBoard = func(s *Seat) tea.Model { return &keyBoard{seat: s} }
	r.AI = func(Game, int) string { return "1" }
	r.Arcade = &Arcade{Title: "COUNT", Tagline: "TO TEN", HowTo: []string{"Add to ten."}, Sounds: true}
	return r
}

// terminal drives a standalone game the way Bubble Tea would, with the
// background work it hands the event loop delivered on request.
type terminal struct {
	t     *testing.T
	s     *standalone
	dir   string
	posts chan func()
}

func newTerminal(t *testing.T, w, h int) *terminal {
	term := &terminal{t: t, dir: t.TempDir(), posts: make(chan func(), 10000)}
	term.s = newStandalone(countArcade(), nil, term.dir, func(fn func()) { term.posts <- fn })
	term.s.Update(tea.WindowSizeMsg{Width: w, Height: h})
	return term
}

func (term *terminal) key(k string) tea.Cmd {
	_, cmd := term.s.Update(pane.KeyMsg(wire.PluginPaneInputPayload{KeyString: k}))
	return cmd
}

func (term *terminal) screen() string { return ansi.Strip(term.s.View()) }

func (term *terminal) showing(text string) {
	term.t.Helper()
	if !strings.Contains(term.screen(), text) {
		term.t.Fatalf("the screen doesn't show %q:\n%s", text, term.screen())
	}
}

// until delivers background work (the computer's moves, ticks) until ok.
func (term *terminal) until(what string, ok func() bool) {
	term.t.Helper()
	deadline := time.After(5 * time.Second)
	for !ok() {
		select {
		case fn := <-term.posts:
			term.s.Update(postMsg(fn))
		case <-deadline:
			term.t.Fatalf("timed out waiting for %s:\n%s", what, term.screen())
		}
	}
}

func isQuitCmd(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestStandaloneOpensOnTheFrontDoor(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.showing("PRESS ENTER")
	term.key("enter")
	for _, item := range []string{"1 PLAYER VS CPU", "2 PLAYERS", "NETWORK GAME", "YOUR RECORD ON THIS COMPUTER"} {
		term.showing(item)
	}
	if strings.Contains(term.screen(), "TAKE A SEAT") {
		t.Fatalf("the menu offers a channel's seats:\n%s", term.screen())
	}
	if strings.Contains(term.screen(), "SOUND") {
		t.Fatalf("the menu offers sound, which a terminal can't play:\n%s", term.screen())
	}
}

func TestStandaloneEscLeavesFromTheTitleOnly(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.key("enter")
	if cmd := term.key("esc"); isQuitCmd(cmd) {
		t.Fatal("Esc on the menu quit; it goes back to the title")
	}
	term.showing("PRESS ENTER")
	if cmd := term.key("esc"); !isQuitCmd(cmd) {
		t.Fatal("Esc on the title didn't quit")
	}
}

// A game against the computer on the arcade's own table, and its record
// kept on disk for next time.
func TestStandalonePlaysTheComputerAndKeepsTheRecord(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.key("enter")
	term.key("enter") // 1 PLAYER VS CPU
	vm := term.s.k.viewers[localID]
	if vm.screen != screenTable {
		t.Fatalf("VS CPU didn't open a table:\n%s", term.screen())
	}
	tbl := vm.table()
	for _, mv := range []string{"3", "3", "3"} { // 3, cpu 1, 3, cpu 1, 3: eleven
		n := len(tbl.Moves)
		term.key(mv)
		if o := tbl.outcome(); o.Over {
			break
		}
		term.until("the computer's move", func() bool { return len(tbl.Moves) == n+2 })
	}
	if o := tbl.outcome(); !o.Over || o.Winner != 0 {
		t.Fatalf("the game should be won by the player: %+v, moves %v", o, tbl.Moves)
	}
	rec := term.s.k.record(localID)
	if rec.Wins != 1 || rec.Games != 1 || rec.Name == "" {
		t.Fatalf("record after a win: %+v", rec)
	}
	if _, err := os.Stat(filepath.Join(term.dir, "tables", "records.json")); err != nil {
		t.Fatalf("records weren't saved: %v", err)
	}
}

// Two players at one keyboard hand off to the plain local table, whose
// menu comes back to the arcade.
func TestStandaloneTwoPlayersAndBack(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.key("enter")
	term.key("down")
	term.key("enter") // 2 PLAYERS
	if term.s.plain == nil {
		t.Fatalf("2 PLAYERS didn't open the local table:\n%s", term.screen())
	}
	term.showing("A: A")
	term.key("1") // A's move
	term.key("2") // then B's, at the same keyboard
	if got := term.s.plain.t.Moves; strings.Join(got, ",") != "1,2" {
		t.Fatalf("moves %v, want both players' moves", got)
	}
	term.key("m")
	term.showing("Back to the menu")
	term.key("right") // Resign, [Back to the menu]
	term.key("enter")
	if term.s.plain != nil {
		t.Fatal("Back to the menu didn't leave the local table")
	}
	term.showing("1 PLAYER VS CPU")
}

// Hosting a network game opens a port and shows the join code; Esc
// closes it and comes back.
func TestStandaloneHostsANetworkGame(t *testing.T) {
	term := newTerminal(t, 80, 24)
	term.key("enter")
	term.key("down")
	term.key("down")
	cmd := term.key("enter") // NETWORK GAME, HOST
	if term.s.plain == nil || term.s.plain.net == nil || !term.s.plain.net.hosting || cmd == nil {
		t.Fatalf("hosting didn't start:\n%s", term.screen())
	}
	listening := run(t, cmd).(netListeningMsg)
	term.s.Update(listening)
	term.showing(listening.code)
	ln := term.s.plain.net.ln
	term.key("esc")
	if term.s.plain != nil {
		t.Fatal("Esc didn't leave the network game")
	}
	if _, err := ln.Accept(); err == nil {
		t.Fatal("the port is still open")
	}
	term.showing("NETWORK GAME")
}

// In a terminal too small for the arcade, Enter opens the plain local
// table's own menu, and leaving it comes back.
func TestStandaloneSmallTerminal(t *testing.T) {
	term := newTerminal(t, 50, 16)
	term.showing("Enter to play")
	term.key("enter")
	term.showing("Two players on this computer")
	term.key("q")
	if term.s.plain != nil {
		t.Fatal("q didn't leave the local menu")
	}
	term.showing("Enter to play")
}
