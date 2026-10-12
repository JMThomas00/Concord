package table_test

import (
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/table"
)

// shortAway makes walking away take a moment, for the test.
func shortAway(t *testing.T) {
	old := table.AwayFor
	table.AwayFor = 300 * time.Millisecond
	t.Cleanup(func() { table.AwayFor = old })
}

// Someone who sits down and walks off before a game starts gives the seat
// back; someone who comes back in time keeps it.
func TestWalkingAwayFreesTheSeat(t *testing.T) {
	shortAway(t)
	srv, ch, _ := startKit(t, nil, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	menu(srv, alice, 0) // alice sits as X
	srv.FrameContaining(alice, "X: alice")

	// alice steps out and back in quickly: still seated.
	time.Sleep(100 * time.Millisecond) // let frames on their way arrive first
	srv.Leave(alice)
	alice = srv.EnterAs(ch, alice.ID, "alice", 60, 12)
	time.Sleep(table.AwayFor * 2)
	srv.FrameContaining(alice, "X: alice")

	// bob sits, then goes home before anyone moves.
	bob := srv.Enter(ch, "bob", 60, 12)
	menu(srv, bob, 0)
	srv.FrameContaining(alice, "O: bob")
	srv.FrameContaining(bob, "O: bob") // nothing still on its way to bob when he goes
	time.Sleep(100 * time.Millisecond)
	srv.Leave(bob)
	deadline := time.Now().Add(5 * time.Second)
	for {
		if f := srv.NextFrame(alice); !strings.Contains(f, "O: bob") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("bob still holds his seat")
		}
	}
	carol := srv.Enter(ch, "carol", 60, 12)
	menu(srv, carol, 0) // the open seat
	srv.FrameContaining(alice, "O: carol")
}

// Walking away in the middle of a two-player game forfeits it, and frees
// the seat for the next player.
func TestWalkingAwayMidGameForfeits(t *testing.T) {
	shortAway(t)
	srv, ch, _ := startKit(t, nil, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 60, 12)
	menu(srv, alice, 0)
	bob := srv.Enter(ch, "bob", 60, 12)
	menu(srv, bob, 0)
	srv.FrameContaining(alice, "your move")
	srv.Key(alice, "5")
	srv.FrameContaining(bob, "your move")
	srv.FrameContaining(alice, "▸ O: bob")
	time.Sleep(100 * time.Millisecond)
	srv.Leave(bob)
	srv.FrameContaining(alice, "left the table")
	deadline := time.Now().Add(5 * time.Second)
	for strings.Contains(srv.NextFrame(alice), "O: bob") {
		if time.Now().After(deadline) {
			t.Fatal("bob is still seated after forfeiting")
		}
	}
}

// With more than two at the table, the computer takes the seat of someone
// who walks off mid-game, and the game goes on.
func TestWalkingAwayFromABigTableLetsTheComputerPlay(t *testing.T) {
	shortAway(t)
	srv, ch := startRelay(t, table.ModeSeats)
	alice := srv.Enter(ch, "alice", 100, 30)
	srv.FrameContaining(alice, "PRESS ENTER")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "TAKE A SEAT")
	srv.Key(alice, "down")
	srv.Key(alice, "enter")
	srv.FrameContaining(alice, "◂ 4 PLAYERS ▸")
	for _, k := range []string{"up", "up", "up", "right", "right"} { // SEAT 3: A PERSON
		srv.Key(alice, k)
	}
	srv.FrameContaining(alice, "◂ A PERSON ▸")
	for _, k := range []string{"down", "down", "down", "enter"} { // START
		srv.Key(alice, k)
	}
	srv.FrameContaining(alice, "SEATS=4")
	bob := srv.Enter(ch, "bob", 100, 30)
	srv.FrameContaining(bob, "PRESS ENTER")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "TAKE A SEAT")
	srv.Key(bob, "down")
	srv.Key(bob, "enter")
	srv.FrameContaining(bob, "SEATS=4")
	srv.Key(alice, "g")
	srv.FrameContaining(bob, "MOVES=2") // bob's turn
	time.Sleep(100 * time.Millisecond)
	srv.Leave(bob)
	// The computer takes South, goes, West goes, and it's alice's turn again.
	srv.FrameContaining(alice, "MOVES=4")
}
