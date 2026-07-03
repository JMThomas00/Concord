package hub

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ── JSON helpers ──────────────────────────────────────────────────────────────

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, ErrorResponse{Error: msg})
}

// ── HMAC auth helper ──────────────────────────────────────────────────────────

// requireSig reads the request body, verifies the X-Grapevine-Sig HMAC, and
// returns the body bytes so the caller can decode them. On failure it writes
// the error response and returns nil.
func requireSig(w http.ResponseWriter, r *http.Request, secret string) []byte {
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(r.Body); err != nil {
		writeError(w, http.StatusBadRequest, "cannot read body")
		return nil
	}
	sig := r.Header.Get("X-Grapevine-Sig")
	if !VerifyRequest(secret, buf.Bytes(), sig) {
		writeError(w, http.StatusUnauthorized, "invalid signature")
		return nil
	}
	return buf.Bytes()
}

// ── Rate limiter (per-IP, token bucket) ───────────────────────────────────────

type bucket struct {
	mu       sync.Mutex
	tokens   float64
	lastFill time.Time
}

// allow consumes one token; returns true if the request is permitted.
// Rate: 5 requests per minute, burst up to 5.
func (b *bucket) allow() bool {
	const rate = 5.0 / 60.0 // tokens per second
	const maxTokens = 5.0

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(b.lastFill).Seconds()
	b.tokens = min(maxTokens, b.tokens+elapsed*rate)
	b.lastFill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

var buckets sync.Map // string (IP) → *bucket

func getBucket(ip string) *bucket {
	if v, ok := buckets.Load(ip); ok {
		return v.(*bucket)
	}
	b := &bucket{tokens: 5, lastFill: time.Now()}
	buckets.Store(ip, b)
	return b
}

func clientIP(r *http.Request) string {
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		return strings.SplitN(fwd, ",", 2)[0]
	}
	// RemoteAddr is "host:port"
	host, _, _ := strings.Cut(r.RemoteAddr, ":")
	return host
}

// ── Handlers ──────────────────────────────────────────────────────────────────

// GET /v1/health
func (h *Hub) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":   "ok",
		"hub_name": h.config.HubName,
	})
}

// POST /v1/servers — register a new Concord server
func (h *Hub) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.Name == "" || req.Host == "" || req.Port == 0 {
		writeError(w, http.StatusBadRequest, "name, host, and port are required")
		return
	}
	if req.MaxMembers <= 0 {
		req.MaxMembers = 1000
	}

	secret, err := GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate secret")
		return
	}

	srv := &RegisteredServer{
		ID:                 uuid.New().String(),
		Name:               req.Name,
		Description:        req.Description,
		Category:           req.Category,
		Tags:               req.Tags,
		Host:               req.Host,
		Port:               req.Port,
		RegistrationSecret: secret,
		MaxMembers:         req.MaxMembers,
		IsOnline:           true,
		LastHeartbeat:      time.Now(),
		RegisteredAt:       time.Now(),
	}

	if err := h.db.CreateServer(srv); err != nil {
		ApiLog.Error("register failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to register server")
		return
	}

	h.stats.Registrations.Add(1)
	ApiLog.Info("server registered", "name", srv.Name, "id", srv.ID, "addr", fmt.Sprintf("%s:%d", srv.Host, srv.Port))
	writeJSON(w, http.StatusCreated, RegisterResponse{
		ServerID:           srv.ID,
		RegistrationSecret: secret,
	})
}

// DELETE /v1/servers/{id} — deregister a server.
// Marks the server offline rather than deleting it, so the server keeps its
// ID and registration secret across restarts. Rows offline for longer than
// stalePurgeAge are removed by CleanupLoop.
func (h *Hub) handleDeregister(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := h.db.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	body := requireSig(w, r, srv.RegistrationSecret)
	if body == nil {
		return
	}

	if err := h.db.MarkServerOffline(id); err != nil {
		ApiLog.Error("deregister failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deregister")
		return
	}

	h.stats.Deregistrations.Add(1)
	ApiLog.Info("server deregistered, marked offline", "name", srv.Name, "id", id)
	w.WriteHeader(http.StatusNoContent)
}

// POST /v1/servers/{id}/heartbeat — update server stats
func (h *Hub) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := h.db.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}

	body := requireSig(w, r, srv.RegistrationSecret)
	if body == nil {
		return
	}

	var req HeartbeatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if err := h.db.UpdateHeartbeat(id, req.MemberCount, req.OnlineCount); err != nil {
		ApiLog.Error("heartbeat update failed", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update heartbeat")
		return
	}

	h.stats.Heartbeats.Add(1)
	ApiLog.Debug("heartbeat", "name", srv.Name, "members", req.MemberCount, "online", req.OnlineCount)
	w.WriteHeader(http.StatusNoContent)
}

// GET /v1/servers — list all online servers (local + federated)
func (h *Hub) handleListServers(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	category := q.Get("category")
	query := q.Get("q")
	all := q.Get("all") == "1"

	servers, err := h.db.ListServers(category, query, all)
	if err != nil {
		ApiLog.Error("list servers failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list servers")
		return
	}

	// Build public listings from local servers.
	listings := make([]ServerListing, 0, len(servers))
	for _, s := range servers {
		listings = append(listings, RegisteredServer(*s).ToListing())
	}

	// Append federated servers unless the caller is another hub pulling our list.
	if q.Get("federation") != "1" {
		fed, err := h.db.ListFederatedServers(category, query)
		if err != nil {
			ApiLog.Error("list federated failed", "error", err)
		} else {
			for _, s := range fed {
				listings = append(listings, *s)
			}
		}
	}

	writeJSON(w, http.StatusOK, listings)
}

// GET /v1/servers/{id} — return a single server's public listing
func (h *Hub) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := h.db.GetServer(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	listing := RegisteredServer(*srv).ToListing()
	writeJSON(w, http.StatusOK, listing)
}

// POST /v1/join/{id} — request connection details for a server (rate-limited)
func (h *Hub) handleJoin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !getBucket(ip).allow() {
		h.stats.JoinsRateLimited.Add(1)
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	id := r.PathValue("id")
	srv, err := h.db.GetServer(id)
	if err != nil {
		// Not one of ours — if it's a federated listing, proxy the join to the
		// hub it originated from (one hop max, guarded by the header).
		if r.Header.Get(federationHopHeader) == "" {
			if origin, oerr := h.db.GetFederatedServerOrigin(id); oerr == nil {
				h.proxyJoin(w, id, origin)
				return
			}
		}
		writeError(w, http.StatusNotFound, "server not found")
		return
	}
	if !srv.IsOnline {
		writeError(w, http.StatusServiceUnavailable, "server is offline")
		return
	}

	tokenStr, err := GenerateSecret()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to generate token")
		return
	}

	// Signal the Concord server so it whitelists the token BEFORE we hand out
	// connection details. This both pre-authorizes the client's redeem step and
	// proves the listed host/port actually belongs to the registered server.
	if err := h.signalServer(srv, tokenStr); err != nil {
		ApiLog.Error("join signal to server failed", "id", srv.ID, "error", err)
		writeError(w, http.StatusBadGateway, "server did not respond to join signal")
		return
	}
	h.stats.JoinsServed.Add(1)
	ApiLog.Info("join served", "name", srv.Name, "id", srv.ID)

	jt := &JoinToken{
		Token:     tokenStr,
		ServerID:  srv.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(2 * time.Minute),
	}
	if err := h.db.CreateJoinToken(jt); err != nil {
		ApiLog.Error("create join token failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create token")
		return
	}

	writeJSON(w, http.StatusOK, JoinResponse{
		ServerID:    srv.ID,
		DisplayName: srv.Name,
		Host:        srv.Host,
		Port:        srv.Port,
		JoinToken:   tokenStr,
	})
}

// federationHopHeader marks a join request as already proxied once, so hubs
// never chain proxies (A → B → C) or loop.
const federationHopHeader = "X-Grapevine-Federated"

// proxyJoin forwards a join request for a federated server to the hub that
// listed it and relays the response verbatim. The origin hub performs its own
// rate limiting, token issuance, and server signaling.
func (h *Hub) proxyJoin(w http.ResponseWriter, serverID, originURL string) {
	req, err := http.NewRequest(http.MethodPost, originURL+"/v1/join/"+serverID, nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "invalid origin hub URL")
		return
	}
	req.Header.Set(federationHopHeader, "1")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		ApiLog.Error("proxy join failed", "id", serverID, "via", originURL, "error", err)
		writeError(w, http.StatusBadGateway, "origin hub unreachable")
		return
	}
	defer resp.Body.Close()

	ApiLog.Info("join proxied to origin hub", "id", serverID, "via", originURL, "status", resp.StatusCode)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// signalServer POSTs the join token to the registered Concord server so it can
// whitelist the incoming client. Synchronous: the caller must not reveal
// connection details unless the server acknowledged the token.
func (h *Hub) signalServer(srv *RegisteredServer, token string) error {
	url := fmt.Sprintf("http://%s:%d/v1/grapevine/signal", srv.Host, srv.Port)
	payload := map[string]string{"join_token": token}
	body, _ := json.Marshal(payload)
	sig := SignBody(srv.RegistrationSecret, body)

	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grapevine-Sig", sig)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}

// GET /v1/hubs — list known peer hubs
func (h *Hub) handleListHubs(w http.ResponseWriter, r *http.Request) {
	hubs, err := h.db.ListPeerHubs()
	if err != nil {
		ApiLog.Error("list hubs failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list hubs")
		return
	}

	out := make([]HubListing, 0, len(hubs))
	for _, ph := range hubs {
		if ph.IsActive {
			out = append(out, HubListing{ID: ph.ID, Name: ph.Name, URL: ph.URL})
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// POST /v1/hubs — add a peer hub (requires admin token)
func (h *Hub) handleAddHub(w http.ResponseWriter, r *http.Request) {
	if !h.requireAdmin(w, r) {
		return
	}

	var req struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	ph := &PeerHub{Name: req.Name, URL: req.URL, IsActive: true}
	if err := h.db.UpsertPeerHub(ph); err != nil {
		ApiLog.Error("add peer hub failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add hub")
		return
	}

	ApiLog.Info("peer hub added", "name", req.Name, "url", req.URL)
	w.WriteHeader(http.StatusCreated)
}

// requireAdmin checks the Authorization: Bearer <admin_token> header.
func (h *Hub) requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	if h.config.AdminToken == "" {
		writeError(w, http.StatusServiceUnavailable, "admin access not configured")
		return false
	}
	auth := r.Header.Get("Authorization")
	token, ok := strings.CutPrefix(auth, "Bearer ")
	if !ok || token != h.config.AdminToken {
		writeError(w, http.StatusUnauthorized, "invalid admin token")
		return false
	}
	return true
}
