package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

func chessBoard() protocol.PluginBoardInfo {
	return protocol.PluginBoardInfo{PluginID: "chess", Name: "Chess", LeaderboardStat: "wins", LeaderboardLabel: "Wins",
		Achievements: []protocol.PluginAchievementInfo{
			{ID: "first_win", Name: "First Victory", Description: "Win a game", Tier: "bronze"},
			{ID: "beat_computer", Name: "Beat the Machine", Description: "Beat the computer"},
			{ID: "fools_mate", Name: "Fool's Mate", Secret: true},
		}}
}

func recordsApp(t *testing.T) (*App, *ServerConnection) {
	a := eggApp(t)
	me := &models.User{ID: uuid.New()}
	sc := &ServerConnection{ServerID: uuid.New(), ServerInfo: &ClientServerInfo{Name: "Sequoia"}, User: me,
		PluginBoards: []protocol.PluginBoardInfo{chessBoard()}}
	a.activeConn = sc
	return a, sc
}

func TestRecordsToastFreshUnlocksAndAddUp(t *testing.T) {
	a, sc := recordsApp(t)
	old := time.Now().Add(-48 * time.Hour)
	a.applyRecords(sc, protocol.PluginRecordsPayload{Records: []protocol.PluginRecordInfo{{PluginID: "chess",
		Record: protocol.PluginRecord{UserID: sc.User.ID,
			Stats:    []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "12", Num: 12}},
			Unlocked: []protocol.PluginUnlock{{ID: "first_win", At: old}}}}}})
	for _, ts := range a.toasts {
		if strings.Contains(ts.label, "Chess") {
			t.Fatal("a toast for an old unlock (sign-in)")
		}
	}
	if a.coll().Achievements["wins_10"] == "" || a.coll().Achievements["first_win"] == "" {
		t.Fatal("Concord's totals didn't count the plugin's wins")
	}
	a.toasts = nil
	a.applyRecords(sc, protocol.PluginRecordsPayload{Records: []protocol.PluginRecordInfo{{PluginID: "chess",
		Record: protocol.PluginRecord{UserID: sc.User.ID,
			Stats:    []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "13", Num: 13}},
			Unlocked: []protocol.PluginUnlock{{ID: "first_win", At: old}, {ID: "beat_computer", At: time.Now()}}}}}})
	found := false
	for _, ts := range a.toasts {
		found = found || (ts.label == "🏆 Chess" && ts.title == "Beat the Machine")
	}
	if !found || a.coll().Achievements["beat_computer"] == "" {
		t.Fatalf("toasts %+v", a.toasts)
	}
}

func TestAchievementTabsAndPage(t *testing.T) {
	a, sc := recordsApp(t)
	other := &ServerConnection{ServerID: uuid.New(), ServerInfo: &ClientServerInfo{Name: "RedOak"},
		PluginBoards: []protocol.PluginBoardInfo{chessBoard(), {PluginID: "tak", Name: "Tak"}}}
	a.connMgr = nil
	a.activeConn = sc
	tabs := a.achievementTabs()
	if len(tabs) != 2 { // just the active connection without a manager
		t.Fatalf("%d tabs", len(tabs))
	}
	_ = other

	a.view = ViewSettings
	a.settingsState = &SettingsState{SelectedCategory: settingsCatAbout}
	a.handleAboutKey(keyOf("a"))
	if !a.settingsState.AboutAch {
		t.Fatal("A didn't open the page")
	}
	sc.PluginRecords = map[string]protocol.PluginRecord{"chess": {UserID: sc.User.ID,
		Stats:    []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "13", Num: 13}, {Key: "losses", Label: "Losses", Value: "4"}},
		Unlocked: []protocol.PluginUnlock{{ID: "first_win", At: time.Now()}}}}
	a.settingsState.AchServer, a.settingsState.AchPlugin = sc.ServerID.String(), "chess"
	a.achChanged()
	a.handleLeaderboard(leaderboardMsg{key: leaderboardKey(sc.ServerID, "chess"), resp: &protocol.LeaderboardResponse{
		Label: "Wins", Entries: []protocol.LeaderboardEntry{
			{Rank: 1, Username: "bob", Value: "20", Num: 20}, {Rank: 2, UserID: sc.User.ID, Username: "gh0st", Value: "13", Num: 13}}}})
	out := ansi.Strip(a.renderAchievementsPage(120, 40))
	for _, want := range []string{"Concord", "Chess", "Wins 13", "Losses 4", "Leaderboard · Wins", "bob", "gh0st", "First Victory", "Achievements  1/3", "1 secret"} {
		if !strings.Contains(out, want) {
			t.Errorf("page missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Fool's Mate") {
		t.Fatal("a secret achievement shows")
	}
	a.handleAchKey(keyOf("esc"))
	if a.settingsState.AboutAch {
		t.Fatal("Esc didn't go back to About")
	}
}

func TestOrdinal(t *testing.T) {
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th", 21: "21st", 112: "112th"} {
		if got := ordinal(n); got != want {
			t.Errorf("%d: %s", n, got)
		}
	}
}

// TestAchievementRows covers the three rows: All on both, a plugin kept
// when the server changes, search (never showing secret names), Esc
// clearing the search before leaving, and the list scrolling.
func TestAchievementRows(t *testing.T) {
	a, sc := recordsApp(t)
	a.view = ViewSettings
	a.settingsState = &SettingsState{SelectedCategory: settingsCatAbout}
	a.handleAboutKey(keyOf("a"))
	s := a.settingsState
	page := func() string { return ansi.Strip(a.renderAchievementsPage(120, 40)) }

	servers, si, _, _ := a.achSelection()
	if servers[0].name != "All" || servers[1].name != "Concord" || servers[si].key != "concord" || len(servers) != 3 {
		t.Fatalf("server row %+v, chosen %d", servers, si)
	}
	if out := page(); strings.Contains(out, "PLUGIN") || !strings.Contains(out, "SERVER") {
		t.Fatal("Concord has no plugin row")
	}

	a.handleAchKey(keyOf("right")) // Sequoia
	_, _, plugins, _ := a.achSelection()
	if s.AchServer != sc.ServerID.String() || plugins[0].name != "All" || plugins[1].name != "Chess" {
		t.Fatalf("server %q, plugins %+v", s.AchServer, plugins)
	}
	a.handleAchKey(keyOf("tab")) // the plugin row
	a.handleAchKey(keyOf("right"))
	if s.AchPlugin != "chess" {
		t.Fatalf("plugin %q", s.AchPlugin)
	}
	a.handleAchKey(keyOf("shift+tab"))
	a.handleAchKey(keyOf("left"))
	a.handleAchKey(keyOf("left")) // All
	if s.AchServer != "all" || s.AchPlugin != "chess" {
		t.Fatalf("All servers should keep Chess: %q %q", s.AchServer, s.AchPlugin)
	}
	if out := page(); !strings.Contains(out, "Chess  on Sequoia") {
		t.Fatalf("All servers, Chess:\n%s", out)
	}

	// search: across everything, by name or description, secrets kept secret
	s.AchPlugin = "all"
	a.handleAchKey(keyOf("/"))
	for _, r := range "beat comp" {
		a.handleAchKey(keyOf(string(r)))
	}
	out := page()
	if !strings.Contains(out, "Beat the computer") || strings.Contains(out, "Win a game") {
		t.Fatalf("search for %q:\n%s", s.AchQuery, out)
	}
	s.AchQuery = "fool"
	if out := page(); strings.Contains(out, "Fool's Mate") || !strings.Contains(out, "Nothing matches") {
		t.Fatal("search found a secret achievement")
	}
	a.handleAchKey(keyOf("esc"))
	if s.AchQuery != "" || !s.AboutAch {
		t.Fatal("Esc should clear the search first")
	}

	// scrolling: a short pane needs it, and says where you are
	small := ansi.Strip(a.renderAchievementsPage(120, 20))
	if !strings.Contains(small, "↑↓ scroll") || !strings.Contains(small, "▼") {
		t.Fatalf("no scroll hint:\n%s", small)
	}
	a.handleAchKey(keyOf("end"))
	if small = ansi.Strip(a.renderAchievementsPage(120, 20)); !strings.Contains(small, "▲") || strings.Contains(small, "▼") {
		t.Fatalf("End should reach the bottom:\n%s", small)
	}
	a.handleAchKey(keyOf("esc"))
	if s.AboutAch {
		t.Fatal("Esc with no search should go back to About")
	}
}

func TestFuzzyMatch(t *testing.T) {
	for _, c := range []struct {
		q, s string
		ok   bool
	}{{"beat comp", "Beat the computer", true}, {"btc", "Beat the computer", true}, {"win ten", "Win ten games", true}, {"wtg", "Spend ten hours", false}, {"", "anything", true}} {
		if got := fuzzyMatch(c.q, c.s); got != c.ok {
			t.Errorf("%q in %q: %v", c.q, c.s, got)
		}
	}
}

// TestAchievementSearchTyping: a NUL (a lone modifier key on Windows)
// doesn't land in the search, a space goes in once, and typing "grape" or
// "disco" there doesn't set off About's eggs.
func TestAchievementSearchTyping(t *testing.T) {
	a, _ := recordsApp(t)
	a.view = ViewSettings
	a.settingsState = &SettingsState{SelectedCategory: settingsCatAbout}
	a.handleAboutKey(keyOf("a"))
	a.handleAchKey(keyOf("/"))
	for _, r := range "grape" {
		k := keyOf(string(r))
		a.watchEggKeys(k)
		a.handleAchKey(k)
	}
	a.handleAchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{0}})
	a.handleAchKey(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if q := a.settingsState.AchQuery; q != "grape " {
		t.Fatalf("query %q", q)
	}
	if a.egg != nil {
		t.Fatal("typing grape in the search set off the grape burst")
	}
}
