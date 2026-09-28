// Package table hosts turn-based games -- chess, checkers, Tak, anything
// with seats and moves -- as Concord channels, and as standalone terminal
// apps from the same code.
//
// A game provides the rules (Game) and a board to draw and move on (a
// Bubble Tea model built by Rules.NewBoard). The kit provides everything
// around them: seats and spectators, the channel's seating mode (sit-down
// seats, challenges, or private games), a lobby, resigning and rematches,
// computer opponents, "your turn" notifications, and saving games so they
// survive restarts and updates.
//
// In Concord:
//
//	kit := table.New(rules)
//	plugin.Run(ctx, cfg, kit.Handler())
//
// Standalone (hotseat or against the computer):
//
//	table.RunLocal(rules)
//
// Keys: Tab opens the table menu (sit, stand, resign, rematch, lobby...);
// every other key goes to the board. In a lobby, arrows and Enter choose.
package table

import (
	"time"

	"github.com/JMThomas00/Concord/sdk/pane"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// Game is one game's rules and state. Moves are strings in whatever
// notation the game likes ("e2e4", "11-15", "a1"); the kit stores the list
// of moves and rebuilds a game by replaying them, so a Game needs no
// saving code of its own.
type Game interface {
	// Turn is the seat whose move it is (0-based), or -1 once the game is over.
	Turn() int
	// Play makes a move for the seat whose turn it is, or returns an error
	// (shown to the player) if it's not a legal move.
	Play(move string) error
	// Outcome reports whether the game is over and how.
	Outcome() Outcome
}

// Outcome is how a game ended (or that it hasn't).
type Outcome struct {
	Over   bool
	Winner int    // seat that won, or -1 for a draw
	Reason string // e.g. "checkmate", "no moves left", "road", "resigned"
}

// Rules describes a game to the kit.
type Rules struct {
	// Name is the game's name, e.g. "Chess".
	Name string
	// SeatNames name the seats in order, e.g. ["White", "Black"]. Their
	// count is the number of players.
	SeatNames []string
	// New starts a game. options are the channel's settings (its
	// create_field values, e.g. a Tak board size), or the standalone
	// app's choices; ignore what you don't use.
	New func(options map[string]string) Game
	// NewBoard builds the board a viewer sees and plays on: a Bubble Tea
	// model that reads the game through seat, sends moves with seat.Play,
	// and re-renders on ChangedMsg. It receives tea.WindowSizeMsg with the
	// space it has, and every key except Tab.
	NewBoard func(seat *Seat) tea.Model
	// AI, if set, lets a computer take a seat. It returns a move for the
	// seat to move in g (a private copy -- modify it freely). level runs
	// from 1 (easy) to 3 (hard). It may take a while; it runs off the
	// main loop.
	AI func(g Game, level int) string
}

// ChangedMsg is sent to every board showing a table after its game changes
// (a move, a new game, a resignation). Move is the move just played, if any.
type ChangedMsg struct {
	Move string
}

// Player is someone -- or a computer -- in a seat.
type Player struct {
	UserID   uuid.UUID `json:"user_id,omitempty"`
	Name     string    `json:"name"`
	Computer bool      `json:"computer,omitempty"`
	Level    int       `json:"level,omitempty"` // computer strength, 1-3
}

// Empty reports whether this seat is open.
func (p Player) Empty() bool { return p.Name == "" }

// Seat is a board's handle on its table: the game, who's playing, and a
// way to move. Boards read everything through it.
type Seat struct {
	// Index is the viewer's seat at this table, or -1 when spectating. In
	// a standalone hotseat game it's the seat whose turn it is.
	Index int

	table   *Table
	rules   *Rules
	play    func(seat int, move string) error
	hotseat bool
	viewer  *pane.Viewer // nil when standalone
}

// Color is the viewer's Concord theme color by name ("red", "cyan",
// "foreground", "comment", ...), or fallback -- always fallback when
// running standalone. Use it so a board matches each viewer's theme.
func (s *Seat) Color(name string, fallback lipgloss.TerminalColor) lipgloss.TerminalColor {
	if s.viewer == nil {
		return fallback
	}
	return s.viewer.Color(name, fallback)
}

// Game is the table's current game (read it; move with Play).
func (s *Seat) Game() Game { return s.table.game }

// Players are the table's seats in order (an open seat has no Name).
func (s *Seat) Players() []Player { return s.table.Seats }

// SeatName is the name of seat i ("White"...).
func (s *Seat) SeatName(i int) string {
	if i >= 0 && i < len(s.rules.SeatNames) {
		return s.rules.SeatNames[i]
	}
	return ""
}

// Moves are the moves played so far, in order.
func (s *Seat) Moves() []string { return s.table.Moves }

// Outcome is how the game ended, including by resignation.
func (s *Seat) Outcome() Outcome { return s.table.outcome() }

// MyTurn reports whether this viewer can move now.
func (s *Seat) MyTurn() bool {
	if s.table.outcome().Over || !s.table.full() {
		return false
	}
	turn := s.table.game.Turn()
	if turn < 0 || turn >= len(s.table.Seats) || s.table.Seats[turn].Computer {
		return false
	}
	return s.hotseat || turn == s.Index
}

// Perspective is the seat to draw the board from: the viewer's own, or
// seat 0 for spectators. (Standalone hotseat games draw from the side to
// move.)
func (s *Seat) Perspective() int {
	if s.hotseat {
		if t := s.table.game.Turn(); t >= 0 {
			return t
		}
		return 0
	}
	if s.Index >= 0 {
		return s.Index
	}
	return 0
}

// Play makes a move as this viewer. It fails if it isn't their turn or the
// game rejects the move; the error is worth showing to them.
func (s *Seat) Play(move string) error {
	seat := s.Index
	if s.hotseat {
		seat = s.table.game.Turn()
	}
	return s.play(seat, move)
}

// Table is one game in progress (or finished) and its seats.
type Table struct {
	ID        string            `json:"id"`
	Seats     []Player          `json:"seats"`
	Moves     []string          `json:"moves"`
	Options   map[string]string `json:"options,omitempty"`
	Resigned  int               `json:"resigned"` // seat that resigned, or -1
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`

	game Game // rebuilt from Moves
}

func (t *Table) full() bool {
	for _, p := range t.Seats {
		if p.Empty() {
			return false
		}
	}
	return len(t.Seats) > 0
}

func (t *Table) outcome() Outcome {
	if t.Resigned >= 0 {
		winner := -1
		if len(t.Seats) == 2 {
			winner = 1 - t.Resigned
		}
		return Outcome{Over: true, Winner: winner, Reason: t.Seats[t.Resigned].Name + " resigned"}
	}
	if t.game == nil {
		return Outcome{}
	}
	return t.game.Outcome()
}

// seatOf returns userID's seat at the table, or -1.
func (t *Table) seatOf(userID uuid.UUID) int {
	for i, p := range t.Seats {
		if !p.Computer && !p.Empty() && p.UserID == userID {
			return i
		}
	}
	return -1
}

// rebuild recreates the game by replaying the moves; a move the rules now
// reject (after an update) ends the replay there.
func (t *Table) rebuild(r *Rules) {
	t.game = r.New(t.Options)
	for i, m := range t.Moves {
		if err := t.game.Play(m); err != nil {
			t.Moves = t.Moves[:i]
			return
		}
	}
}
