package table

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/JMThomas00/Concord/sdk/arcade"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// Records: the kit keeps every player's record (wins, losses, draws,
// streaks) and the standard achievements below, and sends each player's
// record to Concord when a game ends. Members see it on Settings > About
// > Achievements, with the game's leaderboard (by wins).
//
// For Concord to accept the achievements, declare them in plugin.toml
// (StandardAchievementsTOML is the block to paste, and Rules.Awards'
// own ids go alongside), plus the leaderboard:
//
//	[leaderboard]
//	stat = "wins"
//	label = "Wins"

// The standard achievements every game gets.
const (
	AchFirstWin     = "first_win"
	AchWins10       = "wins_10"
	AchWins50       = "wins_50"
	AchBeatComputer = "beat_computer"
	AchStreak5      = "streak_5"
)

// StandardAchievementsTOML declares the standard achievements and the
// leaderboard, for a game's plugin.toml.
const StandardAchievementsTOML = `[[achievement]]
id = "first_win"
name = "First Victory"
description = "Win a game"
tier = "bronze"

[[achievement]]
id = "wins_10"
name = "Seasoned Player"
description = "Win ten games"
tier = "silver"

[[achievement]]
id = "wins_50"
name = "Grandmaster of the Vineyard"
description = "Win fifty games"
tier = "gold"

[[achievement]]
id = "beat_computer"
name = "Beat the Machine"
description = "Beat the computer"
icon = "🤖"

[[achievement]]
id = "streak_5"
name = "On a Roll"
description = "Win five games in a row"
icon = "🔥"

[leaderboard]
stat = "wins"
label = "Wins"
`

// playerRecord is one player's record for this game.
type playerRecord struct {
	Wins       int                  `json:"wins"`
	Losses     int                  `json:"losses"`
	Draws      int                  `json:"draws"`
	Streak     int                  `json:"streak"` // wins in a row now
	BestStreak int                  `json:"best_streak"`
	Unlocked   map[string]time.Time `json:"unlocked,omitempty"`

	// The arcade (arcade.go): who they are, games played, passes and
	// unlockables, and their own options.
	Name     string            `json:"name,omitempty"`
	Games    int               `json:"games,omitempty"`
	Rewards  arcade.Rewards    `json:"rewards"`
	Equipped map[string]string `json:"equipped,omitempty"` // kind -> id
	Muted    bool              `json:"muted,omitempty"`
	Effects  string            `json:"effects,omitempty"` // "" (full), "calm" or "off"
	Earned   int               `json:"-"`                 // passes from the last game, for its results screen
}

func (r *playerRecord) unlock(id string, now time.Time) {
	if r.Unlocked == nil {
		r.Unlocked = map[string]time.Time{}
	}
	if _, done := r.Unlocked[id]; !done {
		r.Unlocked[id] = now
	}
}

func (k *Kit) recordsFile() string {
	dir := "."
	if k.conn != nil && k.conn.DataDir() != "" {
		dir = k.conn.DataDir()
	}
	return filepath.Join(dir, "tables", "records.json")
}

func (k *Kit) loadRecords() {
	if k.records != nil {
		return
	}
	k.records = map[uuid.UUID]*playerRecord{}
	if data, err := os.ReadFile(k.recordsFile()); err == nil {
		_ = json.Unmarshal(data, &k.records)
	}
}

func (k *Kit) saveRecords() {
	path := k.recordsFile()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		log.Printf("table: can't save records: %v", err)
		return
	}
	data, _ := json.MarshalIndent(k.records, "", "  ")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
}

// recordResult adds a finished game to its players' records, unlocks what
// they've earned, and sends each record to Concord.
func (k *Kit) recordResult(t *Table, o Outcome) {
	k.loadRecords()
	now := time.Now().UTC()
	computer := false
	for _, p := range t.Seats {
		computer = computer || p.Computer
	}
	for i, p := range t.Seats {
		if p.Computer || p.UserID == uuid.Nil {
			continue
		}
		r := k.records[p.UserID]
		if r == nil {
			r = &playerRecord{}
			k.records[p.UserID] = r
		}
		r.Name = p.Name
		r.Games++
		before := len(r.Unlocked)
		switch {
		case o.Winner == i:
			r.Wins++
			r.Streak++
			r.BestStreak = max(r.BestStreak, r.Streak)
			r.unlock(AchFirstWin, now)
			if computer {
				r.unlock(AchBeatComputer, now)
			}
		case o.Winner >= 0:
			r.Losses++
			r.Streak = 0
		default:
			r.Draws++
			r.Streak = 0
		}
		if r.Wins >= 10 {
			r.unlock(AchWins10, now)
		}
		if r.Wins >= 50 {
			r.unlock(AchWins50, now)
		}
		if r.BestStreak >= 5 {
			r.unlock(AchStreak5, now)
		}
		if k.rules.Awards != nil {
			for _, id := range k.rules.Awards(t.game, i, o) {
				r.unlock(id, now)
			}
		}
		r.Earned = 0
		if a := k.rules.Arcade; a != nil && len(a.Unlockables) > 0 {
			// A pass for each new achievement, every third win in a row
			// and every tenth game.
			n := len(r.Unlocked) - before
			if o.Winner == i && r.Streak > 0 && r.Streak%3 == 0 {
				n++
			}
			if r.Games%10 == 0 {
				n++
			}
			r.Earned = r.Rewards.Grant(n, a.Unlockables)
		}
		if k.conn != nil {
			_ = k.conn.SendRecord(r.wire(p.UserID))
		}
	}
	k.saveRecords()
}

// wire is the record as Concord takes it.
func (r *playerRecord) wire(userID uuid.UUID) wire.PluginRecord {
	played := r.Wins + r.Losses + r.Draws
	rate := "-"
	if played > 0 {
		rate = fmt.Sprintf("%d%%", r.Wins*100/played)
	}
	rec := wire.PluginRecord{UserID: userID, Stats: []wire.PluginStat{
		{Key: "wins", Label: "Wins", Value: fmt.Sprint(r.Wins), Num: float64(r.Wins)},
		{Key: "losses", Label: "Losses", Value: fmt.Sprint(r.Losses), Num: float64(r.Losses)},
		{Key: "draws", Label: "Draws", Value: fmt.Sprint(r.Draws), Num: float64(r.Draws)},
		{Key: "win_rate", Label: "Win rate", Value: rate},
		{Key: "best_streak", Label: "Best streak", Value: fmt.Sprint(r.BestStreak), Num: float64(r.BestStreak)},
	}}
	for id, at := range r.Unlocked {
		rec.Unlocked = append(rec.Unlocked, wire.PluginUnlock{ID: id, At: at})
	}
	return rec
}
