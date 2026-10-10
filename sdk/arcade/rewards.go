package arcade

import (
	"math/rand/v2"
	"slices"
)

// Tier is how rare an unlockable is.
type Tier int

const (
	Starter Tier = iota // owned from the start
	Common
	Rare
	Legendary
)

// Name is the tier as players see it.
func (t Tier) Name() string {
	return [...]string{"STARTER", "COMMON", "RARE", "LEGENDARY"}[t]
}

// Stars is the tier's star rating: none, ★, ★★ or ★★★.
func (t Tier) Stars() string { return [...]string{"", "★", "★★", "★★★"}[t] }

// Role is the colour role the tier is shown in.
func (t Tier) Role() string { return [...]string{"dim", "fg", "cyan", "yellow"}[t] }

// weight is how often the tier turns up in an offer.
func (t Tier) weight() int { return [...]int{0, 6, 3, 1}[t] }

// Unlockable is something a player can earn: a car, a piece set, a board.
type Unlockable struct {
	ID   string
	Name string
	Tier Tier
	// Kind groups items a player picks one of ("pieces", "board"); a game
	// with a single kind (Grape Race's cars) can leave it empty.
	Kind string
	// Blurb is a line about it, shown when it's offered.
	Blurb string
}

// OfferSize is how many locked items a pass offers.
const OfferSize = 3

// Rewards is one player's passes and what they've unlocked. Each game
// names its passes to suit it -- Pit Passes in Grape Race, Gold Stars in
// tic-tac-toe -- and only the name on screen differs. Keep it
// in the player's saved record: the offer is saved with it, so leaving and
// coming back shows the same choices (no re-rolling).
type Rewards struct {
	Passes int      `json:"passes"`
	Owned  []string `json:"owned,omitempty"`
	Offer  []string `json:"offer,omitempty"`
}

// Owns reports whether id is unlocked (starters always are).
func (r *Rewards) Owns(id string, items []Unlockable) bool {
	for _, it := range items {
		if it.ID == id && it.Tier == Starter {
			return true
		}
	}
	return slices.Contains(r.Owned, id)
}

// Locked is everything still to unlock, in catalogue order.
func (r *Rewards) Locked(items []Unlockable) []Unlockable {
	var out []Unlockable
	for _, it := range items {
		if it.Tier != Starter && !slices.Contains(r.Owned, it.ID) {
			out = append(out, it)
		}
	}
	return out
}

// Count is how many items are unlocked, starters included.
func (r *Rewards) Count(items []Unlockable) int {
	n := 0
	for _, it := range items {
		if r.Owns(it.ID, items) {
			n++
		}
	}
	return n
}

// Grant adds n passes, never more than there is left to unlock. It
// reports how many were actually granted.
func (r *Rewards) Grant(n int, items []Unlockable) int {
	room := len(r.Locked(items)) - r.Passes
	n = max(0, min(n, room))
	r.Passes += n
	return n
}

// Deal returns the current offer, dealing a new one when there's a pass
// to spend and no offer yet: up to OfferSize locked items, weighted by
// tier (common 6, rare 3, legendary 1), no repeats.
func (r *Rewards) Deal(items []Unlockable, rnd *rand.Rand) []string {
	locked := r.Locked(items)
	valid := len(r.Offer) > 0
	for _, id := range r.Offer {
		if slices.Contains(r.Owned, id) {
			valid = false
		}
	}
	if valid {
		return r.Offer
	}
	r.Offer = nil
	if r.Passes <= 0 || len(locked) == 0 {
		return nil
	}
	pool := slices.Clone(locked)
	for len(r.Offer) < OfferSize && len(pool) > 0 {
		total := 0
		for _, it := range pool {
			total += it.Tier.weight()
		}
		pick := rnd.IntN(total)
		i := 0
		for ; i < len(pool)-1; i++ {
			if pick -= pool[i].Tier.weight(); pick < 0 {
				break
			}
		}
		r.Offer = append(r.Offer, pool[i].ID)
		pool = slices.Delete(pool, i, i+1)
	}
	return r.Offer
}

// Pick spends a pass on id, which must be in the offer. The offer is then
// cleared, and the next Deal makes a new one.
func (r *Rewards) Pick(id string) bool {
	if r.Passes <= 0 || !slices.Contains(r.Offer, id) {
		return false
	}
	r.Passes--
	r.Owned = append(r.Owned, id)
	r.Offer = nil
	return true
}
