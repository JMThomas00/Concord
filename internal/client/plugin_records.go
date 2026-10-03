package client

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Plugin records: each plugin with achievements (or a leaderboard) keeps a
// record for you on its server, which the server sends here after you
// sign in and whenever it changes. They're shown on Settings > About >
// Achievements, a tab per plugin per server, with that plugin's
// leaderboard (fetched when you open its tab).

// applyRecords takes in records from a server, with a toast for each
// achievement unlocked just now, and refreshes Concord's game totals.
func (a *App) applyRecords(sc *ServerConnection, p protocol.PluginRecordsPayload) {
	sc.mu.Lock()
	if sc.PluginRecords == nil {
		sc.PluginRecords = map[string]protocol.PluginRecord{}
	}
	var fresh []string
	for _, r := range p.Records {
		old := map[string]bool{}
		for _, u := range sc.PluginRecords[r.PluginID].Unlocked {
			old[u.ID] = true
		}
		for _, u := range r.Record.Unlocked {
			// New to this client, and unlocked in the last few minutes (a
			// sign-in brings the old ones too: no toasts for those).
			if !old[u.ID] && time.Since(u.At) < 5*time.Minute {
				fresh = append(fresh, r.PluginID+"\x00"+u.ID)
			}
		}
		sc.PluginRecords[r.PluginID] = r.Record
	}
	boards := sc.PluginBoards
	sc.mu.Unlock()

	for _, f := range fresh {
		pluginID, id := splitNul(f)
		name, ach := pluginID, protocol.PluginAchievementInfo{ID: id, Name: id}
		for _, b := range boards {
			if b.PluginID == pluginID {
				name = b.Name
				for _, d := range b.Achievements {
					if d.ID == id {
						ach = d
					}
				}
			}
		}
		label := "🏆 " + name
		if m := tierMedal(ach.Tier); m != "" {
			label = m + " " + name
		}
		a.toasts = append(a.toasts, &toast{label: label, title: ach.Name, created: time.Now()})
	}
	a.gameTotals()
}

func splitNul(s string) (string, string) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return s[:i], s[i+1:]
		}
	}
	return s, ""
}

func tierMedal(tier string) string {
	return map[string]string{"bronze": "🥉", "silver": "🥈", "gold": "🥇"}[tier]
}

// gameTotals unlocks Concord's own game achievements from every plugin's
// records, on every server: wins add up, and beating the computer
// anywhere counts.
func (a *App) gameTotals() {
	wins, beatComputer := 0.0, false
	for _, sc := range a.allConnections() {
		sc.mu.RLock()
		for _, rec := range sc.PluginRecords {
			for _, s := range rec.Stats {
				if s.Key == "wins" {
					wins += s.Num
				}
			}
			for _, u := range rec.Unlocked {
				beatComputer = beatComputer || u.ID == "beat_computer"
			}
		}
		sc.mu.RUnlock()
	}
	if wins >= 1 {
		a.unlock("first_win")
	}
	if wins >= 10 {
		a.unlock("wins_10")
	}
	if wins >= 50 {
		a.unlock("wins_50")
	}
	if beatComputer {
		a.unlock("beat_computer")
	}
}

func (a *App) allConnections() []*ServerConnection {
	var all []*ServerConnection
	if a.connMgr != nil {
		all = a.connMgr.GetAllConnections()
	}
	for _, sc := range all {
		if sc == a.activeConn {
			return all
		}
	}
	if a.activeConn != nil {
		all = append(all, a.activeConn)
	}
	return all
}

// --- leaderboards ------------------------------------------------------------

type leaderboardState struct {
	loading bool
	resp    *protocol.LeaderboardResponse
	err     string
	at      time.Time
}

type leaderboardMsg struct {
	key  string
	resp *protocol.LeaderboardResponse
	err  error
}

func leaderboardKey(serverID uuid.UUID, pluginID string) string {
	return serverID.String() + "/" + pluginID
}

// fetchLeaderboard asks a server for a plugin's leaderboard (at most every
// 20 seconds unless forced).
func (a *App) fetchLeaderboard(sc *ServerConnection, pluginID string, force bool) tea.Cmd {
	if sc == nil || sc.ServerInfo == nil {
		return nil
	}
	key := leaderboardKey(sc.ServerID, pluginID)
	if a.leaderboards == nil {
		a.leaderboards = map[string]*leaderboardState{}
	}
	st := a.leaderboards[key]
	if st != nil && (st.loading || (!force && time.Since(st.at) < 20*time.Second)) {
		return nil
	}
	if st == nil {
		st = &leaderboardState{}
		a.leaderboards[key] = st
	}
	st.loading = true
	addr, token := sc.ServerInfo.GetHTTPURL(), sc.Token
	return func() tea.Msg {
		var resp protocol.LeaderboardResponse
		err := getAPI(addr, "/api/plugins/leaderboard/"+url.PathEscape(pluginID), token, &resp)
		if err != nil {
			return leaderboardMsg{key: key, err: err}
		}
		return leaderboardMsg{key: key, resp: &resp}
	}
}

func (a *App) handleLeaderboard(m leaderboardMsg) {
	st := a.leaderboards[m.key]
	if st == nil {
		return
	}
	st.loading, st.at = false, time.Now()
	st.resp, st.err = m.resp, ""
	if m.err != nil {
		st.err = m.err.Error()
	}
}

// setLeaderboardVisible shows or hides you on a server's leaderboards,
// then refreshes the board on screen.
func (a *App) setLeaderboardVisible(sc *ServerConnection, pluginID string, visible bool) tea.Cmd {
	if sc == nil || sc.ServerInfo == nil {
		return nil
	}
	addr, token := sc.ServerInfo.GetHTTPURL(), sc.Token
	key := leaderboardKey(sc.ServerID, pluginID)
	return func() tea.Msg {
		_, err := postAPI(addr, "/api/account/leaderboard", token, map[string]bool{"visible": visible}, nil)
		if err != nil {
			return leaderboardMsg{key: key, err: err}
		}
		var resp protocol.LeaderboardResponse
		err = getAPI(addr, "/api/plugins/leaderboard/"+url.PathEscape(pluginID), token, &resp)
		if err != nil {
			return leaderboardMsg{key: key, err: err}
		}
		return leaderboardMsg{key: key, resp: &resp}
	}
}

// getAPI is postAPI's GET: a signed-in request, decoding a JSON answer.
func getAPI(serverAddr, path, token string, out interface{}) error {
	target, err := apiURL(serverAddr, path)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return apiErrorFrom(resp.StatusCode, raw)
	}
	return json.Unmarshal(raw, out)
}

// --- the tabs on Settings > About > Achievements -------------------------

// achTab is one tab: Concord's own achievements (sc nil), or one plugin's
// on one server.
type achTab struct {
	sc    *ServerConnection
	board protocol.PluginBoardInfo
	title string
}

// achievementTabs lists the tabs: Concord first, then every plugin with
// achievements or a leaderboard on every server, by name. A plugin on more
// than one server gets the server's name too.
func (a *App) achievementTabs() []achTab {
	tabs := []achTab{{title: "Concord"}}
	var plugins []achTab
	count := map[string]int{}
	for _, sc := range a.allConnections() {
		sc.mu.RLock()
		for _, b := range sc.PluginBoards {
			plugins = append(plugins, achTab{sc: sc, board: b, title: b.Name})
			count[b.Name]++
		}
		sc.mu.RUnlock()
	}
	for i := range plugins {
		if count[plugins[i].title] > 1 && plugins[i].sc.ServerInfo != nil {
			plugins[i].title += " · " + plugins[i].sc.ServerInfo.Name
		}
	}
	sort.SliceStable(plugins, func(i, j int) bool { return plugins[i].title < plugins[j].title })
	return append(tabs, plugins...)
}
