package table

import (
	"time"

	"github.com/google/uuid"
)

// People who walk away from a seats channel's table. Someone seated there
// who isn't viewing the channel (they left it, closed Concord, or the
// plugin restarted without them coming back) for AwayFor gives up the seat,
// so one person going home can't keep everyone else from playing:
//
//   - no game going (waiting for players, or finished): they stand up;
//   - a game going at a two-seat table: they forfeit ("left the table"),
//     and the seat opens at the next check;
//   - a game going with more seats: the computer takes their seat, so the
//     others can finish.
//
// Only the channel's own table (Table.Main) is swept: games against the
// computer, challenges and private games keep their seats, since nobody else
// is waiting for them.

// AwayFor is how long a seated player may be gone from a seats channel.
var AwayFor = 10 * time.Minute

// present reports whether a user is viewing the room's channel now.
func (k *Kit) present(r *Room, id uuid.UUID) bool {
	vm := k.viewers[id]
	return vm != nil && vm.channel == r.ChannelID
}

// mainTable is the room's own table, if it's a seats channel and has one.
func (r *Room) mainTable() *Table {
	if r.mode() != ModeSeats {
		return nil
	}
	for _, t := range r.Tables {
		if t.Main {
			return t
		}
	}
	return nil
}

// noteAway starts the clock on everyone seated at r's table who isn't here,
// and checks again once the first of them could be out of time.
func (k *Kit) noteAway(r *Room) {
	t := r.mainTable()
	if t == nil {
		return
	}
	if r.away == nil {
		r.away = map[uuid.UUID]time.Time{}
	}
	pending := false
	for _, p := range t.Seats {
		if p.Empty() || p.Computer {
			continue
		}
		if k.present(r, p.UserID) {
			delete(r.away, p.UserID)
			continue
		}
		if _, ok := r.away[p.UserID]; !ok {
			r.away[p.UserID] = time.Now()
		}
		pending = true
	}
	if pending && !r.sweeping {
		r.sweeping = true
		channelID := r.ChannelID
		time.AfterFunc(AwayFor/4, func() {
			k.post(func() {
				if r := k.rooms[channelID]; r != nil {
					r.sweeping = false
					k.sweepAway(r)
				}
			})
		})
	}
}

// sweepAway frees the seats of anyone gone longer than AwayFor, then keeps
// watching whoever is still away.
func (k *Kit) sweepAway(r *Room) {
	t := r.mainTable()
	if t == nil {
		return
	}
	for s, p := range t.Seats {
		if p.Empty() || p.Computer || k.present(r, p.UserID) {
			continue
		}
		if since, ok := r.away[p.UserID]; ok && time.Since(since) >= AwayFor {
			delete(r.away, p.UserID)
			k.leaveSeat(r, t, s)
		}
	}
	k.noteAway(r)
}

// leaveSeat takes seat s away from a player who walked off.
func (k *Kit) leaveSeat(r *Room, t *Table, s int) {
	playing := t.full() && !t.outcome().Over && len(t.Moves) > 0
	switch {
	case !playing:
		t.Seats[s] = Player{}
		t.Score, t.Ready = nil, nil
		if t.outcome().Over || len(t.Moves) > 0 { // a finished game makes room for the next one
			t.Moves, t.Resigned, t.Away = nil, -1, false
			t.Options = k.seed(t.Options)
			t.rebuild(&k.rules)
			k.resetClock(t)
		}
		k.changed(r, t, "")
	case len(t.Seats) > 2:
		level := 2
		if t.hasComputer() {
			level = t.computerLevel() // the strength already at the table
		}
		t.Seats[s] = k.computerPlayer(level)
		k.changed(r, t, "")
		k.afterMove(r, t, -1) // it may be the computer's turn now
	default:
		t.stopClock()
		t.Resigned, t.Away = s, true
		k.reportResult(r, t)
		k.changed(r, t, "")
		// The game's over, so the next check stands them up: the others see
		// the result first.
		r.away[t.Seats[s].UserID] = time.Now().Add(-AwayFor)
	}
}
