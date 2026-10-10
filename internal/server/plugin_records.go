package server

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Plugin records: a plugin sends each member's record (stats and unlocked
// achievements, declared in its plugin.toml); the server checks it, keeps
// it, and passes it to that member. Members see their records on Settings
// > About > Achievements, and each plugin's leaderboard ranks the members
// who don't keep themselves off it.

const (
	maxRecordStats = 20
	leaderboardTop = 10
)

// PluginBoardInfos describes every loaded plugin's achievements and
// leaderboard, for READY and the registry update.
func (h *Handlers) PluginBoardInfos() []protocol.PluginBoardInfo {
	var out []protocol.PluginBoardInfo
	for _, m := range h.plugins.Registry().All() {
		if len(m.Achievements) == 0 && m.Leaderboard.Stat == "" {
			continue
		}
		b := protocol.PluginBoardInfo{PluginID: m.Plugin.ID, Name: m.Plugin.Name,
			LeaderboardStat: m.Leaderboard.Stat, LeaderboardLabel: m.Leaderboard.Label}
		for _, a := range m.Achievements {
			b.Achievements = append(b.Achievements, protocol.PluginAchievementInfo{
				ID: a.ID, Name: a.Name, Description: a.Description, Tier: a.Tier, Icon: a.Icon, Secret: a.Secret})
		}
		out = append(out, b)
	}
	return out
}

// clip shortens s to n runes, on one line.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// handlePluginRecord stores a member's record from a plugin and passes it
// on to them. Achievements the plugin didn't declare are dropped, and the
// first date each was unlocked is kept.
func (h *Handlers) handlePluginRecord(c *Client, req protocol.PluginEventPayload) {
	var rec protocol.PluginRecord
	if err := json.Unmarshal(req.Payload, &rec); err != nil || rec.UserID == uuid.Nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid record payload")
		return
	}
	m, ok := h.plugins.Registry().Manifest(c.PluginID)
	if !ok {
		return
	}
	if u, err := h.db.GetUserByID(rec.UserID); err != nil || u == nil {
		PluginLog.Debug("Dropped a record for an unknown member", "plugin_id", c.PluginID, "user_id", rec.UserID)
		return
	}

	// Keep the first date each achievement was unlocked.
	first := map[string]time.Time{}
	if old, _ := h.db.GetPluginRecord(c.PluginID, rec.UserID); old != "" {
		var prev protocol.PluginRecord
		if json.Unmarshal([]byte(old), &prev) == nil {
			for _, u := range prev.Unlocked {
				first[u.ID] = u.At
			}
		}
	}
	clean := protocol.PluginRecord{UserID: rec.UserID}
	seen := map[string]bool{}
	for _, u := range rec.Unlocked {
		if seen[u.ID] || !m.HasAchievement(u.ID) {
			continue
		}
		seen[u.ID] = true
		at := u.At
		if t, ok := first[u.ID]; ok && !t.IsZero() {
			at = t
		}
		if at.IsZero() || at.After(time.Now().Add(time.Minute)) {
			at = time.Now().UTC()
		}
		clean.Unlocked = append(clean.Unlocked, protocol.PluginUnlock{ID: u.ID, At: at.UTC()})
	}
	for i, s := range rec.Stats {
		if i == maxRecordStats {
			break
		}
		clean.Stats = append(clean.Stats, protocol.PluginStat{
			Key: clip(s.Key, 32), Label: clip(s.Label, 40), Value: clip(s.Value, 40), Num: s.Num})
	}
	data, _ := json.Marshal(clean)
	if err := h.db.SavePluginRecord(c.PluginID, rec.UserID, string(data)); err != nil {
		PluginLog.Error("Failed to save a plugin record", "plugin_id", c.PluginID, "error", err)
		return
	}
	payload := protocol.PluginRecordsPayload{Records: []protocol.PluginRecordInfo{{PluginID: c.PluginID, Record: clean}}}
	_ = h.hub.SendToUser(rec.UserID, protocol.EventPluginRecords, payload)
}

// sendRecords gives a member who just signed in all their records.
func (h *Handlers) sendRecords(c *Client) {
	rows, err := h.db.PluginRecordsForUser(c.UserID)
	if err != nil || len(rows) == 0 {
		return
	}
	var payload protocol.PluginRecordsPayload
	for _, r := range rows {
		var rec protocol.PluginRecord
		if json.Unmarshal([]byte(r.Record), &rec) == nil {
			payload.Records = append(payload.Records, protocol.PluginRecordInfo{PluginID: r.PluginID, Record: rec})
		}
	}
	_ = h.dispatchTo(c, protocol.EventPluginRecords, payload)
}

// leaderboard ranks the members on a plugin's leaderboard by its stat.
func (h *Handlers) leaderboard(pluginID string, me uuid.UUID) (protocol.LeaderboardResponse, bool) {
	m, ok := h.plugins.Registry().Manifest(pluginID)
	if !ok || m.Leaderboard.Stat == "" {
		return protocol.LeaderboardResponse{}, false
	}
	resp := protocol.LeaderboardResponse{PluginID: pluginID, Label: m.Leaderboard.Label, Hidden: h.db.LeaderboardHidden(me)}
	rows, err := h.db.PluginRecordsForPlugin(pluginID)
	if err != nil {
		return resp, true
	}
	var all []protocol.LeaderboardEntry
	for _, r := range rows {
		var rec protocol.PluginRecord
		if json.Unmarshal([]byte(r.Record), &rec) != nil {
			continue
		}
		for _, s := range rec.Stats {
			if s.Key == m.Leaderboard.Stat {
				all = append(all, protocol.LeaderboardEntry{UserID: r.UserID, Value: s.Value, Num: s.Num})
				break
			}
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].Num > all[j].Num })
	ids := make([]uuid.UUID, 0, len(all))
	for _, e := range all {
		ids = append(ids, e.UserID)
	}
	names := map[uuid.UUID]string{}
	if users, err := h.db.GetUsersByIDs(ids); err == nil {
		for _, u := range users {
			names[u.ID] = u.Username
		}
	}
	for i := range all {
		all[i].Rank = i + 1
		if i > 0 && all[i].Num == all[i-1].Num {
			all[i].Rank = all[i-1].Rank // a tie shares the place
		}
		all[i].Username = names[all[i].UserID]
		if all[i].UserID == me {
			you := all[i]
			resp.You = &you
		}
	}
	resp.Entries = all[:min(len(all), leaderboardTop)]
	return resp, true
}

// handleLeaderboard serves GET /api/plugins/leaderboard/{plugin_id}.
func (s *Server) handleLeaderboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeAPIError(w, http.StatusMethodNotAllowed, "method", "Method not allowed")
		return
	}
	user, _, ok := s.bearerUser(w, r)
	if !ok {
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/plugins/leaderboard/")
	resp, found := s.handlers.leaderboard(id, user.ID)
	if !found {
		writeAPIError(w, http.StatusNotFound, "no_leaderboard", "That plugin has no leaderboard here")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleLeaderboardVisibility serves POST /api/account/leaderboard
// {"visible": bool}: whether you appear on this server's leaderboards.
func (s *Server) handleLeaderboardVisibility(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeAPIError(w, http.StatusMethodNotAllowed, "method", "Method not allowed")
		return
	}
	user, _, ok := s.bearerUser(w, r)
	if !ok {
		return
	}
	var req struct {
		Visible bool `json:"visible"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&req); err != nil {
		writeAPIError(w, http.StatusBadRequest, "bad_request", "Expected {\"visible\": true|false}")
		return
	}
	if err := s.db.SetLeaderboardHidden(user.ID, !req.Visible); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "internal", "Couldn't save that")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"visible": req.Visible})
}
