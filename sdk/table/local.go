package table

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RunLocal plays the game in this terminal, without Concord: two people
// taking turns at one keyboard, one person against the computer (when
// Rules.AI is set), or two people on different computers (one hosts, the
// other joins with the host's address and a join code). options are passed
// to Rules.New (e.g. a board size). It uses the same board as the Concord
// plugin.
func RunLocal(rules Rules, options map[string]string) error {
	// The pane driver renders plugins at full color for later conversion;
	// a real terminal detects what it supports.
	lipgloss.SetDefaultRenderer(lipgloss.NewRenderer(os.Stdout))
	m := newLocal(rules, options)
	_, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if m.net != nil && m.net.peer != nil {
		_ = m.net.peer.send(netMsg{Type: "bye"})
		m.net.peer.conn.Close()
	}
	return err
}

type localModel struct {
	rules   Rules
	options map[string]string
	me      string // this player's name, shown to a network opponent

	choosing bool // start menu
	choices  []localChoice
	cursor   int

	net *netState // network game, or nil

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
	start func() tea.Cmd
}

// netState is a network game being set up or played.
type netState struct {
	hosting bool
	joining bool   // typing the host's address and code
	input   string // what's been typed
	addrs   []string
	port    int
	code    string
	peer    *netPeer
	mySeat  int
	err     string
	gone    bool // the other player left
}

// computerMoveMsg is the computer's move, worked out in the background.
type computerMoveMsg struct {
	move  string
	after int // number of moves the computer saw; stale if the game moved on
}

func newLocal(rules Rules, options map[string]string) *localModel {
	m := &localModel{rules: rules, options: options, choosing: true, me: playerName()}
	m.choices = append(m.choices, localChoice{"Two players on this computer", func() tea.Cmd {
		ps := make([]Player, len(rules.SeatNames))
		for i := range ps {
			ps[i] = Player{Name: rules.SeatNames[i]}
		}
		return m.start(ps, -1)
	}})
	if rules.AI != nil && len(rules.SeatNames) == 2 {
		for _, lv := range []struct {
			level int
			name  string
		}{{1, "easy"}, {2, "normal"}, {3, "hard"}} {
			lv := lv
			m.choices = append(m.choices, localChoice{"Play the computer (" + lv.name + ")", func() tea.Cmd {
				return m.start([]Player{{Name: m.me}, {Name: "Computer (" + lv.name + ")", Computer: true, Level: lv.level}}, 0)
			}})
		}
	}
	if len(rules.SeatNames) == 2 {
		m.choices = append(m.choices,
			localChoice{"Host a network game", func() tea.Cmd {
				m.net = &netState{hosting: true, port: DefaultPort}
				return hostCmd(DefaultPort)
			}},
			localChoice{"Join a network game", func() tea.Cmd {
				m.net = &netState{joining: true}
				return nil
			}},
		)
	}
	m.choices = append(m.choices, localChoice{"Quit", nil})
	return m
}

func playerName() string {
	for _, k := range []string{"USER", "USERNAME"} {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return "Player"
}

func (m *localModel) Init() tea.Cmd { return nil }

// start begins a game. mySeat is this player's seat, or -1 for hotseat
// (whoever's turn it is).
func (m *localModel) start(seats []Player, mySeat int) tea.Cmd {
	m.t = &Table{ID: "local", Seats: seats, Options: m.options, Resigned: -1}
	m.t.rebuild(&m.rules)
	m.seat = &Seat{Index: mySeat, table: m.t, rules: &m.rules, hotseat: mySeat < 0, play: m.playHuman}
	m.board = m.rules.NewBoard(m.seat)
	m.choosing, m.menuOpen, m.notice = false, false, ""
	m.board, _ = m.board.Update(tea.WindowSizeMsg{Width: m.width, Height: m.boardHeight()})
	return tea.Batch(m.board.Init(), m.computerTurn())
}

// playHuman is a move from a person at this computer: never for the
// computer's seat.
func (m *localModel) playHuman(seat int, move string) error {
	if seat >= 0 && seat < len(m.t.Seats) && m.t.Seats[seat].Computer {
		return fmt.Errorf("it's not your turn")
	}
	return m.play(seat, move)
}

func (m *localModel) play(seat int, move string) error {
	t := m.t
	if t.outcome().Over {
		return fmt.Errorf("this game is over")
	}
	if seat < 0 || t.game.Turn() != seat {
		return fmt.Errorf("it's not your turn")
	}
	if m.net != nil && m.net.gone {
		return fmt.Errorf("the other player has left")
	}
	if err := t.game.Play(move); err != nil {
		return err
	}
	t.Moves = append(t.Moves, move)
	if m.net != nil && m.net.peer != nil {
		if err := m.net.peer.send(netMsg{Type: "move", Move: move, Index: len(t.Moves) - 1}); err != nil {
			m.notice = "Couldn't send the move: " + err.Error()
		}
	}
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
		if err := m.play(m.t.game.Turn(), msg.move); err != nil {
			m.notice = "The computer made an illegal move (" + msg.move + "): " + err.Error()
			return m, nil
		}
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(ChangedMsg{Move: msg.move})
		return m, tea.Batch(cmd, m.computerTurn())

	case netListeningMsg:
		m.net.addrs, m.net.code = msg.addrs, msg.code
		m.net.port = portOf(msg.ln.Addr().String())
		return m, acceptCmd(msg.ln, msg.code, m.rules.Name, m.me, m.options)

	case netConnectedMsg:
		m.net.peer, m.net.mySeat, m.net.hosting, m.net.joining = msg.peer, msg.mySeat, false, false
		if msg.options != nil {
			m.options = msg.options // the host's game options
		}
		seats := []Player{{Name: m.me}, {Name: msg.opponent}}
		if msg.mySeat == 1 {
			seats[0], seats[1] = seats[1], seats[0]
		}
		return m, tea.Batch(m.start(seats, msg.mySeat), msg.peer.next())

	case netRemoteMsg:
		return m, tea.Batch(m.remote(netMsg(msg)), m.net.peer.next())

	case netErrMsg:
		if m.net == nil {
			return m, nil
		}
		if m.t != nil && m.net.peer != nil {
			m.net.gone, m.notice = true, msg.err.Error()
		} else {
			m.net.err = msg.err.Error()
		}
		return m, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		if m.choosing {
			if m.net != nil {
				return m, m.netSetupKey(msg)
			}
			return m, m.chooseKey(msg)
		}
		// M opens the menu, as in Concord; Tab does too here, where there's
		// no Concord to move focus with it.
		if k := msg.String(); (k == "m" || k == "M" || k == "tab") && !boardTyping(m.board) {
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
		return m, noQuit(cmd)
	}
	if m.board != nil {
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(msg)
		return m, cmd
	}
	return m, nil
}

// noQuit drops a board's tea.Quit: in Concord it hands the keyboard back,
// but standalone it would end the whole program (on Esc, say). Quitting
// is in the Tab menu. The wrapped command still only runs when Bubble Tea
// runs it.
func noQuit(cmd tea.Cmd) tea.Cmd {
	if cmd == nil {
		return nil
	}
	return func() tea.Msg {
		switch msg := cmd().(type) {
		case tea.QuitMsg:
			return nil
		case tea.BatchMsg:
			out := make(tea.BatchMsg, len(msg))
			for i, c := range msg {
				out[i] = noQuit(c)
			}
			return out
		default:
			return msg
		}
	}
}

func portOf(addr string) int {
	var port int
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		fmt.Sscanf(addr[i+1:], "%d", &port)
	}
	return port
}

// remote applies a message from the network opponent.
func (m *localModel) remote(msg netMsg) tea.Cmd {
	t := m.t
	if t == nil {
		return nil
	}
	theirs := 1 - m.net.mySeat
	switch msg.Type {
	case "move":
		if msg.Index != len(t.Moves) || t.game.Turn() != theirs {
			m.notice, m.net.gone = "The games got out of step; disconnected.", true
			m.net.peer.conn.Close()
			return nil
		}
		if err := t.game.Play(msg.Move); err != nil {
			m.notice, m.net.gone = "The other player sent an illegal move ("+msg.Move+"); disconnected.", true
			m.net.peer.conn.Close()
			return nil
		}
		t.Moves = append(t.Moves, msg.Move)
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(ChangedMsg{Move: msg.Move})
		return cmd
	case "resign":
		if !t.outcome().Over {
			t.Resigned = theirs
			m.board, _ = m.board.Update(ChangedMsg{})
		}
	case "rematch":
		m.rematch(false)
	case "bye":
		m.net.gone, m.notice = true, t.Seats[theirs].Name+" left the game."
	}
	return nil
}

// rematch starts a new game with the same players, sides swapped; tell
// says whether to tell the network opponent.
func (m *localModel) rematch(tell bool) {
	seats := append([]Player(nil), m.t.Seats...)
	if len(seats) == 2 {
		seats[0], seats[1] = seats[1], seats[0]
	}
	mySeat := m.seat.Index
	if m.net != nil {
		m.net.mySeat = 1 - m.net.mySeat
		mySeat = m.net.mySeat
		if tell && m.net.peer != nil {
			_ = m.net.peer.send(netMsg{Type: "rematch"})
		}
	}
	m.start(seats, mySeat)
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
		if c.start == nil {
			return tea.Quit
		}
		return c.start()
	}
	return nil
}

// netSetupKey handles keys while hosting or joining is being set up.
func (m *localModel) netSetupKey(msg tea.KeyMsg) tea.Cmd {
	n := m.net
	if msg.String() == "esc" {
		m.net = nil // (a listening host socket closes when the process ends)
		return nil
	}
	if !n.joining {
		return nil
	}
	switch msg.Type {
	case tea.KeyEnter:
		n.err = ""
		return joinCmd(n.input, m.rules.Name, m.me)
	case tea.KeyBackspace:
		if r := []rune(n.input); len(r) > 0 {
			n.input = string(r[:len(r)-1])
		}
	case tea.KeyRunes, tea.KeySpace:
		n.input += string(msg.Runes)
		if msg.Type == tea.KeySpace {
			n.input += " "
		}
	}
	return nil
}

func (m *localModel) localMenu() []menuItem {
	items := []menuItem{}
	over := m.t.outcome().Over
	if !over && (m.net == nil || !m.net.gone) {
		items = append(items, menuItem{"Resign", func() {
			seat := m.t.game.Turn()
			if m.net != nil {
				seat = m.net.mySeat
				_ = m.net.peer.send(netMsg{Type: "resign"})
			}
			if seat >= 0 {
				m.t.Resigned = seat
			}
		}})
	}
	if over && (m.net == nil || !m.net.gone) {
		items = append(items, menuItem{"Rematch", func() { m.rematch(true) }})
	}
	if m.net == nil {
		items = append(items, menuItem{"New game", func() { m.choosing, m.t, m.board = true, nil, nil }})
	}
	items = append(items, menuItem{"Quit", nil}, menuItem{"Close menu", func() {}})
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
	if m.choosing && m.net != nil {
		return m.netSetupView(accent, dim)
	}
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

	footer := dim.Render("M: menu · Ctrl+C quit")
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

func (m *localModel) netSetupView(accent, dim lipgloss.Style) string {
	n := m.net
	lines := []string{accent.Render(m.rules.Name + " — network game"), ""}
	switch {
	case n.hosting && n.code == "":
		lines = append(lines, "Opening a port…")
	case n.hosting:
		lines = append(lines, "Waiting for the other player. On their computer, choose", "\"Join a network game\" and type one of these:", "")
		for _, a := range n.addrs {
			addr := a
			if n.port != DefaultPort {
				addr = fmt.Sprintf("%s:%d", a, n.port)
			}
			lines = append(lines, "   "+accent.Render(addr+" "+n.code))
		}
		lines = append(lines, "", dim.Render(fmt.Sprintf("(Over the internet, forward TCP port %d to this computer.)", n.port)))
	case n.joining:
		lines = append(lines, "Type the host's address and join code, e.g. 192.168.1.20 K7Q2XM:", "", "   > "+n.input+"█")
	}
	if n.err != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(n.err))
	}
	lines = append(lines, "", dim.Render("Esc back · Ctrl+C quit"))
	return strings.Join(lines, "\n")
}
