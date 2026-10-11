package table

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	mrand "math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// Seating modes -- a channel's "seating" setting (declare it in your
// manifest as a select create_field with these options).
const (
	// ModeSeats: one table in the channel. Viewers sit in open seats; the
	// game starts when every seat is filled; everyone else watches.
	ModeSeats = "seats"
	// ModeChallenge: a lobby. Members challenge each other (Concord
	// notifies them); accepted challenges become tables anyone can watch.
	ModeChallenge = "challenge"
	// ModePrivate: each member has their own games against chosen
	// opponents; only the two players can see a game.
	ModePrivate = "private"
)

// Channel setting keys the kit reads (alongside the game's own options).
const (
	SettingSeating    = "seating"           // ModeSeats (default) | ModeChallenge | ModePrivate
	SettingSpectators = "allow_spectators"  // "false" hides games from non-players (default "true")
	SettingComputer   = "computer_opponent" // "false" disables playing the computer (default "true" when Rules.AI is set)
	SettingLevel      = "computer_level"    // "easy" | "normal" (default) | "hard", or "1".."3"
)

// Room is one channel's games.
type Room struct {
	ChannelID  uuid.UUID         `json:"channel_id"`
	Tables     []*Table          `json:"tables"`
	Challenges []Challenge       `json:"challenges"`
	Last       string            `json:"last,omitempty"` // the last finished game, for the arcade menu
	settings   map[string]string // the channel's create_field values
}

// Challenge is an invitation waiting for an answer.
type Challenge struct {
	ID   string    `json:"id"`
	From Player    `json:"from"`
	To   Player    `json:"to"`
	At   time.Time `json:"at"`
	// Options are the game's options the challenger chose (Arcade.Options).
	Options map[string]string `json:"options,omitempty"`
}

func (r *Room) mode() string {
	switch r.settings[SettingSeating] {
	case ModeChallenge, ModePrivate:
		return r.settings[SettingSeating]
	}
	return ModeSeats
}

func (r *Room) spectators() bool {
	return r.mode() != ModePrivate && r.settings[SettingSpectators] != "false"
}

func (r *Room) table(id string) *Table {
	for _, t := range r.Tables {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// Kit runs a game as a Concord plugin. Create it with New and hand its
// Handler to plugin.Run.
type Kit struct {
	rules   Rules
	host    *pane.Host
	conn    *plugin.Conn
	rooms   map[uuid.UUID]*Room
	viewers map[uuid.UUID]*viewerModel
	dirty   map[uuid.UUID]*Room         // rooms changed during the current event
	moved   map[string]string           // table id -> move just played, for ChangedMsg
	records map[uuid.UUID]*playerRecord // every player's record (records.go), loaded on first use

	ticking bool // the arcade animation clock is running (arcade.go)
	tickGen int
	rnd     *mrand.Rand // offers of unlockables
}

// New makes a Kit for a game.
func New(rules Rules) *Kit {
	k := &Kit{
		rules:   rules,
		rooms:   map[uuid.UUID]*Room{},
		viewers: map[uuid.UUID]*viewerModel{},
		dirty:   map[uuid.UUID]*Room{},
		moved:   map[string]string{},
		rnd:     newRand(),
	}
	k.host = pane.NewHost(func(v *pane.Viewer) tea.Model { return k.newViewer(v) })
	return k
}

// Handler returns the plugin callbacks that run the game. Every change made
// while handling an event is saved and shown to affected viewers once the
// event is done.
func (k *Kit) Handler() plugin.Handler {
	h := k.host.Handler()
	wrap := func(c *plugin.Conn, fn func()) { k.conn = c; fn(); k.flush() }
	return plugin.Handler{
		OnReady: func(c *plugin.Conn, _ *wire.User) { k.conn = c },
		OnEnter: func(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
			wrap(c, func() { k.room(e.ChannelID); h.OnEnter(c, e) })
		},
		OnInput:  func(c *plugin.Conn, e wire.PluginPaneInputPayload) { wrap(c, func() { h.OnInput(c, e) }) },
		OnResize: func(c *plugin.Conn, e wire.PluginPaneResizePayload) { wrap(c, func() { h.OnResize(c, e) }) },
		OnLeave: func(c *plugin.Conn, e wire.PluginPaneLeavePayload) {
			wrap(c, func() { h.OnLeave(c, e); delete(k.viewers, e.ViewerID) })
		},
		OnChannel: func(c *plugin.Conn, ch wire.Channel) {
			wrap(c, func() {
				r := k.room(ch.ID)
				r.settings = ch.PluginConfig
				k.dirty[ch.ID] = r
			})
		},
		OnChannelDelete: func(c *plugin.Conn, e wire.ChannelDeletePayload) {
			k.conn = c
			delete(k.rooms, e.ChannelID)
			_ = os.Remove(k.roomFile(e.ChannelID))
		},
	}
}

// Post runs fn on the event loop and then shows its changes -- for work
// that finished in the background.
func (k *Kit) post(fn func()) {
	if k.conn == nil {
		return
	}
	k.conn.Post(func() { fn(); k.flush() })
}

// room returns (loading if needed) the room for a channel.
func (k *Kit) room(channelID uuid.UUID) *Room {
	if r := k.rooms[channelID]; r != nil {
		return r
	}
	r := &Room{ChannelID: channelID, settings: map[string]string{}}
	if data, err := os.ReadFile(k.roomFile(channelID)); err == nil {
		if err := json.Unmarshal(data, r); err != nil {
			log.Printf("table: ignoring unreadable saved games for %s: %v", channelID, err)
			r = &Room{ChannelID: channelID}
		}
		r.settings = map[string]string{}
	}
	for _, t := range r.Tables {
		t.rebuild(&k.rules)
	}
	k.rooms[channelID] = r
	return r
}

func (k *Kit) roomFile(channelID uuid.UUID) string {
	dir := "."
	if k.conn != nil && k.conn.DataDir() != "" {
		dir = k.conn.DataDir()
	}
	return filepath.Join(dir, "tables", channelID.String()+".json")
}

func (k *Kit) save(r *Room) {
	path := k.roomFile(r.ChannelID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("table: can't save games: %v", err)
		return
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// changed marks a room as needing a save and a redraw; move is the move
// just played at table t, if any.
func (k *Kit) changed(r *Room, t *Table, move string) {
	k.dirty[r.ChannelID] = r
	if t != nil {
		t.UpdatedAt = time.Now()
		k.moved[t.ID] = move
	}
}

// flush saves changed rooms and redraws everyone viewing them.
func (k *Kit) flush() {
	for id, r := range k.dirty {
		delete(k.dirty, id)
		k.save(r)
		moved := k.moved
		k.moved = map[string]string{}
		k.host.Broadcast(id, roomChangedMsg{moved: moved})
	}
}

// roomChangedMsg tells every viewer of a room to redraw; moved lists the
// tables whose games changed (and the move, if any).
type roomChangedMsg struct{ moved map[string]string }

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (k *Kit) newTable(r *Room, seats []Player) *Table { return k.newTableWith(r, seats, nil) }

// newTableWith starts a table whose options are the channel's settings with
// opts (a player's choices on the NEW GAME screen) over them.
func (k *Kit) newTableWith(r *Room, seats []Player, opts map[string]string) *Table {
	t := &Table{
		ID: newID(), Seats: seats, Resigned: -1, Options: withOptions(r.settings, opts),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	t.rebuild(&k.rules)
	r.Tables = append(r.Tables, t)
	k.changed(r, t, "")
	return t
}

// withOptions is settings with opts laid over them.
func withOptions(settings, opts map[string]string) map[string]string {
	out := copyMap(settings)
	for k, v := range opts {
		out[k] = v
	}
	return out
}

func copyMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ── Actions (all run on the event loop) ─────────────────────────────────────

// play makes a move for seat at table t.
func (k *Kit) play(r *Room, t *Table, seat int, move string) error {
	if !t.full() {
		return fmt.Errorf("waiting for every seat to be filled")
	}
	if t.outcome().Over {
		return fmt.Errorf("this game is over")
	}
	if seat < 0 || t.game.Turn() != seat {
		return fmt.Errorf("it's not your turn")
	}
	if err := t.game.Play(move); err != nil {
		return err
	}
	t.Moves = append(t.Moves, move)
	k.changed(r, t, move)
	k.moveSound(r, t, move)
	k.afterMove(r, t, seat)
	return nil
}

// afterMove starts the computer if it's next, or tells the next player.
func (k *Kit) afterMove(r *Room, t *Table, mover int) {
	if t.outcome().Over {
		k.reportResult(r, t)
		return
	}
	next := t.game.Turn()
	if next < 0 || next >= len(t.Seats) {
		return
	}
	p := t.Seats[next]
	if p.Computer {
		k.startComputer(r, t)
		return
	}
	if k.conn != nil && !k.watching(p.UserID, t.ID) && mover >= 0 {
		_ = k.conn.NotifyUser(p.UserID, r.ChannelID, fmt.Sprintf("%s moved — your turn in %s", t.Seats[mover].Name, k.rules.Name))
	}
}

// watching reports whether userID has table tableID open right now.
func (k *Kit) watching(userID uuid.UUID, tableID string) bool {
	vm := k.viewers[userID]
	return vm != nil && vm.screen == screenTable && vm.tableID == tableID
}

// startComputer works out the computer's move in the background, on a
// replayed copy of the game, and plays it when it's ready (if the game
// hasn't moved on meanwhile).
func (k *Kit) startComputer(r *Room, t *Table) {
	if k.rules.AI == nil || k.conn == nil {
		return
	}
	seat := t.game.Turn()
	level := t.Seats[seat].Level
	moves := append([]string(nil), t.Moves...)
	options := copyMap(t.Options)
	channelID, tableID := r.ChannelID, t.ID
	go func() {
		g := k.rules.New(options)
		for _, m := range moves {
			if g.Play(m) != nil {
				return
			}
		}
		move := k.rules.AI(g, level)
		k.post(func() {
			r := k.room(channelID)
			t := r.table(tableID)
			if t == nil || len(t.Moves) != len(moves) {
				return // resigned, rematched or deleted while thinking
			}
			if err := k.play(r, t, seat, move); err != nil {
				log.Printf("table: computer's move %q rejected: %v", move, err)
			}
		})
	}()
}

func (k *Kit) sit(r *Room, t *Table, seat int, p Player) {
	if seat < 0 || seat >= len(t.Seats) || !t.Seats[seat].Empty() || t.seatOf(p.UserID) >= 0 {
		return
	}
	t.Seats[seat] = p
	t.Score, t.Ready = nil, nil // new players, a new score
	k.changed(r, t, "")
	if t.full() {
		k.afterMove(r, t, -1) // the computer may be first to move
	}
}

func (k *Kit) stand(r *Room, t *Table, userID uuid.UUID) {
	if s := t.seatOf(userID); s >= 0 && (!t.full() || t.outcome().Over) {
		t.Seats[s] = Player{}
		t.Score, t.Ready = nil, nil
		if t.outcome().Over { // a finished game makes room for the next one
			t.Moves, t.Resigned = nil, -1
			t.rebuild(&k.rules)
		}
		k.changed(r, t, "")
	}
}

func (k *Kit) resign(r *Room, t *Table, seat int) {
	if seat >= 0 && t.full() && !t.outcome().Over {
		t.Resigned = seat
		k.reportResult(r, t)
		k.changed(r, t, "")
	}
}

// rematch starts a new game with the same players, seats swapped (so the
// other player moves first).
func (k *Kit) rematch(r *Room, t *Table) {
	if !t.outcome().Over {
		return
	}
	seats := append([]Player(nil), t.Seats...)
	if len(seats) == 2 {
		seats[0], seats[1] = seats[1], seats[0]
		if len(t.Score) == 2 { // the score follows the players
			t.Score[0], t.Score[1] = t.Score[1], t.Score[0]
		}
	}
	t.Seats, t.Moves, t.Resigned, t.Ready = seats, nil, -1, nil
	if !k.gameOptions() { // a game with its own options keeps the ones chosen for this table
		t.Options = copyMap(r.settings)
	}
	t.rebuild(&k.rules)
	k.changed(r, t, "")
	k.afterMove(r, t, -1)
}

func (k *Kit) computerPlayer(level int) Player {
	names := []string{"", "Computer (easy)", "Computer", "Computer (hard)"}
	if level < 1 || level > 3 {
		level = 2
	}
	return Player{Name: names[level], Computer: true, Level: level}
}

// computerLevel is the channel's computer strength, 1-3 (default 2).
func (r *Room) computerLevel() int {
	switch strings.ToLower(strings.TrimSpace(r.settings[SettingLevel])) {
	case "easy", "1":
		return 1
	case "hard", "3":
		return 3
	}
	return 2
}

func (k *Kit) computerAllowed(r *Room) bool {
	return k.rules.AI != nil && r.settings[SettingComputer] != "false"
}

func (k *Kit) challenge(r *Room, from, to Player) { k.challengeWith(r, from, to, nil) }

func (k *Kit) challengeWith(r *Room, from, to Player, opts map[string]string) {
	for _, c := range r.Challenges {
		if c.From.UserID == from.UserID && c.To.UserID == to.UserID {
			return
		}
	}
	r.Challenges = append(r.Challenges, Challenge{ID: newID(), From: from, To: to, At: time.Now(), Options: opts})
	k.changed(r, nil, "")
	if k.conn != nil {
		_ = k.conn.NotifyUser(to.UserID, r.ChannelID, fmt.Sprintf("%s challenged you to %s%s", from.Name, k.rules.Name, k.optionsNote(opts)))
	}
}

// answer accepts or declines a challenge (or its sender cancels it).
func (k *Kit) answer(r *Room, id string, accept bool) *Table {
	for i, c := range r.Challenges {
		if c.ID != id {
			continue
		}
		r.Challenges = append(r.Challenges[:i], r.Challenges[i+1:]...)
		k.changed(r, nil, "")
		if !accept {
			return nil
		}
		t := k.newTableWith(r, []Player{c.From, c.To}, c.Options)
		if k.conn != nil {
			_ = k.conn.NotifyUser(c.From.UserID, r.ChannelID, fmt.Sprintf("%s accepted your %s challenge", c.To.Name, k.rules.Name))
		}
		k.afterMove(r, t, -1)
		return t
	}
	return nil
}

// startGame opens a new two-player table directly (private games, and
// playing the computer), notifying a human opponent.
func (k *Kit) startGame(r *Room, me, opponent Player) *Table {
	return k.startGameWith(r, me, opponent, nil)
}

func (k *Kit) startGameWith(r *Room, me, opponent Player, opts map[string]string) *Table {
	t := k.newTableWith(r, []Player{me, opponent}, opts)
	if !opponent.Computer && k.conn != nil {
		_ = k.conn.NotifyUser(opponent.UserID, r.ChannelID, fmt.Sprintf("%s started a game of %s with you", me.Name, k.rules.Name))
	}
	k.afterMove(r, t, -1)
	return t
}

// seatsTable is ModeSeats' one table, created on first use.
// Other tables in a seats room are games against the computer, started
// from the arcade menu. (Games saved before Main existed: the first table
// was the only one.)
func (k *Kit) seatsTable(r *Room) *Table {
	for _, t := range r.Tables {
		if t.Main {
			return t
		}
	}
	if len(r.Tables) > 0 {
		r.Tables[0].Main = true
		return r.Tables[0]
	}
	t := k.newTable(r, make([]Player, len(k.rules.SeatNames)))
	t.Main = true
	return t
}

// requestMembers looks up who can see the channel, in the background.
func (k *Kit) requestMembers(channelID uuid.UUID, then func([]wire.PluginMember)) {
	if k.conn == nil {
		return
	}
	conn := k.conn
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		members, err := conn.RequestMembers(ctx, channelID)
		if err != nil {
			log.Printf("table: members lookup failed: %v", err)
			return
		}
		k.post(func() { then(members) })
	}()
}

// moveSound plays Rules.Sound's choice for everyone watching table t.
func (k *Kit) moveSound(r *Room, t *Table, move string) {
	if k.rules.Sound == nil || k.conn == nil {
		return
	}
	sound := k.rules.Sound(t.game, move)
	if sound == "" {
		return
	}
	for id := range k.viewers {
		if k.watching(id, t.ID) && !(k.rules.Arcade != nil && k.record(id).Muted) {
			_ = k.conn.PlaySound(r.ChannelID, id, sound, 1)
		}
	}
}

// reportResult tells each person at a finished table how it went for them,
// so their client can count the win (or loss) towards its achievements.
func (k *Kit) reportResult(r *Room, t *Table) {
	o := t.outcome()
	if k.conn == nil || !o.Over {
		return
	}
	if o.Winner >= 0 && o.Winner < len(t.Seats) {
		if len(t.Score) != len(t.Seats) {
			t.Score = make([]int, len(t.Seats))
		}
		t.Score[o.Winner]++
	}
	k.recordResult(t, o) // records and achievements (records.go)
	if t.full() {
		r.Last = k.lastResult(t, o)
	}
	computer := false
	for _, p := range t.Seats {
		computer = computer || p.Computer
	}
	for i, p := range t.Seats {
		if p.Computer || p.UserID == uuid.Nil {
			continue
		}
		result := "draw"
		switch {
		case o.Winner == i:
			result = "win"
		case o.Winner >= 0:
			result = "loss"
		}
		_ = k.conn.Event(wire.PluginEventGameResult, p.UserID, wire.PluginGameResultPayload{
			Game: k.rules.Name, Result: result, Computer: computer, Reason: o.Reason})
	}
}
