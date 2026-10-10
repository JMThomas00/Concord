package plugins

import (
	"fmt"
	"regexp"
)

// Achievements and leaderboards: a plugin declares the achievements its
// members can unlock in plugin.toml, and which stat its leaderboard ranks
// by; it then sends each member's record (sdk/wire.PluginRecord), which
// the server keeps and members see on Settings > About > Achievements.
//
//	[[achievement]]
//	id = "first_win"
//	name = "First Victory"
//	description = "Win a game"
//	tier = "bronze"   # optional: bronze, silver, gold
//	icon = "🏆"       # optional, shown before the name
//	secret = false    # hidden until unlocked
//
//	[leaderboard]
//	stat = "wins"     # a stat key from the records
//	label = "Wins"

// AchievementDef is one [[achievement]] entry.
type AchievementDef struct {
	ID          string `toml:"id" json:"id"`
	Name        string `toml:"name" json:"name"`
	Description string `toml:"description" json:"description"`
	Tier        string `toml:"tier" json:"tier,omitempty"`
	Icon        string `toml:"icon" json:"icon,omitempty"`
	Secret      bool   `toml:"secret" json:"secret,omitempty"`
}

// LeaderboardDef is [leaderboard]: which stat ranks members (empty: none).
type LeaderboardDef struct {
	Stat  string `toml:"stat" json:"stat,omitempty"`
	Label string `toml:"label" json:"label,omitempty"`
}

// MaxAchievements is how many a plugin may declare.
const MaxAchievements = 100

var achievementID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,47}$`)

func (m *Manifest) validateAchievements() error {
	if len(m.Achievements) > MaxAchievements {
		return fmt.Errorf("%d achievements (at most %d)", len(m.Achievements), MaxAchievements)
	}
	seen := map[string]bool{}
	for _, a := range m.Achievements {
		if !achievementID.MatchString(a.ID) {
			return fmt.Errorf("achievement id %q: use lowercase letters, digits, _ . - (up to 48)", a.ID)
		}
		if seen[a.ID] {
			return fmt.Errorf("achievement %q declared twice", a.ID)
		}
		seen[a.ID] = true
		if a.Name == "" || len(a.Name) > 60 || len(a.Description) > 160 || len([]rune(a.Icon)) > 4 {
			return fmt.Errorf("achievement %q: a name is needed (60 characters at most), a description of 160 and an icon of 4", a.ID)
		}
		switch a.Tier {
		case "", "bronze", "silver", "gold":
		default:
			return fmt.Errorf("achievement %q: tier is bronze, silver or gold", a.ID)
		}
	}
	return nil
}

// HasAchievement reports whether the plugin declared an achievement.
func (m *Manifest) HasAchievement(id string) bool {
	for _, a := range m.Achievements {
		if a.ID == id {
			return true
		}
	}
	return false
}
