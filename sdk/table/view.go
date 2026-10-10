package table

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/JMThomas00/Concord/sdk/arcade"
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

	cursor     int // lobby / picker selection
	headerRows int // rows above the board in the last table view (for Images)
	menuOpen   bool
	menuIdx    int
	members    []wire.PluginMember
	notice     string
	pending    tea.Cmd // e.g. a newly opened board's Init, returned with the next Update

	// The arcade (arcade.go).
	level   int            // 1 PLAYER VS CPU's level
	frame   int            // animation ticks
	lastKey time.Time      // attract mode stops 30s after the last key
	setsRow int            // the collection screen's row
	setsIdx map[string]int // the collection screen's item per kind
	picked  string         // the unlockable just picked from a draft
	reveal  time.Time      // when it was picked (its reveal is showing)
	ticked  bool           // animated at the last tick

	width, height int
}

func (k *Kit) newViewer(v *pane.Viewer) tea.Model {
	m := &viewerModel{k: k, v: v, channel: v.ChannelID, width: v.Width, height: v.Height}
	k.viewers[v.ID] = m
	r := k.room(v.ChannelID)
	m.level = r.computerLevel()
	switch {
	case k.rules.Arcade != nil: // always the front door
		m.screen = screenTitle
		m.lastKey = time.Now()
		k.startTicker()
	case r.mode() == ModeSeats:
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
		equipped: func(kind string) string { return m.k.equippedFor(m.v.ID)[kind] },
		frame:    func() int { return m.frame },
		effects:  func() string { return m.effects() },
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
		var t *Table
		if m.screen == screenTable || m.screen == screenResults {
			t = m.room().table(m.tableID)
		}
		if (m.screen == screenTable || m.screen == screenResults) && t == nil {
			m.screen, m.notice = screenLobby, "That game has ended."
			if m.k.rules.Arcade != nil {
				m.screen = screenMenu
			}
		}
		if t != nil {
			m.seat.table, m.seat.Index = t, t.seatOf(m.v.ID)
			if move, ok := msg.moved[t.ID]; ok {
				m.board, _ = m.board.Update(ChangedMsg{Move: move})
			}
			if m.screen == screenResults && !t.outcome().Over { // the rematch started
				m.screen = screenTable
			}
			if m.screen == screenTable && t.outcome().Over {
				m.lastKey = time.Now() // the winning line blinks for a while
			}
			m.k.startTicker() // the board may animate the move (Animator)
		}
	case tickMsg:
		if m.effects() == "" {
			m.frame++
		}
		if !m.reveal.IsZero() && time.Since(m.reveal) >= revealFor {
			m.endReveal()
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
	if done, cmd := m.arcadeKey(msg); done {
		return cmd
	}
	if m.arcadeOn() {
		m.lastKey = time.Now()
		switch k := msg.String(); {
		case k == "esc" && m.screen == screenTable && !m.menuOpen && !m.boardClaims(wire.PaneKeyEsc):
			m.back()
			return nil
		case (k == "esc" || k == "q") && m.screen == screenLobby:
			m.back()
			return nil
		case k == "esc" && m.screen == screenPicker:
			m.back()
			return nil
		case (k == "enter" || k == " ") && m.screen == screenTable && !m.menuOpen && m.arcadeTable():
			if t := m.table(); t != nil && t.outcome().Over {
				m.k.uiSound(m, arcade.SoundSelect)
				m.goTo(screenResults)
				return nil
			}
		}
	}
	switch m.screen {
	case screenTable:
		// M opens the table menu, unless the board is taking typed text.
		if k := msg.String(); (k == "m" || k == "M") && !boardTyping(m.board) {
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
					items = append(items, menuItem{"Computer plays " + k.rules.SeatNames[i], func() { k.sit(r, t, i, k.computerPlayer(r.computerLevel())) }})
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
	extra   bool   // new games: the arcade menu has its own way to start them
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
	items = append(items, lobbyItem{label: "+ " + verb, extra: true, open: func() {
		m.screen, m.cursor, m.members = screenPicker, 0, nil
		id := m.v.ID
		k.requestMembers(m.channel, func(members []wire.PluginMember) { k.host.Send(id, membersMsg{members}) })
	}})
	if k.computerAllowed(r) {
		items = append(items, lobbyItem{label: "+ Play the computer", extra: true, open: func() { m.openTable(k.startGame(r, me, k.computerPlayer(r.computerLevel()))) }})
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
	items := m.lobbyItems()
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
	if m.k.rules.Arcade != nil {
		if m.screen >= screenTitle && m.screen != screenResults && !m.big() {
			return m.smallFront()
		}
		if m.arcadeOn() && (m.screen >= screenTitle || m.screen == screenLobby || m.screen == screenPicker || m.arcadeTable()) {
			return m.arcadeView()
		}
		if m.screen == screenResults { // the pane shrank on the results screen
			m.screen = screenTable
		}
	}
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
		hint := "M: menu"
		switch {
		case mySeat < 0 && m.room().mode() == ModeSeats && !t.full():
			hint = "M: sit down"
		case mySeat < 0:
			hint = "Spectating · M: menu"
		}
		if m.notice != "" {
			hint = m.notice + " · " + hint
		}
		footer = m.style("comment").Render(hint)
	}
	m.headerRows = lipgloss.Height(header)
	return lipgloss.JoinVertical(lipgloss.Left, header, m.board.View(), footer)
}

// ClaimedKeys (pane.KeyClaimer) keeps Esc while the kit has something
// open for it to close (the table menu, the member picker), and otherwise
// passes on whatever the board claims. Unclaimed, Esc leaves the pane.
func (m *viewerModel) ClaimedKeys() []string {
	if m.arcadeOn() {
		// Esc goes back one screen, except on the title, where it leaves.
		switch {
		case m.screen == screenTitle:
			return nil
		case m.screen == screenTable && !m.menuOpen && m.board != nil:
			if kc, ok := m.board.(pane.KeyClaimer); ok {
				return append(kc.ClaimedKeys(), wire.PaneKeyEsc)
			}
		}
		return []string{wire.PaneKeyEsc}
	}
	switch {
	case m.screen == screenPicker:
		return []string{wire.PaneKeyEsc}
	case m.screen != screenTable:
		return nil
	case m.menuOpen:
		return []string{wire.PaneKeyEsc}
	}
	if kc, ok := m.board.(pane.KeyClaimer); ok {
		return kc.ClaimedKeys()
	}
	return nil
}

// Typer is implemented by a board that sometimes takes typed text, such as
// a move in notation. While Typing is true, every key goes to the board:
// M doesn't open the table menu. (Claim Esc too, so it can cancel.)
type Typer interface {
	Typing() bool
}

// boardClaims reports whether the board claims key right now.
func (m *viewerModel) boardClaims(key string) bool {
	kc, ok := m.board.(pane.KeyClaimer)
	return ok && slices.Contains(kc.ClaimedKeys(), key)
}

func boardTyping(b tea.Model) bool {
	t, ok := b.(Typer)
	return ok && t.Typing()
}

// Images passes on a board's images (pane.Imager), moved down past the
// header above it.
func (m *viewerModel) Images() []wire.PaneImage {
	if m.screen != screenTable || m.board == nil || m.table() == nil || m.arcadeTable() {
		return nil // the arcade draws pieces itself
	}
	im, ok := m.board.(pane.Imager)
	if !ok {
		return nil
	}
	images := im.Images()
	for i := range images {
		images[i].Row += m.headerRows
	}
	return images
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
