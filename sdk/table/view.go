package table

import (
	"fmt"
	"sort"
	"strings"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

type screen int

const (
	screenLobby  screen = iota // challenge / private modes: games and invitations
	screenPicker               // choosing an opponent
	screenTable                // a board
)

// membersMsg delivers a members lookup to the viewer who asked.
type membersMsg struct{ members []wire.PluginMember }

// viewerModel is one viewer's screen: the lobby or a table.
type viewerModel struct {
	k       *Kit
	v       *pane.Viewer
	channel uuid.UUID

	screen  screen
	tableID string
	board   tea.Model
	seat    *Seat

	cursor   int // lobby / picker selection
	menuOpen bool
	menuIdx  int
	members  []wire.PluginMember
	notice   string
	pending  tea.Cmd // e.g. a newly opened board's Init, returned with the next Update

	width, height int
}

func (k *Kit) newViewer(v *pane.Viewer) tea.Model {
	m := &viewerModel{k: k, v: v, channel: v.ChannelID, width: v.Width, height: v.Height}
	k.viewers[v.ID] = m
	r := k.room(v.ChannelID)
	if r.mode() == ModeSeats {
		m.openTable(k.seatsTable(r))
	}
	return m
}

func (m *viewerModel) me() Player {
	return Player{UserID: m.v.ID, Name: m.v.DisplayName}
}

func (m *viewerModel) room() *Room { return m.k.room(m.channel) }

func (m *viewerModel) table() *Table {
	if m.screen != screenTable {
		return nil
	}
	return m.room().table(m.tableID)
}

// openTable shows table t with a fresh board.
func (m *viewerModel) openTable(t *Table) {
	r := m.room()
	m.screen, m.tableID, m.menuOpen, m.notice = screenTable, t.ID, false, ""
	m.seat = &Seat{Index: t.seatOf(m.v.ID), table: t, rules: &m.k.rules, viewer: m.v,
		play: func(seat int, move string) error {
			// Always the table as it is now (seats may have changed).
			if cur := r.table(t.ID); cur != nil {
				return m.k.play(r, cur, seat, move)
			}
			return fmt.Errorf("that game is gone")
		}}
	m.board = m.k.rules.NewBoard(m.seat)
	// The board's Init runs through the pane driver (which knows which
	// commands are safe to run), after the current key or event.
	m.pending = tea.Batch(m.pending, m.board.Init())
	m.board, _ = m.board.Update(tea.WindowSizeMsg{Width: m.width, Height: m.boardHeight()})
}

func (m *viewerModel) boardHeight() int {
	h := m.height - 2 // header + footer
	if h < 1 {
		h = 1
	}
	return h
}

func (m *viewerModel) Init() tea.Cmd { return m.takePending() }

func (m *viewerModel) takePending() tea.Cmd {
	cmd := m.pending
	m.pending = nil
	return cmd
}

func (m *viewerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.board != nil {
			m.board, _ = m.board.Update(tea.WindowSizeMsg{Width: m.width, Height: m.boardHeight()})
		}
	case roomChangedMsg:
		t := m.table()
		if m.screen == screenTable && t == nil {
			m.screen, m.notice = screenLobby, "That game has ended."
		}
		if t != nil {
			m.seat.table, m.seat.Index = t, t.seatOf(m.v.ID)
			if move, ok := msg.moved[t.ID]; ok {
				m.board, _ = m.board.Update(ChangedMsg{Move: move})
			}
		}
	case membersMsg:
		m.members = msg.members
	case tea.KeyMsg:
		cmd := m.key(msg)
		return m, tea.Batch(cmd, m.takePending())
	}
	return m, m.takePending()
}

func (m *viewerModel) key(msg tea.KeyMsg) tea.Cmd {
	switch m.screen {
	case screenTable:
		if msg.String() == "tab" {
			m.menuOpen, m.menuIdx = !m.menuOpen, 0
			return nil
		}
		if m.menuOpen {
			return m.menuKey(msg)
		}
		var cmd tea.Cmd
		m.board, cmd = m.board.Update(msg) // a board's tea.Quit hands the keyboard back
		return cmd
	case screenPicker:
		return m.pickerKey(msg)
	default:
		return m.lobbyKey(msg)
	}
}

// ── Table menu ──────────────────────────────────────────────────────────────

type menuItem struct {
	label string
	do    func()
}

func (m *viewerModel) menu() []menuItem {
	r, t, k := m.room(), m.table(), m.k
	if t == nil {
		return nil
	}
	me := m.me()
	mySeat := t.seatOf(me.UserID)
	over := t.outcome().Over
	var items []menuItem
	if r.mode() == ModeSeats {
		if mySeat < 0 && (!t.full() || over) {
			for i, p := range t.Seats {
				if p.Empty() {
					i := i
					items = append(items, menuItem{"Sit as " + k.rules.SeatNames[i], func() { k.sit(r, t, i, me) }})
				}
			}
		}
		if mySeat >= 0 && !t.full() && k.computerAllowed(r) {
			for i, p := range t.Seats {
				if p.Empty() {
					i := i
					items = append(items, menuItem{"Computer plays " + k.rules.SeatNames[i], func() { k.sit(r, t, i, k.computerPlayer(2)) }})
				}
			}
		}
		if mySeat >= 0 && (!t.full() || over) { // once a game starts, leaving means resigning
			items = append(items, menuItem{"Stand up", func() { k.stand(r, t, me.UserID) }})
		}
	}
	if mySeat >= 0 && t.full() && !over {
		items = append(items, menuItem{"Resign", func() { k.resign(r, t, mySeat) }})
	}
	if over && mySeat >= 0 && t.full() {
		items = append(items, menuItem{"Rematch", func() { k.rematch(r, t) }})
	}
	if r.mode() != ModeSeats {
		items = append(items, menuItem{"Back to lobby", func() { m.screen, m.cursor = screenLobby, 0 }})
	}
	items = append(items, menuItem{"Close menu", func() {}})
	return items
}

func (m *viewerModel) menuKey(msg tea.KeyMsg) tea.Cmd {
	items := m.menu()
	switch msg.String() {
	case "left", "up", "shift+tab", "h", "k":
		m.menuIdx = (m.menuIdx - 1 + len(items)) % len(items)
	case "right", "down", "l", "j":
		m.menuIdx = (m.menuIdx + 1) % len(items)
	case "esc":
		m.menuOpen = false
	case "enter", " ":
		if m.menuIdx < len(items) {
			items[m.menuIdx].do()
		}
		m.menuOpen = false
	}
	return nil
}

// ── Lobby (challenge / private modes) ───────────────────────────────────────

type lobbyItem struct {
	label   string
	open    func()
	decline func() // incoming challenges only
}

func (m *viewerModel) lobby() []lobbyItem {
	r, k, me := m.room(), m.k, m.me()
	var items []lobbyItem
	for _, c := range r.Challenges {
		c := c
		switch {
		case c.To.UserID == me.UserID:
			items = append(items, lobbyItem{
				label: "★ " + c.From.Name + " challenged you — Enter to accept, d to decline",
				open: func() {
					if t := k.answer(r, c.ID, true); t != nil {
						m.openTable(t)
					}
				},
				decline: func() { k.answer(r, c.ID, false) },
			})
		case c.From.UserID == me.UserID:
			items = append(items, lobbyItem{label: "… waiting for " + c.To.Name + " — Enter to cancel", open: func() { k.answer(r, c.ID, false) }})
		}
	}
	tables := append([]*Table(nil), r.Tables...)
	sort.SliceStable(tables, func(i, j int) bool {
		mi, mj := tables[i].seatOf(me.UserID) >= 0, tables[j].seatOf(me.UserID) >= 0
		if mi != mj {
			return mi
		}
		return tables[i].UpdatedAt.After(tables[j].UpdatedAt)
	})
	for _, t := range tables {
		t := t
		mine := t.seatOf(me.UserID) >= 0
		if !mine && !r.spectators() {
			continue
		}
		items = append(items, lobbyItem{label: m.tableLine(t, mine), open: func() { m.openTable(t) }})
	}
	verb := "Challenge someone…"
	if r.mode() == ModePrivate {
		verb = "New game with…"
	}
	items = append(items, lobbyItem{label: "+ " + verb, open: func() {
		m.screen, m.cursor, m.members = screenPicker, 0, nil
		id := m.v.ID
		k.requestMembers(m.channel, func(members []wire.PluginMember) { k.host.Send(id, membersMsg{members}) })
	}})
	if k.computerAllowed(r) {
		items = append(items, lobbyItem{label: "+ Play the computer", open: func() { m.openTable(k.startGame(r, me, k.computerPlayer(2))) }})
	}
	return items
}

func (m *viewerModel) tableLine(t *Table, mine bool) string {
	names := make([]string, len(t.Seats))
	for i, p := range t.Seats {
		names[i] = p.Name
		if p.Empty() {
			names[i] = "(open)"
		}
	}
	status := fmt.Sprintf("move %d", len(t.Moves)+1)
	if o := t.outcome(); o.Over {
		status = "finished"
	} else if mine && t.full() && t.game.Turn() == t.seatOf(m.v.ID) {
		status = "your move"
	}
	return fmt.Sprintf("%s — %s", strings.Join(names, " vs "), status)
}

func (m *viewerModel) lobbyKey(msg tea.KeyMsg) tea.Cmd {
	items := m.lobby()
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(items)-1 {
			m.cursor++
		}
	case "enter":
		if m.cursor < len(items) {
			items[m.cursor].open()
		}
	case "d":
		if m.cursor < len(items) && items[m.cursor].decline != nil {
			items[m.cursor].decline()
		}
	case "q", "esc":
		return tea.Quit
	}
	return nil
}

func (m *viewerModel) pickable() []wire.PluginMember {
	var out []wire.PluginMember
	for _, p := range m.members {
		if p.UserID != m.v.ID {
			out = append(out, p)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Online && !out[j].Online })
	return out
}

func (m *viewerModel) pickerKey(msg tea.KeyMsg) tea.Cmd {
	people := m.pickable()
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(people)-1 {
			m.cursor++
		}
	case "esc":
		m.screen, m.cursor = screenLobby, 0
	case "enter":
		if m.cursor >= len(people) {
			return nil
		}
		p := people[m.cursor]
		them := Player{UserID: p.UserID, Name: p.DisplayName}
		r := m.room()
		if r.mode() == ModePrivate {
			m.openTable(m.k.startGame(r, m.me(), them))
			return nil
		}
		m.k.challenge(r, m.me(), them)
		m.screen, m.cursor, m.notice = screenLobby, 0, "Challenge sent to "+them.Name+"."
	}
	return nil
}

// ── Drawing ─────────────────────────────────────────────────────────────────

func (m *viewerModel) style(color string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(m.v.Color(color, lipgloss.NoColor{}))
}

func (m *viewerModel) View() string {
	switch m.screen {
	case screenTable:
		return m.tableView()
	case screenPicker:
		return m.pickerView()
	default:
		return m.lobbyView()
	}
}

func (m *viewerModel) tableView() string {
	t := m.table()
	if t == nil {
		return "That game is gone."
	}
	var who []string
	for i, p := range t.Seats {
		name := p.Name
		if p.Empty() {
			name = m.style("comment").Render("(open)")
		}
		seat := m.k.rules.SeatNames[i]
		if !t.outcome().Over && t.full() && t.game.Turn() == i {
			seat = "▸ " + seat
		}
		who = append(who, fmt.Sprintf("%s: %s", seat, name))
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
		header += "   " + m.style("yellow").Bold(true).Render(result)
	} else if !t.full() {
		header += "   " + m.style("comment").Render("waiting for players")
	}

	var footer string
	if m.menuOpen {
		var parts []string
		for i, it := range m.menu() {
			label := it.label
			if i == m.menuIdx {
				label = m.style("background").Background(m.v.Color("cyan", lipgloss.Color("6"))).Render(" " + label + " ")
			}
			parts = append(parts, label)
		}
		footer = strings.Join(parts, "  ")
	} else {
		mySeat := t.seatOf(m.v.ID)
		hint := "Tab: menu"
		switch {
		case mySeat < 0 && m.room().mode() == ModeSeats && !t.full():
			hint = "Tab: sit down"
		case mySeat < 0:
			hint = "Spectating · Tab: menu"
		}
		if m.notice != "" {
			hint = m.notice + " · " + hint
		}
		footer = m.style("comment").Render(hint)
	}
	return lipgloss.JoinVertical(lipgloss.Left, header, m.board.View(), footer)
}

func (m *viewerModel) lobbyView() string {
	r := m.room()
	title := m.k.rules.Name
	if r.mode() == ModePrivate {
		title += " — your games"
	} else {
		title += " — lobby"
	}
	lines := []string{m.style("cyan").Bold(true).Render(title), ""}
	for i, it := range m.lobby() {
		prefix := "  "
		if i == m.cursor {
			prefix = m.style("cyan").Render("▸ ")
		}
		lines = append(lines, prefix+it.label)
	}
	lines = append(lines, "")
	if m.notice != "" {
		lines = append(lines, m.style("green").Render(m.notice))
	}
	lines = append(lines, m.style("comment").Render("↑↓ choose · Enter open · q hand keys back to Concord"))
	return strings.Join(lines, "\n")
}

func (m *viewerModel) pickerView() string {
	lines := []string{m.style("cyan").Bold(true).Render("Choose an opponent"), ""}
	people := m.pickable()
	if m.members == nil {
		lines = append(lines, m.style("comment").Render("  Looking up members…"))
	} else if len(people) == 0 {
		lines = append(lines, m.style("comment").Render("  Nobody else can see this channel yet."))
	}
	for i, p := range people {
		prefix := "  "
		if i == m.cursor {
			prefix = m.style("cyan").Render("▸ ")
		}
		dot := m.style("comment").Render("○")
		if p.Online {
			dot = m.style("green").Render("●")
		}
		lines = append(lines, prefix+dot+" "+p.DisplayName)
	}
	lines = append(lines, "", m.style("comment").Render("↑↓ choose · Enter pick · Esc back"))
	return strings.Join(lines, "\n")
}
