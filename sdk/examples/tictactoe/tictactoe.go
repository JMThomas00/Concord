// Package tictactoe is the smallest complete game built on the table kit:
// rules, a board, and a computer opponent. It's meant to be read -- start
// here when writing a new game.
package tictactoe

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"

	"github.com/JMThomas00/Concord/sdk/table"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Rules is everything the table kit needs to host tic-tac-toe.
var Rules = table.Rules{
	Name:      "Tic-tac-toe",
	SeatNames: []string{"X", "O"},
	New:       func(map[string]string) table.Game { return &Game{} },
	NewBoard:  func(s *table.Seat) tea.Model { return &Board{seat: s, cursor: 4} },
	AI:        func(g table.Game, level int) string { return bestMove(g.(*Game), level) },
}

// ── The rules ──────────────────────────────────────────────────────────────

// Game is a board of nine cells, numbered 1-9 left to right, top to bottom.
// A move is the cell's number.
type Game struct {
	cells [9]int // 0 empty, 1 X, 2 O
	moves int
}

var lines = [8][3]int{{0, 1, 2}, {3, 4, 5}, {6, 7, 8}, {0, 3, 6}, {1, 4, 7}, {2, 5, 8}, {0, 4, 8}, {2, 4, 6}}

func (g *Game) winner() int {
	for _, l := range lines {
		if c := g.cells[l[0]]; c != 0 && c == g.cells[l[1]] && c == g.cells[l[2]] {
			return c - 1
		}
	}
	return -1
}

// Turn is 0 (X) or 1 (O), or -1 once the game is over.
func (g *Game) Turn() int {
	if g.Outcome().Over {
		return -1
	}
	return g.moves % 2
}

// Play takes a cell number, "1" to "9".
func (g *Game) Play(move string) error {
	n, err := strconv.Atoi(move)
	if err != nil || n < 1 || n > 9 {
		return fmt.Errorf("pick a cell from 1 to 9")
	}
	if g.Outcome().Over {
		return fmt.Errorf("the game is over")
	}
	if g.cells[n-1] != 0 {
		return fmt.Errorf("that cell is taken")
	}
	g.cells[n-1] = g.moves%2 + 1
	g.moves++
	return nil
}

func (g *Game) Outcome() table.Outcome {
	if w := g.winner(); w >= 0 {
		return table.Outcome{Over: true, Winner: w, Reason: "three in a row"}
	}
	if g.moves == 9 {
		return table.Outcome{Over: true, Winner: -1, Reason: "board full"}
	}
	return table.Outcome{}
}

// Cell reports what's in cell i (0-8): "", "X" or "O".
func (g *Game) Cell(i int) string { return [...]string{"", "X", "O"}[g.cells[i]] }

// ── The computer ───────────────────────────────────────────────────────────

// bestMove: level 1 plays randomly, 2 wins or blocks when it can, 3 is
// perfect (minimax).
func bestMove(g *Game, level int) string {
	var free []int
	for i, c := range g.cells {
		if c == 0 {
			free = append(free, i)
		}
	}
	pick := func(i int) string { return strconv.Itoa(i + 1) }
	if level <= 1 {
		return pick(free[rand.Intn(len(free))])
	}
	if level == 2 {
		me := g.moves%2 + 1
		for _, who := range []int{me, 3 - me} { // win first, then block
			for _, i := range free {
				g.cells[i] = who
				won := g.winner() >= 0
				g.cells[i] = 0
				if won {
					return pick(i)
				}
			}
		}
		return pick(free[rand.Intn(len(free))])
	}
	best, bestScore := free[0], -2
	for _, i := range free {
		g.cells[i] = g.moves%2 + 1
		g.moves++
		score := -minimax(g)
		g.moves--
		g.cells[i] = 0
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	return pick(best)
}

// minimax scores the position for the side to move: 1 win, 0 draw, -1 loss.
func minimax(g *Game) int {
	if g.winner() >= 0 {
		return -1 // the previous move won
	}
	if g.moves == 9 {
		return 0
	}
	best := -2
	for i := range g.cells {
		if g.cells[i] != 0 {
			continue
		}
		g.cells[i] = g.moves%2 + 1
		g.moves++
		if s := -minimax(g); s > best {
			best = s
		}
		g.moves--
		g.cells[i] = 0
	}
	return best
}

// ── The board ──────────────────────────────────────────────────────────────

// Board draws the grid and lets the viewer move with the arrow keys and
// Enter (or by typing a cell number). It's a plain Bubble Tea model: it
// works the same in Concord and in a terminal.
type Board struct {
	seat   *table.Seat
	cursor int
	err    string
	width  int
	height int
}

func (b *Board) Init() tea.Cmd { return nil }

func (b *Board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
	case table.ChangedMsg:
		b.err = ""
	case tea.KeyMsg:
		switch k := msg.String(); k {
		case "up", "k":
			b.cursor = (b.cursor + 6) % 9
		case "down", "j":
			b.cursor = (b.cursor + 3) % 9
		case "left", "h":
			b.cursor = b.cursor/3*3 + (b.cursor+2)%3
		case "right", "l":
			b.cursor = b.cursor/3*3 + (b.cursor+1)%3
		case "enter", " ":
			b.move(strconv.Itoa(b.cursor + 1))
		case "q", "esc":
			return b, tea.Quit // hand the keyboard back to Concord
		default:
			if len(k) == 1 && k[0] >= '1' && k[0] <= '9' {
				b.cursor = int(k[0] - '1')
				b.move(k)
			}
		}
	}
	return b, nil
}

func (b *Board) move(cell string) {
	if !b.seat.MyTurn() {
		b.err = "not your turn"
		return
	}
	if err := b.seat.Play(cell); err != nil {
		b.err = err.Error()
	}
}

func (b *Board) View() string {
	g := b.seat.Game().(*Game)
	x := lipgloss.NewStyle().Foreground(b.seat.Color("red", lipgloss.Color("9"))).Bold(true)
	o := lipgloss.NewStyle().Foreground(b.seat.Color("cyan", lipgloss.Color("14"))).Bold(true)
	cur := lipgloss.NewStyle().Reverse(true)
	var rows []string
	for r := 0; r < 3; r++ {
		var cells []string
		for c := 0; c < 3; c++ {
			i := r*3 + c
			mark := " "
			switch g.Cell(i) {
			case "X":
				mark = x.Render("X")
			case "O":
				mark = o.Render("O")
			}
			cell := " " + mark + " "
			if i == b.cursor && b.seat.MyTurn() {
				cell = cur.Render(" " + g.Cell(i) + strings.Repeat(" ", 1-len(g.Cell(i))) + " ")
			}
			cells = append(cells, cell)
		}
		rows = append(rows, strings.Join(cells, "│"))
		if r < 2 {
			rows = append(rows, "───┼───┼───")
		}
	}
	status := ""
	switch {
	case b.err != "":
		status = b.err
	case b.seat.MyTurn():
		status = "your move — arrows + Enter, or 1-9"
	case b.seat.Index < 0 && !b.seat.Outcome().Over:
		status = "watching"
	}
	return strings.Join(append(rows, "", status), "\n")
}
