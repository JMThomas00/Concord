package table

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/JMThomas00/Concord/sdk/arcade"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Arcade puts a game behind the Concord Arcade front door (see the
// "Concord - Arcade Standard" and .claude/skills/concord/arcade.md): every
// time someone opens the channel they see the game's title screen, then a
// menu -- 1 PLAYER VS CPU, the seating mode's way to find an opponent,
// WATCH, the game's collection of unlockables, HALL OF FAME, HOW TO PLAY
// and OPTIONS -- and games are played on an arcade table with the players
// on either side. Each game gives it its own personality: the logo, what
// attract mode shows, what you unlock and what the rewards are called.
//
// The board draws itself on the arcade canvas by implementing ArcadeBoard;
// a board that doesn't keeps the plain table view behind the front door.
type Arcade struct {
	// Title is the logo, in the arcade pixel font: up to 13 characters
	// fits 80 columns ("TIC-TAC-TOE").
	Title string
	// Tagline is the line under the logo ("THREE IN A ROW").
	Tagline string
	// HowTo is the HOW TO PLAY page: the rules in a few short lines.
	HowTo []string
	// Keys are the board's own keys for the footer while playing
	// ({"1-9", "place"}); the kit adds M (menu) and Esc (back).
	Keys []arcade.Key
	// Sounds says the plugin ships the arcade sound kit in its client/
	// folder (arcade.WriteSoundKit), so menus blip and select.
	Sounds bool

	// Reward names one pass in capitals ("GOLD STAR"; default "PASS"),
	// Rewards the plural (default Reward + "S").
	Reward, Rewards string
	// Collection names the menu item and screen for the unlockables
	// ("SETS"; default "COLLECTION").
	Collection string
	// Unlockables are everything a player can own, each with a Kind;
	// starters are owned from the start. Passes are earned for each new
	// achievement, every third win in a row and every tenth game.
	Unlockables []arcade.Unlockable
	// Kinds are the rows of the collection screen, in order. Each player
	// picks one item of each kind (Seat.Equipped).
	Kinds []Kind
	// Preview draws unlockable id within w x h cells at (x, y): shown in
	// the collection, on offer cards and on the menu. sel is the player's
	// choice of each kind with id swapped in, so a board can be shown with
	// their pieces. ghost draws it as a locked silhouette (role "ghost").
	Preview func(c *arcade.Canvas, id string, sel map[string]string, x, y, w, h int, ghost bool)

	// Attract draws the title screen's attract mode in w x h cells at
	// (x, y) -- 76 x 14 in an 80-column pane. frame counts ticks, about
	// five a second; it stops changing
	// 30 seconds after the last key, or when the viewer turns effects off.
	Attract func(c *arcade.Canvas, x, y, w, h, frame int)
	// Result is the results screen's headline ("X WINS!", "CAT'S GAME!");
	// by default the winner's seat name and WINS!, or DRAW!.
	Result func(g Game, o Outcome, seatNames []string) string
	// ResultArt may draw something in w x h cells at (x, y) on the results
	// screen and report true; otherwise the final board is shown there.
	ResultArt func(c *arcade.Canvas, g Game, o Outcome, x, y, w, h, frame int) bool
}

// Kind is one row of the collection screen.
type Kind struct {
	ID    string // matches arcade.Unlockable.Kind
	Label string // "PIECES"
}

// ArcadeBoard is a board that draws itself on the arcade canvas. The kit
// draws the top bar, the players' panels, the score and the keys, and
// hands the board the middle of the screen.
type ArcadeBoard interface {
	// Draw draws the board in w x h cells at (x, y).
	Draw(c *arcade.Canvas, x, y, w, h int)
	// DrawSeat draws seat's piece (an X, a white king) in w x h cells at
	// (x, y), for that player's panel.
	DrawSeat(c *arcade.Canvas, seat, x, y, w, h int)
	// Status is a line from the board for the status row ("not your
	// turn"), or "" for the kit's own.
	Status() string
}

const (
	arcadeW, arcadeH = 80, 24 // the design size; bigger panes centre it
	tickEvery        = 200 * time.Millisecond
	attractFor       = 30 * time.Second // attract mode runs this long after the last key
	revealFor        = 2500 * time.Millisecond
)

// The arcade's own screens (the lobby, picker and table are shared with
// the plain layout).
const (
	screenTitle screen = iota + 100
	screenMenu
	screenResults
	screenSets
	screenDraft
	screenHOF
	screenHowTo
	screenOptions
)

// tickMsg is the arcade's animation clock.
type tickMsg struct{}

func (a *Arcade) reward(n int) string {
	one := a.Reward
	if one == "" {
		one = "PASS"
	}
	if n == 1 {
		return one
	}
	if a.Rewards != "" {
		return a.Rewards
	}
	return one + "S"
}

func (a *Arcade) collection() string {
	if a.Collection == "" {
		return "COLLECTION"
	}
	return a.Collection
}

func (a *Arcade) kinds() []Kind {
	if len(a.Kinds) > 0 {
		return a.Kinds
	}
	return []Kind{{ID: "", Label: a.collection()}}
}

func (a *Arcade) ofKind(kind string) []arcade.Unlockable {
	var out []arcade.Unlockable
	for _, u := range a.Unlockables {
		if u.Kind == kind {
			out = append(out, u)
		}
	}
	return out
}

func (a *Arcade) item(id string) (arcade.Unlockable, bool) {
	for _, u := range a.Unlockables {
		if u.ID == id {
			return u, true
		}
	}
	return arcade.Unlockable{}, false
}

// ── State the arcade keeps ──────────────────────────────────────────────────

// record is a viewer's record, created (unsaved) the first time it's needed.
func (k *Kit) record(id uuid.UUID) *playerRecord {
	k.loadRecords()
	r := k.records[id]
	if r == nil {
		r = &playerRecord{}
		k.records[id] = r
	}
	return r
}

// equippedFor is a player's choice of each kind, starters filled in.
func (k *Kit) equippedFor(id uuid.UUID) map[string]string {
	a := k.rules.Arcade
	sel := map[string]string{}
	if a == nil {
		return sel
	}
	rec := k.record(id)
	for _, kind := range a.kinds() {
		if v := rec.Equipped[kind.ID]; v != "" && rec.Rewards.Owns(v, a.Unlockables) {
			sel[kind.ID] = v
			continue
		}
		for _, u := range a.ofKind(kind.ID) {
			if u.Tier == arcade.Starter {
				sel[kind.ID] = u.ID
				break
			}
		}
	}
	return sel
}

// uiSound plays one of the arcade kit's sounds to one viewer, unless the
// game has no sounds or they muted them.
func (k *Kit) uiSound(m *viewerModel, sound string) {
	if a := k.rules.Arcade; a == nil || !a.Sounds || k.conn == nil || k.record(m.v.ID).Muted {
		return
	}
	_ = k.conn.PlaySound(m.channel, m.v.ID, sound, 0)
}

// startTicker runs the animation clock until no viewer needs it. The
// clock runs on its own goroutine and hands each tick back through
// Conn.Post, since a pane model's commands run synchronously (a tea.Tick
// would block every viewer).
func (k *Kit) startTicker() {
	if k.ticking || k.conn == nil || k.rules.Arcade == nil {
		return
	}
	k.ticking = true
	k.tickGen++
	gen, conn, stop := k.tickGen, k.conn, make(chan struct{})
	go func() {
		t := time.NewTicker(tickEvery)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				conn.Post(func() {
					if k.tickGen != gen || !k.ticking {
						return
					}
					if !k.tick() {
						k.ticking = false
						close(stop)
					}
				})
			}
		}
	}()
}

// tick advances every animated viewer; it reports whether any still are.
func (k *Kit) tick() bool {
	live := false
	for id, m := range k.viewers {
		if m.animated() {
			live = true
			k.host.Send(id, tickMsg{})
		}
	}
	return live
}

// soloTable is the viewer's game against the computer: their unfinished
// one at this level, or a new one. Their other games against the computer
// are cleared away.
func (k *Kit) soloTable(r *Room, me Player, level int) *Table {
	var keep []*Table
	var resume *Table
	for _, t := range r.Tables {
		if t.Main || t.seatOf(me.UserID) < 0 || !t.hasComputer() {
			keep = append(keep, t)
			continue
		}
		if resume == nil && !t.outcome().Over && t.computerLevel() == level {
			resume = t
			keep = append(keep, t)
		}
	}
	if len(keep) != len(r.Tables) {
		r.Tables = keep
		k.changed(r, nil, "")
	}
	if resume != nil {
		return resume
	}
	return k.startGame(r, me, k.computerPlayer(level))
}

func (t *Table) hasComputer() bool {
	for _, p := range t.Seats {
		if p.Computer {
			return true
		}
	}
	return false
}

func (t *Table) computerLevel() int {
	for _, p := range t.Seats {
		if p.Computer {
			return p.Level
		}
	}
	return 0
}

// ready asks for a rematch from seat; when every person at the table has,
// it starts.
func (k *Kit) ready(r *Room, t *Table, seat int) {
	if !t.outcome().Over || seat < 0 || seat >= len(t.Seats) {
		return
	}
	if len(t.Ready) != len(t.Seats) {
		t.Ready = make([]bool, len(t.Seats))
	}
	t.Ready[seat] = true
	k.changed(r, t, "")
	for i, p := range t.Seats {
		if !p.Computer && !t.Ready[i] {
			return
		}
	}
	k.rematch(r, t)
}

// lastResult describes a finished game for the menu's ticker line.
func (k *Kit) lastResult(t *Table, o Outcome) string {
	moves := fmt.Sprintf(" IN %d MOVES", len(t.Moves))
	if o.Winner >= 0 && o.Winner < len(t.Seats) && len(t.Seats) == 2 {
		return strings.ToUpper(t.Seats[o.Winner].Name + " BEAT " + t.Seats[1-o.Winner].Name + moves)
	}
	var names []string
	for _, p := range t.Seats {
		names = append(names, p.Name)
	}
	return strings.ToUpper(strings.Join(names, " AND ") + " DREW" + moves)
}

// ── The viewer's side ───────────────────────────────────────────────────────

// big reports whether the pane fits the arcade layout.
func (m *viewerModel) big() bool { return m.width >= 64 && m.height >= arcadeH }

// arcadeOn reports whether the arcade draws this viewer's screen.
func (m *viewerModel) arcadeOn() bool { return m.k.rules.Arcade != nil && m.big() }

// arcadeTable reports whether the current table is drawn by the arcade.
func (m *viewerModel) arcadeTable() bool {
	if !m.arcadeOn() || m.screen != screenTable {
		return false
	}
	_, ok := m.board.(ArcadeBoard)
	return ok
}

func (m *viewerModel) effects() string { return m.k.record(m.v.ID).Effects }

// animated reports whether the viewer's screen moves on its own now.
func (m *viewerModel) animated() bool {
	if !m.reveal.IsZero() {
		return true // the draft's reveal moves on by itself
	}
	if m.effects() != "" || !m.arcadeOn() || time.Since(m.lastKey) > attractFor {
		return false
	}
	switch m.screen {
	case screenTitle, screenResults:
		return true
	case screenTable:
		t := m.table()
		return t != nil && t.outcome().Over // the winning line blinks
	}
	return false
}

// blink is on for half of each second (always on when effects are off).
func (m *viewerModel) blink() bool { return m.effects() != "" || m.frame/3%2 == 0 }

// goTo shows an arcade screen.
func (m *viewerModel) goTo(s screen) {
	m.screen, m.notice, m.cursor = s, "", 0
	m.lastKey = time.Now()
	m.k.startTicker()
}

// back goes to the menu from any arcade screen.
func (m *viewerModel) back() {
	m.k.uiSound(m, arcade.SoundBack)
	m.menuOpen = false
	m.goTo(screenMenu)
}

// enterPlain leaves the front door for the plain layout's first screen
// (a pane too small for the arcade).
func (m *viewerModel) enterPlain() {
	r := m.room()
	if r.mode() == ModeSeats {
		m.openTable(m.k.seatsTable(r))
		return
	}
	m.screen, m.cursor = screenLobby, 0
}

// arcadeKey handles keys on the arcade's own screens; it reports whether
// it did.
func (m *viewerModel) arcadeKey(msg tea.KeyMsg) (bool, tea.Cmd) {
	if m.screen < screenTitle {
		return false, nil
	}
	m.lastKey = time.Now()
	m.k.startTicker()
	key := msg.String()
	if !m.big() {
		switch key {
		case "enter", " ":
			m.enterPlain()
		case "esc", "q":
			return true, tea.Quit
		}
		return true, nil
	}
	switch m.screen {
	case screenTitle:
		switch key {
		case "enter", " ":
			m.k.uiSound(m, arcade.SoundCoin)
			m.goTo(screenMenu)
		case "esc", "q":
			return true, tea.Quit
		}
	case screenMenu:
		m.menuKeyArcade(key)
	case screenResults:
		m.resultsKey(key)
	case screenSets:
		m.setsKey(key)
	case screenDraft:
		m.draftKey(key)
	case screenHOF, screenHowTo:
		if key == "esc" || key == "enter" || key == "q" {
			m.back()
		}
	case screenOptions:
		m.optionsKey(key)
	}
	return true, nil
}

// ── Menu ────────────────────────────────────────────────────────────────────

type arcadeItem struct {
	label string
	desc  func() (string, string) // text, colour role
	enter func()
	turn  func(d int) // ←/→
}

func (m *viewerModel) arcadeMenu() []arcadeItem {
	k, r, a, me := m.k, m.room(), m.k.rules.Arcade, m.me()
	var items []arcadeItem
	if k.computerAllowed(r) {
		items = append(items, arcadeItem{
			label: "1 PLAYER VS CPU",
			desc: func() (string, string) {
				d := "◂ " + levelName(m.level) + " ▸"
				for _, t := range r.Tables {
					if !t.Main && t.seatOf(me.UserID) >= 0 && t.hasComputer() && !t.outcome().Over && t.computerLevel() == m.level {
						return d + fmt.Sprintf("  RESUME · MOVE %d", len(t.Moves)+1), "fg"
					}
				}
				return d, "fg"
			},
			enter: func() { m.openTable(k.soloTable(r, me, m.level)) },
			turn:  func(d int) { m.level = min(3, max(1, m.level+d)) },
		})
	}
	switch r.mode() {
	case ModeSeats:
		items = append(items, arcadeItem{
			label: "TAKE A SEAT",
			desc: func() (string, string) {
				t := k.seatsTable(r)
				var parts []string
				for i, p := range t.Seats {
					name := strings.ToUpper(p.Name)
					if p.Empty() {
						name = "OPEN"
					}
					parts = append(parts, k.rules.SeatNames[i]+" "+name)
				}
				return strings.Join(parts, " · "), "dim"
			},
			enter: func() {
				t := k.seatsTable(r)
				if t.seatOf(me.UserID) < 0 && (!t.full() || t.outcome().Over) {
					for i, p := range t.Seats {
						if p.Empty() {
							k.sit(r, t, i, me)
							break
						}
					}
				}
				m.openTable(t)
			},
		})
	default:
		label, desc := "2 PLAYERS", "CHALLENGE SOMEONE HERE"
		if r.mode() == ModePrivate {
			label, desc = "NEW GAME", "PICK AN OPPONENT"
		}
		items = append(items, arcadeItem{
			label: label,
			desc:  func() (string, string) { return desc, "dim" },
			enter: func() { m.openPicker() },
		})
		label = "WATCH"
		if r.mode() == ModePrivate {
			label = "YOUR GAMES"
		}
		items = append(items, arcadeItem{
			label: label,
			desc: func() (string, string) {
				in := 0
				for _, c := range r.Challenges {
					if c.To.UserID == me.UserID {
						in++
					}
				}
				if in > 0 {
					return fmt.Sprintf("★ %d %s FOR YOU!", in, plural(in, "CHALLENGE")), "yellow"
				}
				return fmt.Sprintf("%d %s", len(m.lobbyItems()), plural(len(m.lobbyItems()), "GAME")), "dim"
			},
			enter: func() { m.screen, m.cursor, m.notice = screenLobby, 0, "" },
		})
	}
	if len(a.Unlockables) > 0 {
		items = append(items, arcadeItem{
			label: a.collection(),
			desc: func() (string, string) {
				rec := k.record(me.UserID)
				if n := rec.Rewards.Passes; n > 0 {
					return fmt.Sprintf("★ %d %s · NEW %s!", n, a.reward(n), a.collection()), "yellow"
				}
				return fmt.Sprintf("%d OF %d UNLOCKED", rec.Rewards.Count(a.Unlockables), len(a.Unlockables)), "dim"
			},
			enter: func() { m.goTo(screenSets); m.setsRow = 0 },
		})
	}
	items = append(items, arcadeItem{label: "HALL OF FAME", desc: func() (string, string) { return "MOST WINS IN THIS CHANNEL'S GAME", "dim" }, enter: func() { m.goTo(screenHOF) }})
	if len(a.HowTo) > 0 {
		items = append(items, arcadeItem{label: "HOW TO PLAY", desc: func() (string, string) { return "RULES AND KEYS", "dim" }, enter: func() { m.goTo(screenHowTo) }})
	}
	items = append(items, arcadeItem{label: "OPTIONS", desc: func() (string, string) {
		rec := k.record(me.UserID)
		s := "EFFECTS " + effectsName(rec.Effects)
		if a.Sounds {
			s = "SOUND " + onOff(!rec.Muted) + " · " + s
		}
		return s, "dim"
	}, enter: func() { m.goTo(screenOptions) }})
	return items
}

func levelName(l int) string { return [...]string{"", "EASY", "NORMAL", "HARD"}[min(3, max(1, l))] }

func effectsName(e string) string {
	switch e {
	case "calm":
		return "CALM"
	case "off":
		return "OFF"
	}
	return "FULL"
}

func onOff(b bool) string {
	if b {
		return "ON"
	}
	return "OFF"
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "S"
}

func (m *viewerModel) menuKeyArcade(key string) {
	items := m.arcadeMenu()
	m.cursor = min(m.cursor, len(items)-1)
	switch key {
	case "up", "k":
		m.cursor = (m.cursor + len(items) - 1) % len(items)
		m.k.uiSound(m, arcade.SoundBlip)
	case "down", "j":
		m.cursor = (m.cursor + 1) % len(items)
		m.k.uiSound(m, arcade.SoundBlip)
	case "left", "h", "right", "l":
		if it := items[m.cursor]; it.turn != nil {
			d := 1
			if key == "left" || key == "h" {
				d = -1
			}
			it.turn(d)
			m.k.uiSound(m, arcade.SoundBlip)
		}
	case "enter", " ":
		m.k.uiSound(m, arcade.SoundSelect)
		items[m.cursor].enter()
	case "esc", "q":
		m.k.uiSound(m, arcade.SoundBack)
		m.goTo(screenTitle)
	}
}

func (m *viewerModel) openPicker() {
	m.screen, m.cursor, m.members = screenPicker, 0, nil
	id := m.v.ID
	m.k.requestMembers(m.channel, func(members []wire.PluginMember) { m.k.host.Send(id, membersMsg{members}) })
}

// ── Results, collection, draft, options ────────────────────────────────────

func (m *viewerModel) resultsKey(key string) {
	r, t := m.room(), m.room().table(m.tableID)
	switch key {
	case "enter", " ":
		if t == nil {
			m.back()
			return
		}
		if seat := t.seatOf(m.v.ID); seat >= 0 {
			m.k.uiSound(m, arcade.SoundSelect)
			m.k.ready(r, t, seat)
			return
		}
		m.screen = screenTable // a spectator goes back to watching
	case "esc", "q":
		m.back()
	}
}

func (m *viewerModel) setsKey(key string) {
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	kinds := a.kinds()
	m.setsRow = min(m.setsRow, len(kinds)-1)
	kind := kinds[m.setsRow]
	items := a.ofKind(kind.ID)
	if m.setsIdx == nil {
		m.setsIdx = map[string]int{}
	}
	if _, ok := m.setsIdx[kind.ID]; !ok { // start on what they're using
		sel := m.k.equippedFor(m.v.ID)[kind.ID]
		m.setsIdx[kind.ID] = max(0, slices.IndexFunc(items, func(u arcade.Unlockable) bool { return u.ID == sel }))
	}
	i := m.setsIdx[kind.ID]
	m.notice = ""
	switch key {
	case "up", "k":
		m.setsRow = (m.setsRow + len(kinds) - 1) % len(kinds)
		m.k.uiSound(m, arcade.SoundBlip)
	case "down", "j":
		m.setsRow = (m.setsRow + 1) % len(kinds)
		m.k.uiSound(m, arcade.SoundBlip)
	case "left", "h":
		m.setsIdx[kind.ID] = (i + len(items) - 1) % max(1, len(items))
		m.k.uiSound(m, arcade.SoundBlip)
	case "right", "l":
		m.setsIdx[kind.ID] = (i + 1) % max(1, len(items))
		m.k.uiSound(m, arcade.SoundBlip)
	case "enter", " ":
		if i >= len(items) {
			return
		}
		u := items[i]
		switch {
		case rec.Rewards.Owns(u.ID, a.Unlockables):
			if rec.Equipped == nil {
				rec.Equipped = map[string]string{}
			}
			rec.Equipped[kind.ID] = u.ID
			m.k.saveRecords()
			m.k.uiSound(m, arcade.SoundSelect)
			m.notice = u.Name + " IT IS."
		case rec.Rewards.Passes > 0:
			m.k.uiSound(m, arcade.SoundSelect)
			m.goTo(screenDraft)
		default:
			m.notice = "EARN " + a.reward(2) + " TO UNLOCK MORE."
			m.k.uiSound(m, arcade.SoundBack)
		}
	case "esc", "q":
		m.back()
	}
}

// offer is the viewer's current draft, dealt (and saved) on first look.
func (m *viewerModel) offer() []string {
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	had := len(rec.Rewards.Offer)
	offer := rec.Rewards.Deal(a.Unlockables, m.k.rnd)
	if len(offer) != had {
		m.k.saveRecords()
	}
	return offer
}

func (m *viewerModel) draftKey(key string) {
	if !m.reveal.IsZero() {
		return
	}
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	offer := m.offer()
	if len(offer) == 0 {
		m.goTo(screenSets)
		return
	}
	m.cursor = min(m.cursor, len(offer)-1)
	switch key {
	case "left", "h":
		m.cursor = (m.cursor + len(offer) - 1) % len(offer)
		m.k.uiSound(m, arcade.SoundBlip)
	case "right", "l":
		m.cursor = (m.cursor + 1) % len(offer)
		m.k.uiSound(m, arcade.SoundBlip)
	case "enter", " ":
		id := offer[m.cursor]
		if !rec.Rewards.Pick(id) {
			return
		}
		u, _ := a.item(id)
		if rec.Equipped == nil {
			rec.Equipped = map[string]string{}
		}
		rec.Equipped[u.Kind] = id
		m.k.saveRecords()
		m.picked, m.reveal = id, time.Now()
		m.k.uiSound(m, arcade.SoundRecord)
		m.k.startTicker()
	case "esc", "q":
		m.k.uiSound(m, arcade.SoundBack)
		m.goTo(screenSets)
	}
}

// endReveal moves on from the draft's reveal to the collection, showing
// what was picked.
func (m *viewerModel) endReveal() {
	a := m.k.rules.Arcade
	u, _ := a.item(m.picked)
	for row, kind := range a.kinds() {
		if kind.ID == u.Kind {
			m.setsRow = row
			if m.setsIdx == nil {
				m.setsIdx = map[string]int{}
			}
			m.setsIdx[kind.ID] = max(0, slices.IndexFunc(a.ofKind(kind.ID), func(x arcade.Unlockable) bool { return x.ID == u.ID }))
		}
	}
	m.reveal, m.picked = time.Time{}, ""
	m.goTo(screenSets)
}

func (m *viewerModel) optionsKey(key string) {
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	rows := 1
	if a.Sounds {
		rows = 2
	}
	m.cursor = min(m.cursor, rows-1)
	switch key {
	case "up", "k", "down", "j":
		m.cursor = (m.cursor + 1) % rows
		m.k.uiSound(m, arcade.SoundBlip)
	case "left", "h", "right", "l", "enter", " ":
		if rows == 2 && m.cursor == 0 {
			rec.Muted = !rec.Muted
		} else {
			order := []string{"", "calm", "off"}
			i := slices.Index(order, rec.Effects)
			d := 1
			if key == "left" || key == "h" {
				d = 2
			}
			rec.Effects = order[(max(0, i)+d)%3]
		}
		m.k.saveRecords()
		m.k.uiSound(m, arcade.SoundBlip)
	case "esc", "q":
		m.back()
	}
}

// ── Drawing ─────────────────────────────────────────────────────────────────

// scr is an 80 x 24 design frame centred in the pane.
type scr struct {
	c      *arcade.Canvas
	ox, oy int
	w      int // the frame's width: 80, or the pane's if narrower
}

func (m *viewerModel) canvas() scr {
	c := arcade.New(m.width, m.height, arcade.NewPalette(m.v.Theme))
	w := min(m.width, arcadeW)
	return scr{c: c, ox: (m.width - w) / 2, oy: (m.height - arcadeH) / 2, w: w}
}

func (s scr) text(x, y int, str, fg, bg string, bold bool) int {
	return s.c.Text(s.ox+x, s.oy+y, str, fg, bg, bold) - s.ox
}
func (s scr) center(y int, str, fg string, bold bool) {
	s.c.CenterIn(s.ox, s.w, s.oy+y, str, fg, "", bold)
}
func (s scr) centerIn(x, w, y int, str, fg string, bold bool) {
	s.c.CenterIn(s.ox+x, w, s.oy+y, str, fg, "", bold)
}
func (s scr) box(x, y, w, h int, role, title, titleRole string) {
	s.c.Box(s.ox+x, s.oy+y, w, h, role, title, titleRole)
}
func (s scr) logo(text string, row int, shade []string) {
	s.c.Logo(text, s.ox+(s.w-arcade.LogoWidth(text, 1))/2, 2*(s.oy+row), 1, shade)
}
func (s scr) keys(keys ...arcade.Key) { s.c.Keys(s.ox+1, s.oy+arcadeH-1, keys...) }

// topBar is the arcade's first row: you, the best in the channel, and
// where you are.
func (m *viewerModel) topBar(s scr, where string) {
	rec := m.k.record(m.v.ID)
	x := s.text(1, 0, "1UP ", "pink", "", true)
	s.text(x, 0, fmt.Sprintf("%s %d-%d-%d", strings.ToUpper(m.v.DisplayName), rec.Wins, rec.Losses, rec.Draws), "fg", "", false)
	if best, name := m.k.best(); best > 0 && s.w >= 80 {
		x = s.text(31, 0, "HI-SCORE ", "pink", "", true)
		s.text(x, 0, fmt.Sprintf("%d %s %s", best, plural(best, "WIN"), strings.ToUpper(name)), "fg", "", false)
	}
	s.text(s.w-1-arcade.TextWidth(where), 0, where, "dim", "", false)
}

// best is the channel game's top winner.
func (k *Kit) best() (int, string) {
	k.loadRecords()
	best, name := 0, ""
	for _, r := range k.records {
		if r.Wins > best || (r.Wins == best && best > 0 && r.Name < name) {
			best, name = r.Wins, r.Name
		}
	}
	return best, name
}

func (m *viewerModel) where() string {
	return "#" + strings.ToLower(strings.ReplaceAll(m.k.rules.Name, " ", "-"))
}

func (m *viewerModel) arcadeView() string {
	s := m.canvas()
	switch m.screen {
	case screenTitle:
		m.drawTitle(s)
	case screenMenu:
		m.drawMenu(s)
	case screenLobby:
		m.drawLobby(s)
	case screenPicker:
		m.drawPicker(s)
	case screenTable:
		m.drawTable(s)
	case screenResults:
		m.drawResults(s)
	case screenSets:
		m.drawSets(s)
	case screenDraft:
		m.drawDraft(s)
	case screenHOF:
		m.drawHOF(s)
	case screenHowTo:
		m.drawHowTo(s)
	case screenOptions:
		m.drawOptions(s)
	}
	return s.c.String()
}

// smallFront is the front door in a pane too small for the arcade.
func (m *viewerModel) smallFront() string {
	a := m.k.rules.Arcade
	lines := []string{
		m.style("purple").Bold(true).Render(a.Title),
		m.style("comment").Render(a.Tagline),
		"",
		m.style("yellow").Bold(true).Render("Enter to play"),
		m.style("comment").Render("(a pane of 64×24 or more shows the arcade)"),
	}
	return strings.Join(lines, "\n")
}

func (m *viewerModel) drawTitle(s scr) {
	a, r := m.k.rules.Arcade, m.room()
	m.topBar(s, m.where())
	s.logo(a.Title, 1, nil)
	if a.Tagline != "" {
		s.center(5, spaced(a.Tagline), "dim", false)
	}
	if a.Attract != nil {
		a.Attract(s.c, s.ox+2, s.oy+6, s.w-4, 14, m.frame)
	}
	if m.blink() {
		s.center(20, "▶  PRESS ENTER  ◀", "yellow", true)
	}
	live, waiting := 0, 0
	for _, t := range r.Tables {
		if t.full() && !t.outcome().Over {
			live++
		}
	}
	waiting = len(r.Challenges)
	status := fmt.Sprintf("%d %s LIVE", live, plural(live, "GAME"))
	if waiting > 0 {
		status += fmt.Sprintf(" · %d %s WAITING", waiting, plural(waiting, "CHALLENGE"))
	}
	s.center(21, status, "cyan", false)
	s.text(1, arcadeH-1, "© CONCORD ARCADE", "dim", "", false)
	x := s.c.Keys(s.ox+s.w-26, s.oy+arcadeH-1, arcade.Key{Key: "Enter", Does: "start"}, arcade.Key{Key: "Esc", Does: "leave"})
	_ = x
}

// spaced writes a tagline with spaces between the letters.
func spaced(t string) string {
	var words []string
	for _, w := range strings.Fields(strings.ToUpper(t)) {
		words = append(words, strings.Join(strings.Split(w, ""), " "))
	}
	return "·  " + strings.Join(words, "   ") + "  ·"
}

func (m *viewerModel) drawMenu(s scr) {
	a := m.k.rules.Arcade
	m.topBar(s, m.where())
	s.logo(a.Title, 3, nil)
	items := m.arcadeMenu()
	m.cursor = min(m.cursor, len(items)-1)
	descW := s.w - 26
	if s.w >= 80 {
		descW = 34
	}
	for i, it := range items {
		y, sel := 9+i, i == m.cursor
		if sel {
			s.text(4, y, "▶", "purple", "", true)
		}
		fg, bg := "fg", ""
		if sel {
			fg, bg = "yellow", "line"
		}
		s.text(6, y, " "+padRight(it.label, 16), fg, bg, sel)
		d, role := it.desc()
		if sel && role == "dim" {
			role = "fg"
		}
		s.text(24, y, cut(d, descW), role, "", role == "yellow")
	}
	// The player's look, on the right.
	if s.w >= 80 && a.Preview != nil && len(a.Unlockables) > 0 {
		kind := a.kinds()[0]
		sel := m.k.equippedFor(m.v.ID)
		s.centerIn(58, 22, 9, "YOUR "+kind.Label, "pink", true)
		a.Preview(s.c, sel[kind.ID], sel, s.ox+59, s.oy+10, 20, 6, false)
	}
	if last := m.room().Last; last != "" {
		s.c.Fill(s.ox, s.oy+18, s.w, "─", "line", "")
		s.text(1, 19, cut("LAST GAME ▸ "+last, s.w-2), "cyan", "", false)
		s.c.Fill(s.ox, s.oy+20, s.w, "─", "line", "")
	}
	if m.notice != "" {
		s.center(21, m.notice, "green", true)
	}
	s.keys(arcade.Key{Key: "↑↓", Does: "choose"}, arcade.Key{Key: "←→", Does: "level"}, arcade.Key{Key: "Enter", Does: "select"}, arcade.Key{Key: "Esc", Does: "title"})
}

func padRight(s string, n int) string {
	if w := arcade.TextWidth(s); w < n {
		return s + strings.Repeat(" ", n-w)
	}
	return s
}

func cut(s string, n int) string {
	r := []rune(s)
	if len(r) > n && n > 1 {
		return string(r[:n-1]) + "…"
	}
	return s
}

// lobbyItems are the lobby's games and challenges (the arcade's menu
// already offers new games and the computer).
func (m *viewerModel) lobbyItems() []lobbyItem {
	items := m.lobby()
	if !m.arcadeOn() {
		return items
	}
	var out []lobbyItem
	for _, it := range items {
		if !it.extra {
			out = append(out, it)
		}
	}
	return out
}

func (m *viewerModel) drawLobby(s scr) {
	r := m.room()
	title := "WATCH · GAMES IN THIS CHANNEL"
	if r.mode() == ModePrivate {
		title = "YOUR GAMES"
	}
	m.topBar(s, "LOBBY")
	s.box(1, 1, s.w-2, 21, "comment", title, "purple")
	items := m.lobbyItems()
	if len(items) == 0 {
		s.center(10, "NOTHING ON YET.", "dim", false)
		s.center(11, "START ONE FROM THE MENU.", "dim", false)
	}
	top := max(0, m.cursor-15)
	for i := top; i < len(items) && i < top+16; i++ {
		y, sel := 3+i-top, i == m.cursor
		label := strings.ToUpper(items[i].label)
		role := "fg"
		if strings.HasPrefix(label, "★") {
			role = "yellow"
		}
		if sel {
			s.text(3, y, "▶", "purple", "", true)
			s.text(5, y, " "+cut(label, s.w-12)+" ", "yellow", "line", true)
		} else {
			s.text(6, y, cut(label, s.w-12), role, "", false)
		}
	}
	if m.notice != "" {
		s.center(20, m.notice, "green", true)
	}
	s.keys(arcade.Key{Key: "↑↓", Does: "choose"}, arcade.Key{Key: "Enter", Does: "open"}, arcade.Key{Key: "D", Does: "decline"}, arcade.Key{Key: "Esc", Does: "back"})
}

func (m *viewerModel) drawPicker(s scr) {
	title := "2 PLAYERS · WHO'S UP FOR IT?"
	if m.room().mode() == ModePrivate {
		title = "NEW GAME · PICK AN OPPONENT"
	}
	m.topBar(s, "LOBBY")
	s.box(1, 1, s.w-2, 21, "comment", title, "purple")
	s.text(5, 3, "IN THIS CHANNEL", "pink", "", true)
	s.text(36, 3, "W-L-D", "pink", "", true)
	people := m.pickable()
	switch {
	case m.members == nil:
		s.text(5, 5, "LOOKING…", "dim", "", false)
	case len(people) == 0:
		s.text(5, 5, "NOBODY ELSE CAN SEE THIS CHANNEL YET.", "dim", "", false)
	}
	m.k.loadRecords()
	top := max(0, m.cursor-7)
	for i := top; i < len(people) && i < top+8; i++ {
		p, y, sel := people[i], 5+(i-top)*2, i == m.cursor
		dot, dotRole := "○", "ghost"
		if p.Online {
			dot, dotRole = "●", "green"
		}
		if sel {
			s.text(3, y, "▶", "purple", "", true)
		}
		s.text(5, y, dot, dotRole, "", false)
		fg, bg := "fg", ""
		if sel {
			fg, bg = "yellow", "line"
		}
		s.text(7, y, " "+padRight(cut(strings.ToUpper(p.DisplayName), 20), 20)+" ", fg, bg, sel)
		if rec := m.k.records[p.UserID]; rec != nil {
			s.text(36, y, fmt.Sprintf("%d-%d-%d", rec.Wins, rec.Losses, rec.Draws), "cyan", "", false)
		}
		if sel {
			verb := "[ CHALLENGE ]"
			if m.room().mode() == ModePrivate {
				verb = "[ PLAY ]"
			}
			s.text(50, y, verb, "green", "", true)
		}
	}
	s.text(5, 21, "SOMEONE ELSEWHERE IN CONCORD GETS A NOTIFICATION.", "dim", "", false)
	s.keys(arcade.Key{Key: "↑↓", Does: "choose"}, arcade.Key{Key: "Enter", Does: "pick"}, arcade.Key{Key: "Esc", Does: "back"})
}

func (m *viewerModel) drawTable(s scr) {
	t := m.table()
	board := m.board.(ArcadeBoard)
	k, a := m.k, m.k.rules.Arcade
	where := "WATCHING"
	if t.hasComputer() {
		where = "VS CPU · " + levelName(t.computerLevel())
	} else if seat := t.seatOf(m.v.ID); seat >= 0 && len(t.Seats) == 2 && !t.Seats[1-seat].Empty() {
		where = "VS " + strings.ToUpper(t.Seats[1-seat].Name)
	}
	m.topBar(s, where)
	over := t.outcome().Over
	turn := -1
	if !over && t.full() {
		turn = t.game.Turn()
	}

	bx, bw := 1, s.w-2
	if s.w >= 80 && len(t.Seats) == 2 {
		bx, bw = 19, s.w-38
		for i := range t.Seats {
			x := 1
			if i == 1 {
				x = s.w - 17
			}
			m.drawSeat(s, board, t, i, x, turn)
		}
		// The score since these players sat down, and the move count.
		score := t.Score
		if len(score) != 2 {
			score = []int{0, 0}
		}
		s.text(5, 13, "SCORE", "pink", "", true)
		s.c.Digit(min(9, score[0]), s.ox+3, 2*(s.oy+14)+1, "pink", 1)
		s.text(8, 16, "-", "dim", "", false)
		s.c.Digit(min(9, score[1]), s.ox+10, 2*(s.oy+14)+1, "cyan", 1)
		s.text(s.w-14, 13, "MOVE", "pink", "", true)
		s.c.Number(len(t.Moves), 2, s.ox+s.w-16, 2*(s.oy+14)+1, "green", 1)
		if n := m.watchers(t); n > 0 {
			s.text(s.w-16, 19, fmt.Sprintf("%d WATCHING", n), "dim", "", false)
		}
	} else {
		var who []string
		for i, p := range t.Seats {
			name := strings.ToUpper(p.Name)
			if p.Empty() {
				name = "OPEN"
			}
			mark := ""
			if i == turn {
				mark = "▸ "
			}
			who = append(who, mark+k.rules.SeatNames[i]+" "+name)
		}
		s.center(1, strings.Join(who, "   "), "fg", false)
	}
	board.Draw(s.c, s.ox+bx, s.oy+2, bw, 18)

	// The status row.
	status, role := board.Status(), "red"
	if over { // the board's own complaint about a finished game reads oddly here
		status = ""
	}
	if status == "" {
		status, role = m.tableStatus(t, turn)
	}
	if m.notice != "" {
		status, role = m.notice, "green"
	}
	s.center(21, strings.ToUpper(status), role, true)

	if m.menuOpen {
		x := s.ox + 1
		for i, it := range m.menu() {
			fg, bg := "fg", ""
			if i == m.menuIdx {
				fg, bg = "yellow", "line"
			}
			x = s.c.Text(x, s.oy+arcadeH-1, " "+strings.ToUpper(it.label)+" ", fg, bg, i == m.menuIdx) + 1
		}
		return
	}
	if over {
		s.keys(arcade.Key{Key: "Enter", Does: "results"}, arcade.Key{Key: "M", Does: "menu"}, arcade.Key{Key: "Esc", Does: "back"})
		return
	}
	keys := append([]arcade.Key(nil), a.Keys...)
	if t.seatOf(m.v.ID) < 0 {
		keys = nil
	}
	s.keys(append(keys, arcade.Key{Key: "M", Does: "menu"}, arcade.Key{Key: "Esc", Does: "back"})...)
}

// drawSeat is one player's panel beside the board.
func (m *viewerModel) drawSeat(s scr, board ArcadeBoard, t *Table, i, x, turn int) {
	p := t.Seats[i]
	active := i == turn
	role := "comment"
	if active {
		role = "purple"
	}
	s.box(x, 2, 16, 10, role, m.k.rules.SeatNames[i], role)
	board.DrawSeat(s.c, i, s.ox+x+1, s.oy+3, 14, 4)
	name, sub := strings.ToUpper(p.Name), ""
	switch {
	case p.Empty():
		name, sub = "OPEN", "M TO SIT"
	case p.Computer:
		name, sub = "CPU", levelName(p.Level)
	default:
		if rec := m.k.records[p.UserID]; rec != nil {
			sub = fmt.Sprintf("%d-%d-%d", rec.Wins, rec.Losses, rec.Draws)
		}
	}
	nameRole := "dim"
	if active {
		nameRole = "fg"
	}
	s.centerIn(x, 16, 7, cut(name, 14), nameRole, true)
	s.centerIn(x, 16, 8, sub, "dim", false)
	switch {
	case active && p.Computer:
		s.centerIn(x, 16, 10, "THINKING…", "orange", true)
	case active && (p.UserID == m.v.ID || t.seatOf(m.v.ID) < 0):
		if m.blink() || m.effects() != "" {
			label := "YOUR TURN"
			if p.UserID != m.v.ID {
				label = "TO MOVE"
			}
			s.centerIn(x, 16, 10, label, "yellow", true)
		}
	case active:
		s.centerIn(x, 16, 10, "TO MOVE", "dim", false)
	}
}

func (m *viewerModel) watchers(t *Table) int {
	n := 0
	for _, vm := range m.k.viewers {
		if vm.channel == m.channel && (vm.screen == screenTable || vm.screen == screenResults) && vm.tableID == t.ID && t.seatOf(vm.v.ID) < 0 {
			n++
		}
	}
	return n
}

func (m *viewerModel) tableStatus(t *Table, turn int) (string, string) {
	mySeat := t.seatOf(m.v.ID)
	switch o := t.outcome(); {
	case o.Over && o.Winner == mySeat && mySeat >= 0:
		return "You win! Enter: results", "green"
	case o.Over && o.Winner >= 0 && mySeat >= 0:
		return "You lose this one. Enter: results", "dim"
	case o.Over && o.Winner >= 0:
		return t.Seats[o.Winner].Name + " wins. Enter: results", "yellow"
	case o.Over:
		return "A draw. Enter: results", "yellow"
	case !t.full():
		if mySeat < 0 {
			return "Waiting for players · M to sit down", "dim"
		}
		return "Waiting for an opponent · M: the computer can play", "dim"
	case turn == mySeat:
		return "Your move", "fg"
	case mySeat < 0:
		return "Watching", "dim"
	}
	return "Waiting for " + t.Seats[turn].Name, "dim"
}

func (m *viewerModel) drawResults(s scr) {
	a, t := m.k.rules.Arcade, m.room().table(m.tableID)
	m.topBar(s, "GAME OVER")
	if t == nil {
		s.center(10, "THAT GAME IS GONE.", "dim", false)
		s.keys(arcade.Key{Key: "Esc", Does: "menu"})
		return
	}
	o := t.outcome()
	headline := "DRAW!"
	if a.Result != nil {
		headline = a.Result(t.game, o, m.k.rules.SeatNames)
	} else if o.Winner >= 0 {
		headline = strings.ToUpper(m.k.rules.SeatNames[o.Winner]) + " WINS!"
	}
	shade := arcade.LogoShade
	if o.Winner >= 0 && !m.blink() {
		shade = []string{"yellow"}
	}
	s.logo(headline, 3, shade)
	drew := a.ResultArt != nil && a.ResultArt(s.c, t.game, o, s.ox+20, s.oy+8, s.w-40, 12, m.frame)
	if !drew {
		if b, ok := m.board.(ArcadeBoard); ok {
			b.Draw(s.c, s.ox+20, s.oy+8, s.w-40, 12)
		}
	}
	mySeat := t.seatOf(m.v.ID)
	s.text(2, 9, "THIS GAME", "pink", "", true)
	line := fmt.Sprintf("A DRAW · %d MOVES", len(t.Moves))
	switch {
	case o.Winner >= 0 && o.Winner == mySeat:
		line = fmt.Sprintf("WON IN %d MOVES", len(t.Moves))
	case o.Winner >= 0 && mySeat >= 0:
		line = fmt.Sprintf("LOST IN %d MOVES", len(t.Moves))
	case o.Winner >= 0:
		line = strings.ToUpper(t.Seats[o.Winner].Name) + " WON"
	}
	s.text(2, 10, cut(line, 17), "fg", "", false)
	if o.Reason != "" {
		s.text(2, 11, cut(strings.ToUpper(o.Reason), 17), "dim", "", false)
	}
	if mySeat >= 0 && len(a.Unlockables) > 0 {
		if n := m.k.record(m.v.ID).Earned; n > 0 {
			role := "yellow"
			if !m.blink() {
				role = "orange"
			}
			s.text(2, 13, fmt.Sprintf("★ +%d %s", n, a.reward(n)), role, "", true)
			s.text(2, 14, "SPEND IN "+a.collection(), "dim", "", false)
		}
	}
	x := s.w - 18
	s.text(x, 9, "REMATCH?", "pink", "", true)
	for i, p := range t.Seats {
		lit := p.Computer || (i < len(t.Ready) && t.Ready[i])
		dot := "ghost"
		if lit {
			dot = "green"
		}
		s.text(x, 10+i, "●", dot, "", false)
		name := strings.ToUpper(p.Name)
		if p.Computer {
			name = "CPU"
		}
		s.text(x+2, 10+i, cut(name, 15), "fg", "", false)
	}
	s.text(x, 11+len(t.Seats), "STARTS WHEN", "dim", "", false)
	s.text(x, 12+len(t.Seats), "ALL ARE LIT", "dim", "", false)
	if mySeat >= 0 {
		s.keys(arcade.Key{Key: "Enter", Does: "rematch"}, arcade.Key{Key: "Esc", Does: "menu"})
	} else {
		s.keys(arcade.Key{Key: "Enter", Does: "keep watching"}, arcade.Key{Key: "Esc", Does: "menu"})
	}
}

func (m *viewerModel) drawSets(s scr) {
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	title := a.collection()
	m.topBar(s, title)
	s.box(1, 1, s.w-2, 21, "comment", title, "purple")
	kinds := a.kinds()
	m.setsRow = min(m.setsRow, len(kinds)-1)
	equipped := m.k.equippedFor(m.v.ID)
	sel := map[string]string{}
	for key, v := range equipped {
		sel[key] = v
	}
	if m.setsIdx == nil {
		m.setsIdx = map[string]int{}
	}
	current := func(kind Kind) (arcade.Unlockable, bool) {
		items := a.ofKind(kind.ID)
		if len(items) == 0 {
			return arcade.Unlockable{}, false
		}
		i, ok := m.setsIdx[kind.ID]
		if !ok {
			i = max(0, slices.IndexFunc(items, func(u arcade.Unlockable) bool { return u.ID == equipped[kind.ID] }))
		}
		return items[min(i, len(items)-1)], true
	}
	for _, kind := range kinds {
		if u, ok := current(kind); ok {
			sel[kind.ID] = u.ID
		}
	}
	focus, _ := current(kinds[m.setsRow])
	owned := rec.Rewards.Owns(focus.ID, a.Unlockables)
	if a.Preview != nil {
		a.Preview(s.c, focus.ID, sel, s.ox+4, s.oy+3, 38, 17, !owned)
	}
	x := 45
	for row, kind := range kinds {
		u, ok := current(kind)
		if !ok {
			continue
		}
		y := 3 + row*3
		isSel := row == m.setsRow
		if isSel {
			s.text(x-2, y, "▶", "purple", "", true)
		}
		labelRole := "pink"
		if isSel {
			labelRole = "yellow"
		}
		s.text(x, y, kind.Label, labelRole, "", true)
		if equipped[kind.ID] == u.ID {
			s.text(s.w-12, y, "✓ IN USE", "green", "", false)
		}
		name, role := "? ? ?", "dim"
		if rec.Rewards.Owns(u.ID, a.Unlockables) {
			name, role = u.Name, "fg"
		}
		end := s.text(x, y+1, "◂ "+cut(name, 18)+" ▸", role, "", true)
		if u.Tier != arcade.Starter {
			s.text(end+1, y+1, u.Tier.Stars(), u.Tier.Role(), "", true)
		}
	}
	y := 3 + len(kinds)*3
	s.text(x, y, focus.Tier.Name(), focus.Tier.Role(), "", true)
	if owned {
		if focus.Blurb != "" {
			s.text(x, y+1, cut("\""+focus.Blurb+"\"", s.w-x-3), "dim", "", false)
		}
	} else {
		s.text(x, y+1, cut("LOCKED: EARN "+a.reward(2), s.w-x-3), "dim", "", false)
	}
	have, all := rec.Rewards.Count(a.Unlockables), len(a.Unlockables)
	s.text(x, 14, a.collection()+" OWNED", "pink", "", true)
	for i := 0; i < all && x+i < s.w-3; i++ {
		ch, role := "▯", "ghost"
		if i < have {
			ch, role = "▮", "purple"
		}
		s.text(x+i, 15, ch, role, "", false)
	}
	s.text(x, 16, fmt.Sprintf("%d OF %d", have, all), "fg", "", false)
	s.text(x, 18, a.reward(2), "pink", "", true)
	passRole := "dim"
	if rec.Rewards.Passes > 0 {
		passRole = "yellow"
	}
	end := s.text(x, 19, fmt.Sprintf("★ %d", rec.Rewards.Passes), passRole, "", true)
	hint := "NONE YET"
	if rec.Rewards.Passes > 0 {
		hint = "ENTER ON A LOCKED ONE"
	}
	s.text(end+1, 19, cut(hint, s.w-end-3), "dim", "", false)
	if m.notice != "" {
		s.center(21, m.notice, "green", true)
	}
	s.keys(arcade.Key{Key: "↑↓", Does: "row"}, arcade.Key{Key: "←→", Does: "change"}, arcade.Key{Key: "Enter", Does: "use / unlock"}, arcade.Key{Key: "Esc", Does: "back"})
}

func (m *viewerModel) drawDraft(s scr) {
	a := m.k.rules.Arcade
	revealing := !m.reveal.IsZero()
	offer := []string{m.picked}
	if !revealing {
		offer = m.offer()
	}
	m.topBar(s, a.reward(1))
	title, shade := "PICK ONE", arcade.LogoShade
	if revealing {
		title = "UNLOCKED!"
		shade = []string{"yellow"}
		if !m.blink() {
			shade = []string{"orange"}
		}
	}
	s.logo(title, 2, shade)
	if len(offer) == 0 {
		s.center(12, "NOTHING TO PICK.", "dim", false)
		return
	}
	m.cursor = min(m.cursor, len(offer)-1)
	cardW := min(26, (s.w-2)/len(offer))
	left := (s.w - cardW*len(offer)) / 2
	equipped := m.k.equippedFor(m.v.ID)
	for i, id := range offer {
		u, _ := a.item(id)
		x := left + i*cardW
		chosen := i == m.cursor || revealing
		role := "comment"
		if chosen {
			role = "purple"
			if revealing {
				role = "yellow"
			}
		}
		s.box(x, 6, cardW-1, 14, role, "", "")
		if chosen && !revealing {
			s.text(x+cardW/2-1, 5, "▼", "purple", "", true)
		}
		s.centerIn(x, cardW-1, 7, strings.TrimSpace(u.Tier.Stars()+" "+u.Tier.Name()), u.Tier.Role(), true)
		if a.Preview != nil {
			sel := map[string]string{}
			for key, v := range equipped {
				sel[key] = v
			}
			sel[u.Kind] = u.ID
			a.Preview(s.c, u.ID, sel, s.ox+x+2, s.oy+8, cardW-5, 6, false)
		}
		nameRole := "dim"
		if chosen {
			nameRole = "fg"
		}
		s.centerIn(x, cardW-1, 15, cut(u.Name, cardW-3), nameRole, true)
		for _, kind := range a.kinds() {
			if kind.ID == u.Kind && len(a.kinds()) > 1 {
				s.centerIn(x, cardW-1, 16, kind.Label, "pink", false)
			}
		}
		s.centerIn(x, cardW-1, 18, cut(u.Blurb, cardW-3), "dim", false)
	}
	if revealing {
		u, _ := a.item(m.picked)
		s.center(21, u.Name+" IS YOURS.", "green", true)
		return
	}
	rec := m.k.record(m.v.ID)
	s.center(21, fmt.Sprintf("SPENDS 1 %s · YOU HAVE %d · THE OFFER STAYS UNTIL YOU PICK", a.reward(1), rec.Rewards.Passes), "dim", false)
	s.keys(arcade.Key{Key: "←→", Does: "choose"}, arcade.Key{Key: "Enter", Does: "unlock"}, arcade.Key{Key: "Esc", Does: "not now"})
}

func (m *viewerModel) drawHOF(s scr) {
	m.topBar(s, "HALL OF FAME")
	s.logo("HALL OF FAME", 3, nil)
	m.k.loadRecords()
	type row struct {
		id  uuid.UUID
		rec *playerRecord
	}
	var rows []row
	for id, r := range m.k.records {
		if r.Games > 0 || r.Wins+r.Losses+r.Draws > 0 {
			rows = append(rows, row{id, r})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].rec.Wins != rows[j].rec.Wins {
			return rows[i].rec.Wins > rows[j].rec.Wins
		}
		return rows[i].rec.Name < rows[j].rec.Name
	})
	x := (s.w - 52) / 2
	s.text(x, 8, "RANK   NAME              WINS   W-L-D       BEST RUN", "pink", "", true)
	ranks := []string{"1ST", "2ND", "3RD", "4TH", "5TH", "6TH", "7TH", "8TH"}
	roles := []string{"yellow", "orange", "red", "pink", "cyan", "green", "fg", "fg"}
	mine := -1
	for i, r := range rows {
		if r.id == m.v.ID {
			mine = i
		}
		if i >= len(ranks) {
			continue
		}
		name := r.rec.Name
		if name == "" {
			name = "?"
		}
		line := fmt.Sprintf("%-6s %-16s  %4d   %-10s  %d", ranks[i], cut(strings.ToUpper(name), 16), r.rec.Wins,
			fmt.Sprintf("%d-%d-%d", r.rec.Wins, r.rec.Losses, r.rec.Draws), r.rec.BestStreak)
		fg, bg := roles[i], ""
		if r.id == m.v.ID {
			fg, bg = "fg", "line"
		}
		s.text(x, 10+i, line, fg, bg, true)
	}
	if len(rows) == 0 {
		s.center(12, "NO GAMES PLAYED YET. BE THE FIRST!", "dim", false)
	}
	if mine >= len(ranks) {
		s.center(19, fmt.Sprintf("YOU: %d OF %d", mine+1, len(rows)), "fg", false)
	}
	s.keys(arcade.Key{Key: "Esc", Does: "back"})
}

func (m *viewerModel) drawHowTo(s scr) {
	a := m.k.rules.Arcade
	m.topBar(s, "HOW TO PLAY")
	s.box(1, 1, s.w-2, 21, "comment", "HOW TO PLAY", "purple")
	y := 3
	for _, l := range a.HowTo {
		if y > 13 {
			break
		}
		s.text(4, y, cut(l, s.w-8), "fg", "", false)
		y++
	}
	y++
	s.text(4, y, "KEYS", "pink", "", true)
	y++
	keys := append(append([]arcade.Key(nil), a.Keys...),
		arcade.Key{Key: "M", Does: "the table menu: resign, rematch, stand up"},
		arcade.Key{Key: "Esc", Does: "back one screen (on the title: leave the game)"})
	keyW := 0
	for _, key := range keys {
		keyW = max(keyW, arcade.TextWidth(key.Key))
	}
	for _, key := range keys {
		if y > 20 {
			break
		}
		key.Key = padRight(key.Key, keyW) // the descriptions line up
		s.c.Keys(s.ox+4, s.oy+y, key)
		y++
	}
	s.keys(arcade.Key{Key: "Esc", Does: "back"})
}

func (m *viewerModel) drawOptions(s scr) {
	a, rec := m.k.rules.Arcade, m.k.record(m.v.ID)
	m.topBar(s, "OPTIONS")
	s.logo("OPTIONS", 3, nil)
	type opt struct{ label, value, help string }
	var opts []opt
	if a.Sounds {
		opts = append(opts, opt{"SOUND", onOff(!rec.Muted), "MENU BLIPS AND THE GAME'S SOUNDS, FOR YOU"})
	}
	opts = append(opts, opt{"EFFECTS", effectsName(rec.Effects), "FULL: BLINKING AND ATTRACT MODE · CALM: STILL · OFF"})
	for i, o := range opts {
		y, sel := 9+i*3, i == m.cursor
		if sel {
			s.text(20, y, "▶", "purple", "", true)
		}
		role := "pink"
		if sel {
			role = "yellow"
		}
		s.text(22, y, o.label, role, "", true)
		s.text(34, y, "◂ "+o.value+" ▸", "fg", "", true)
		s.text(22, y+1, o.help, "dim", "", false)
	}
	s.center(18, "THESE ARE YOURS: EVERYONE ELSE KEEPS THEIR OWN.", "dim", false)
	s.keys(arcade.Key{Key: "↑↓", Does: "choose"}, arcade.Key{Key: "←→", Does: "change"}, arcade.Key{Key: "Esc", Does: "back"})
}

// newRand is the kit's random source for offers.
func newRand() *rand.Rand {
	return rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), 0x9e3779b97f4a7c15))
}
