package table

import (
	"math/rand/v2"
	"strconv"
	"strings"
)

// Games for more than two (Rules.MinSeats, more than two SeatNames) and
// games that deal (Rules.Seeded).
//
// Whoever starts a game chooses on the NEW GAME screen how many play and
// what fills each other seat: the computer at a level, or a person. A
// person seat is offered only where people can sit down (a seats channel's
// own table); elsewhere every other seat is the computer, except the one
// person invited by a challenge or a private game. The choices travel in
// the game's options like any other (OptionPlayers, OptionSeat), so they're
// remembered, carried by challenges and kept by rematches.
//
// With more than two seats the arcade table hands the board the whole
// width: the board draws the players around it (Seat.Players, MyTurn).

// seatValues are what can fill a seat: the computer at each level, and a
// person when people can sit down.
var seatValues = []string{"cpu1", "cpu2", "cpu3", "person"}

// many reports whether the NEW GAME screen asks about seats.
func (k *Kit) many() bool {
	return len(k.rules.SeatNames) > 2 || (k.rules.MinSeats > 0 && k.rules.MinSeats < len(k.rules.SeatNames))
}

// seatOptions are the kit's own NEW GAME rows: how many play, and what
// fills each other seat. people offers person seats.
func (k *Kit) seatOptions(people bool) []Option {
	n := len(k.rules.SeatNames)
	var out []Option
	if k.rules.MinSeats > 0 && k.rules.MinSeats < n {
		var values, names []string
		for i := k.rules.MinSeats; i <= n; i++ {
			values = append(values, strconv.Itoa(i))
			names = append(names, strconv.Itoa(i)+" PLAYERS")
		}
		out = append(out, Option{Key: OptionPlayers, Label: "PLAYERS", Values: values, Names: names, Default: strconv.Itoa(n)})
	}
	if n <= 2 {
		return out
	}
	values, names := seatValues[:3], []string{"CPU · EASY", "CPU · NORMAL", "CPU · HARD", "A PERSON"}
	if people {
		values = seatValues
	}
	for i := 1; i < n; i++ {
		i := i
		out = append(out, Option{
			Key: OptionSeat(i), Label: "SEAT " + strconv.Itoa(i+1), Values: values, Names: names[:len(values)], Default: "cpu2",
			Describe: func(v string) string {
				if v == "person" {
					return "SOMEONE SITS DOWN HERE"
				}
				return "THE COMPUTER PLAYS HERE"
			},
			Show: func(o map[string]string) bool { return i < k.rules.Seats(o) },
		})
	}
	return out
}

// setupOptions are every NEW GAME row, the kit's first.
func (k *Kit) setupOptions(people bool) []Option {
	opts := k.seatOptions(people)
	if k.rules.Arcade != nil {
		opts = append(opts, k.rules.Arcade.Options...)
	}
	return opts
}

// shown are the options the choices so far call for.
func shown(opts []Option, chosen map[string]string) []Option {
	var out []Option
	for _, o := range opts {
		if o.Show == nil || o.Show(chosen) {
			out = append(out, o)
		}
	}
	return out
}

// seatLevel is the computer level an OptionSeat value asks for (0: a person).
func seatLevel(v string, fallback int) int {
	switch v {
	case "person":
		return 0
	case "cpu1", "cpu2", "cpu3":
		return int(v[3] - '0')
	}
	return fallback
}

// seatsFor lays out a new table's seats: the people given first (the
// starter, and anyone invited), then each other seat as its option says --
// the computer, or open for a person when people can sit down (otherwise the
// computer at level).
func (k *Kit) seatsFor(opts map[string]string, people []Player, open bool, level int) []Player {
	seats := make([]Player, k.rules.Seats(opts))
	copy(seats, people)
	for i := len(people); i < len(seats); i++ {
		v, chosen := opts[OptionSeat(i)]
		if !chosen && open {
			continue // a game that doesn't ask: open, as it always was
		}
		switch l := seatLevel(v, level); {
		case l == 0 && open:
			// left open for someone to sit down
		case l == 0:
			seats[i] = k.computerPlayer(level)
		default:
			seats[i] = k.computerPlayer(l)
		}
	}
	return seats
}

// seed gives a dealing game a new random seed (Rules.Seeded).
func (k *Kit) seed(opts map[string]string) map[string]string {
	if !k.rules.Seeded {
		return opts
	}
	if opts == nil {
		opts = map[string]string{}
	}
	opts[OptionSeed] = strconv.FormatUint(rand.Uint64()>>1, 10)
	return opts
}

// seatsSummary is "4 PLAYERS" for a table of more than two, or "".
func seatsSummary(t *Table) string {
	if len(t.Seats) <= 2 {
		return ""
	}
	return strconv.Itoa(len(t.Seats)) + " PLAYERS"
}

// teamOf names the winners for a status line: "alice", or "alice and carol".
func teamOf(t *Table, o Outcome) string {
	names := []string{t.Seats[o.Winner].Name}
	for _, s := range o.Team {
		if s != o.Winner && s >= 0 && s < len(t.Seats) {
			names = append(names, t.Seats[s].Name)
		}
	}
	return strings.Join(names, " and ")
}

// teamWins is "alice wins", or "alice and carol win".
func teamWins(t *Table, o Outcome) string {
	if len(o.Team) > 1 {
		return teamOf(t, o) + " win"
	}
	return teamOf(t, o) + " wins"
}
