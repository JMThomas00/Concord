package table

import (
	"fmt"
	"time"
)

// Clocks. A game with Rules.Clock gives each seat a time allowance from its
// options. Nobody's clock runs until the first move; after that the side to
// move's clock runs, a move takes off the time it took and adds the
// increment, and running out is a loss (Outcome.Reason "time"). The kit
// keeps the time on the Table, so it's saved with the game, and checks
// for a flag with a timer of its own, so it happens even when nobody is
// watching. Clocks need a Concord connection: standalone games have none.

// clock is the table's time control (zero base: no clock).
func (k *Kit) clock(t *Table) (base, inc time.Duration) {
	if k.rules.Clock == nil || k.conn == nil {
		return 0, 0
	}
	return k.rules.Clock(t.Options)
}

// resetClock sets every seat's clock back to the start, stopped.
func (k *Kit) resetClock(t *Table) {
	t.Left, t.TurnAt, t.OutOfTime = nil, time.Time{}, 0
	if base, _ := k.clock(t); base > 0 {
		t.Left = make([]time.Duration, len(t.Seats))
		for i := range t.Left {
			t.Left[i] = base
		}
	}
}

// running reports whether the side to move's clock is running.
func (t *Table) running() bool {
	return len(t.Left) > 0 && !t.TurnAt.IsZero() && !t.result().Over
}

// left is seat's time left now.
func (t *Table) left(seat int) time.Duration {
	if seat < 0 || seat >= len(t.Left) {
		return 0
	}
	d := t.Left[seat]
	if t.running() && t.game != nil && t.game.Turn() == seat {
		d -= time.Since(t.TurnAt)
	}
	return max(0, d)
}

// spend stops the running clock, taking the time since it started off
// seat's allowance (a game just ended by this move still pays for it).
func (t *Table) spend(seat int) {
	if len(t.Left) == 0 || t.TurnAt.IsZero() || seat < 0 || seat >= len(t.Left) {
		return
	}
	t.Left[seat] = max(0, t.Left[seat]-time.Since(t.TurnAt))
	t.TurnAt = time.Time{}
}

// flag ends the game: seat ran out of time.
func (t *Table) flag(seat int) {
	t.spend(seat)
	t.Left[seat] = 0
	t.OutOfTime = seat + 1
}

// outOfTime reports whether seat, to move, has no time left.
func (t *Table) outOfTime(seat int) bool { return t.running() && t.left(seat) <= 0 }

// clockMoved runs after seat's move: the increment, then the next side's
// clock starts (unless the game is over).
func (k *Kit) clockMoved(r *Room, t *Table, seat int) {
	if len(t.Left) == 0 {
		return
	}
	_, inc := k.clock(t)
	if seat >= 0 && seat < len(t.Left) && len(t.Moves) > 1 { // the first move starts the clocks, so it earns nothing
		t.Left[seat] += inc
	}
	if t.outcome().Over {
		t.TurnAt = time.Time{}
		return
	}
	t.TurnAt = time.Now()
	k.watchFlag(r, t)
}

// stopClock freezes the clocks where they are (a resignation).
func (t *Table) stopClock() {
	if t.running() {
		t.spend(t.game.Turn())
	}
}

// watchFlag checks for the side to move running out of time once their
// time is up. A check that finds time left (they moved meanwhile, or a
// restart moved TurnAt) does nothing, so stale timers are harmless.
func (k *Kit) watchFlag(r *Room, t *Table) {
	if !t.running() || k.conn == nil {
		return
	}
	seat := t.game.Turn()
	channelID, tableID := r.ChannelID, t.ID
	time.AfterFunc(t.left(seat)+50*time.Millisecond, func() {
		k.post(func() {
			r := k.room(channelID)
			t := r.table(tableID)
			if t == nil || t.game.Turn() != seat || !t.outOfTime(seat) {
				return
			}
			t.flag(seat)
			k.reportResult(r, t)
			k.changed(r, t, "")
		})
	})
}

// resumeClocks restarts the clocks of a room just loaded from disk: the
// time the plugin was stopped isn't charged to anyone.
func (k *Kit) resumeClocks(r *Room) {
	for _, t := range r.Tables {
		if t.running() {
			t.TurnAt = time.Now()
			k.watchFlag(r, t)
		}
	}
}

// Clock reports seat i's time left, and whether it's running now; ok is
// false in a game without a clock.
func (s *Seat) Clock(i int) (left time.Duration, running, ok bool) {
	if len(s.table.Left) == 0 {
		return 0, false, false
	}
	return s.table.left(i), s.table.running() && s.table.game.Turn() == i, true
}

// ClockText shows a time left the way a chess clock does: 4:58, or 0:09.4
// in the last ten seconds.
func ClockText(d time.Duration) string {
	d = max(0, d)
	if d < 10*time.Second {
		return fmt.Sprintf("0:%02d.%d", int(d/time.Second), int(d%time.Second/(100*time.Millisecond)))
	}
	s := int((d + time.Second - 1) / time.Second)
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
