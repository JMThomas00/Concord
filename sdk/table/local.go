package table

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RunLocal plays the game in this terminal, without Concord: two people
// taking turns at one keyboard, or one person against the computer (when
// Rules.AI is set). options are passed to Rules.New (e.g. a board size).
// It uses the same board as the Concord plugin.
func RunLocal(rules Rules, options map[string]string) error {
	// The pane driver renders plugins at full color for later conversion;
	// a real terminal detects what it supports.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stdout))
	_, err := tea.NewProgram(newLocal(rules, options), tea.WithAltScreen()).Run()
	return err
}

type localModel struct {
	rules   Rules
	options map[string]string

	choosing bool // start menu
	choices  []localChoice
	cursor   int

	t        *Table
	seat     *Seat
	board    tea.Model
	thinking bool
	menuOpen bool
	menuIdx  int
	notice   string

	width, height int
}

type localChoice struct {
	label string
	seats func() []Player
}

// computerMoveMsg is the computer's move, worked out in the background.
type computerMoveMsg struct {
	move  string
	after int // number of moves the computer saw; stale if the game moved on
}

func newLocal(rules Rules, options map[string]string) *localModel {
	m := &localModel{rules: rules, options: options, choosing: true}
	m.choices = append(m.choices, localChoice{"Two players on this computer", func() []Player {
		ps := make([]Player, len(rules.SeatNames))
		for i := range ps {
			ps[i] = Player{Name: rules.SeatNames[i]}
		}
		return ps
	}})
	if rules.AI != nil && len(rules.SeatNames) == 2 {
		for _, lv := range []struct {
			level int
			name  string
		}{{1, "easy"}, {2, "normal"}, {3, "hard"}} {
			lv := lv
			m.choices = append(m.choices, localChoice{"Play the computer (" + lv.name + ")", func() []Player {
				return []Player{{Name: "You"}, {Name: "Computer (" + lv.name + ")", Computer: true, Level: lv.level}}
			}})
		}
	}
	m.choices = append(m.choices, localChoice{"Quit", nil})
	return m
}

func (m *localModel) Init() tea.Cmd { return nil }

func (m *localModel) start(seats []Player) tea.Cmd {
	m.t = &Table{ID: "local", Seats: seats, Options: m.options, Resigned: -1}
	m.t.rebuild(&m.rules)
	hotseat := true
	for _, p := range seats {
		hotseat = hotseat && !p.Computer
	}
	m.seat = &Seat{Index: 0, table: m.t, rules: &m.rules, hotseat: hotseat, play: m.play}
	m.board = m.rules.NewBoard(m.seat)
	m.choosing, m.menuOpen, m.notice = false, false, ""
	m.board, _ = m.board.Update(tea.WindowSizeMsg{Width: m.width, Height: m.boardHeight()})
	return tea.Batch(m.board.Init(), m.computerTurn())
}

func (m *localModel) play(seat int, move string) error {
	t := m.t
	if t.outcome().Over {
		return fmt.Errorf("this game is over")
	}
	if seat < 0 || t.game.Turn() != seat || t.Seats[seat].Computer {
		return fmt.Errorf("it's not your turn")
	}
	if err := t.game.Play(move); err != nil {
		return err
	}
	t.Moves = append(t.Moves, move)
	return nil
}

// computerTurn starts the computer thinking if it's its move.
func (m *localModel) computerTurn() tea.Cmd {
	t := m.t
	if m.thinking || m.rules.AI == nil || t.outcome().Over {
		return nil
	}
	turn := t.game.Turn()
	if turn < 0 || !t.Seats[turn].Computer {
		return nil
	}
	m.thinking = true
	moves := append([]string(nil), t.Moves...)
	level, options, rules := t.Seats[turn].Level, t.Options, m.rules
	return func() tea.Msg {
		g := rules.New(options)
		for _, mv := range moves {
			_ = g.Play(mv)
		}
		return computerMoveMsg{move: rules.AI(g, level), after: len(moves)}
	}
}

func (m *localModel) boardHeight() int {
	if h := m.height - 2; h > 0 {
		return h
	}
	return 1
}

func (m *localModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.board != nil {
			m.board, _ = m.board.Update(tea.WindowSizeMsg{Width: m.width, Height: m.boardHeight()})
		}
		return m, nil
	case computerMoveMsg:
		m.thinking = false
		if m.t == nil || len(m.t.Moves) != msg.after {
			return m, nil
		}
		turn := m.t.game.Turn()
		if err := m.play(turn, msg.move); err != nil {
			m.notice = "The computer made an illegal move (" + msg.move + "): " + err.Error()
			return m, nil
		}
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(ChangedMsg{Move: msg.move})
		return m, tea.Batch(cmd, m.computerTurn())
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.choosing {
			return m, m.chooseKey(msg)
		}
		if msg.String() == "tab" {
			m.menuOpen, m.menuIdx = !m.menuOpen, 0
			return m, nil
		}
		if m.menuOpen {
			return m, m.menuKey(msg)
		}
		before := len(m.t.Moves)
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(msg)
		if len(m.t.Moves) != before {
			var changed tea.Cmd
			m.board, changed = m.board.Update(ChangedMsg{Move: m.t.Moves[len(m.t.Moves)-1]})
			cmd = tea.Batch(cmd, changed, m.computerTurn())
		}
		return m, cmd
	}
	if m.board != nil {
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *localModel) chooseKey(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.choices)-1 {
			m.cursor++
		}
	case "q", "esc":
		return tea.Quit
	case "enter", " ":
		c := m.choices[m.cursor]
		if c.seats == nil {
			return tea.Quit
		}
		return m.start(c.seats())
	}
	return nil
}

func (m *localModel) localMenu() []menuItem {
	items := []menuItem{}
	if !m.t.outcome().Over {
		turn := m.t.game.Turn()
		items = append(items, menuItem{"Resign", func() {
			if turn >= 0 {
				m.t.Resigned = turn
			}
		}})
	}
	items = append(items,
		menuItem{"New game", func() { m.choosing, m.t, m.board = true, nil, nil }},
		menuItem{"Quit", nil},
		menuItem{"Close menu", func() {}},
	)
	return items
}

func (m *localModel) menuKey(msg tea.KeyMsg) tea.Cmd {
	items := m.localMenu()
	switch msg.String() {
	case "left", "up", "h", "k":
		m.menuIdx = (m.menuIdx - 1 + len(items)) % len(items)
	case "right", "down", "l", "j":
		m.menuIdx = (m.menuIdx + 1) % len(items)
	case "esc":
		m.menuOpen = false
	case "enter", " ":
		it := items[m.menuIdx]
		m.menuOpen = false
		if it.do == nil {
			return tea.Quit
		}
		it.do()
		if m.board != nil {
			m.board, _ = m.board.Update(ChangedMsg{})
		}
	}
	return nil
}

func (m *localModel) View() string {
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	if m.choosing {
		lines := []string{accent.Render(m.rules.Name), ""}
		for i, c := range m.choices {
			prefix := "  "
			if i == m.cursor {
				prefix = accent.Render("▸ ")
			}
			lines = append(lines, prefix+c.label)
		}
		lines = append(lines, "", dim.Render("↑↓ choose · Enter start · q quit"))
		return strings.Join(lines, "\n")
	}

	t := m.t
	var who []string
	for i, p := range t.Seats {
		label := fmt.Sprintf("%s: %s", m.rules.SeatNames[i], p.Name)
		if !t.outcome().Over && t.game.Turn() == i {
			label = "▸ " + label
		}
		who = append(who, label)
	}
	header := strings.Join(who, "   ")
	if o := t.outcome(); o.Over {
		result := "Draw"
		if o.Winner >= 0 {
			result = t.Seats[o.Winner].Name + " wins"
		}
		if o.Reason != "" {
			result += " — " + o.Reason
		}
		header += "   " + lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render(result)
	} else if m.thinking {
		header += "   " + dim.Render("thinking…")
	}

	footer := dim.Render("Tab: menu · Ctrl+C quit")
	if m.notice != "" {
		footer = m.notice + " · " + footer
	}
	if m.menuOpen {
		var parts []string
		for i, it := range m.localMenu() {
			label := it.label
			if i == m.menuIdx {
				label = lipgloss.NewStyle().Reverse(true).Render(" " + label + " ")
			}
			parts = append(parts, label)
		}
		footer = strings.Join(parts, "  ")
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, m.board.View(), footer)
}
