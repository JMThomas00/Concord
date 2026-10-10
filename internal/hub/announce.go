package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Hubs add each other by announcing: a hub with a public_url tells each of
// its peers "I'm here", and a peer that accepts announcements checks the hub
// answers at that address, then adds it back. Connecting a new hub to the
// official one (the installer's question) is then enough for both to share
// listings, and for the new hub to show up on concordchat.cc.

const (
	// maxPeerHubs caps the peer list, so announcements can't grow it forever.
	maxPeerHubs = 500
	// stalePeerAfter is how long a hub that added itself may go without a
	// successful sync before it's dropped (it comes back when it announces).
	stalePeerAfter = 7 * 24 * time.Hour
)

// announceBuckets rate-limits announcements per address, apart from joins.
var announceBuckets sync.Map

func announceBucket(ip string) *bucket {
	v, _ := announceBuckets.LoadOrStore(ip, &bucket{tokens: 5, lastFill: time.Now()})
	return v.(*bucket)
}

// normalizeHubURL checks a hub address and returns it in the form peers are
// stored in: scheme and host (and port), no path, no trailing slash.
func normalizeHubURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > 200 {
		return "", fmt.Errorf("url is too long")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" {
		return "", fmt.Errorf("url must be http(s)://host[:port]")
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return "", fmt.Errorf("url must be just the hub's address, like https://hub.example.com")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// POST /v1/hubs/announce {"name": "...", "url": "https://hub.example.com"}
func (h *Hub) handleAnnounce(w http.ResponseWriter, r *http.Request) {
	if !h.config.AcceptPeerAnnouncements {
		writeError(w, http.StatusForbidden, "this hub doesn't accept announcements")
		return
	}
	if !announceBucket(h.clientIP(r)).allow() {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}
	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	hubURL, err := normalizeHubURL(req.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if own, err := normalizeHubURL(h.config.PublicURL); err == nil && own == hubURL {
		writeError(w, http.StatusBadRequest, "that's this hub")
		return
	}

	// It must answer as a Grapevine hub, at a public address.
	name, err := checkHub(hubURL)
	if err != nil {
		ApiLog.Warn("announcement refused", "url", hubURL, "error", err)
		writeError(w, http.StatusBadRequest, "couldn't reach a Grapevine hub at that address: "+err.Error())
		return
	}

	if n, err := h.db.CountPeerHubs(); err == nil && n >= maxPeerHubs {
		writeError(w, http.StatusServiceUnavailable, "this hub's peer list is full")
		return
	}
	added, err := h.db.AddAnnouncedPeer(name, hubURL)
	if err != nil {
		ApiLog.Error("add announced hub failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add hub")
		return
	}
	if added {
		FedLog.Info("peer hub announced itself", "name", name, "url", hubURL)
		go h.syncNewPeer(hubURL)
		writeJSON(w, http.StatusCreated, map[string]string{"status": "added"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "known"})
}

// checkHub asks a hub's health endpoint, through the public-only client, and
// returns its name.
func checkHub(hubURL string) (string, error) {
	resp, err := publicClient(8 * time.Second).Get(hubURL + "/v1/health")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("health check returned HTTP %d", resp.StatusCode)
	}
	var health struct {
		Status  string `json:"status"`
		HubName string `json:"hub_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&health); err != nil || health.Status != "ok" {
		return "", fmt.Errorf("not a Grapevine hub")
	}
	name := strings.TrimSpace(health.HubName)
	if name == "" {
		name = strings.TrimPrefix(strings.TrimPrefix(hubURL, "https://"), "http://")
	}
	if r := []rune(name); len(r) > 64 {
		name = string(r[:64])
	}
	return name, nil
}

// syncNewPeer pulls a just-added hub's listings now, rather than at the next
// federation tick.
func (h *Hub) syncNewPeer(hubURL string) {
	hubs, err := h.db.ListPeerHubs()
	if err != nil {
		return
	}
	for _, ph := range hubs {
		if ph.URL == hubURL && ph.IsActive {
			h.syncHub(ph)
			return
		}
	}
}

// announceToPeers tells every active peer hub this hub's address. Run at
// startup and on every federation tick, so a peer that dropped or lost this
// hub adds it again.
func (h *Hub) announceToPeers() {
	self, err := normalizeHubURL(h.config.PublicURL)
	if err != nil {
		return
	}
	body, _ := json.Marshal(map[string]string{"name": h.config.HubName, "url": self})
	hubs, err := h.db.ListPeerHubs()
	if err != nil {
		return
	}
	for _, ph := range hubs {
		if !ph.IsActive || ph.URL == self {
			continue
		}
		go func(ph *PeerHub) {
			resp, err := h.peerClient(ph.URL, 15*time.Second).Post(ph.URL+"/v1/hubs/announce", "application/json", bytes.NewReader(body))
			if err != nil {
				FedLog.Warn("announce failed", "peer", ph.Name, "error", err)
				return
			}
			defer resp.Body.Close()
			switch resp.StatusCode {
			case http.StatusCreated:
				FedLog.Info("announced to peer: added", "peer", ph.Name)
			case http.StatusOK, http.StatusNotFound, http.StatusForbidden:
				// known already, an older hub without announcements, or one
				// that doesn't take them: nothing to do
			default:
				msg, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
				FedLog.Warn("announce refused", "peer", ph.Name, "status", resp.StatusCode, "reply", strings.TrimSpace(string(msg)))
			}
		}(ph)
	}
}

// DELETE /v1/hubs/{id} — block a peer hub (requires admin token). It stays
// recorded as inactive, so announcing again doesn't bring it back; the admin
// API's POST /v1/hubs re-enables it.
func (h *Hub) handleBlockHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}
	ok, err := h.db.DeactivatePeerHub(r.PathValue("id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to block hub")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "hub not found")
		return
	}
	FedLog.Info("peer hub blocked", "id", r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}
