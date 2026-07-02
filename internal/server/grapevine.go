package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// GrapevineConfig holds the server's Grapevine registration settings.
// Maps to the [grapevine] section of concord-server.toml.
type GrapevineConfig struct {
	Enabled            bool     `toml:"enabled"`
	HubURL             string   `toml:"hub_url"`
	ServerID           string   `toml:"server_id"`
	RegistrationSecret string   `toml:"registration_secret"`
	PublicHost         string   `toml:"public_host"`
	PublicPort         int      `toml:"public_port"`
	Description        string   `toml:"description"`
	Category           string   `toml:"category"`
	Tags               []string `toml:"tags"`
	HeartbeatInterval  int      `toml:"heartbeat_interval_seconds"`
	MaxMembers         int      `toml:"max_members"`
}

// tokenStore holds short-lived join tokens pre-authorized by the hub.
type tokenStore struct {
	mu     sync.Mutex
	tokens map[string]time.Time // token → expiry
}

func newTokenStore() *tokenStore {
	return &tokenStore{tokens: make(map[string]time.Time)}
}

func (ts *tokenStore) add(token string, expiry time.Time) {
	ts.mu.Lock()
	ts.tokens[token] = expiry
	ts.mu.Unlock()
}

// consume validates a token and removes it (single-use). Returns true if the
// token existed and had not expired.
func (ts *tokenStore) consume(token string) bool {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	exp, ok := ts.tokens[token]
	if !ok {
		return false
	}
	delete(ts.tokens, token)
	return time.Now().Before(exp)
}

func (ts *tokenStore) purgeExpired() {
	now := time.Now()
	ts.mu.Lock()
	for k, exp := range ts.tokens {
		if now.After(exp) {
			delete(ts.tokens, k)
		}
	}
	ts.mu.Unlock()
}

// GrapevineClient manages the server's relationship with a Grapevine hub.
type GrapevineClient struct {
	cfg            *GrapevineConfig
	serverName     string
	publicHost     string
	publicPort     int
	httpClient     *http.Client
	stopCh         chan struct{}
	getMemberCount func() int
	getOnlineCount func() int
	onSaveConfig   func() error
}

// newGrapevineClient creates a GrapevineClient ready to be started.
func newGrapevineClient(
	cfg *GrapevineConfig,
	serverName, publicHost string,
	publicPort int,
	getMemberCount, getOnlineCount func() int,
	saveConfig func() error,
) *GrapevineClient {
	return &GrapevineClient{
		cfg:            cfg,
		serverName:     serverName,
		publicHost:     publicHost,
		publicPort:     publicPort,
		httpClient:     &http.Client{Timeout: 10 * time.Second},
		stopCh:         make(chan struct{}),
		getMemberCount: getMemberCount,
		getOnlineCount: getOnlineCount,
		onSaveConfig:   saveConfig,
	}
}

// Start launches the Grapevine client in a goroutine.
// Registers with the hub if this is the first run (no ServerID), then begins heartbeating.
func (g *GrapevineClient) Start() {
	go func() {
		if g.cfg.ServerID == "" {
			if err := g.register(); err != nil {
				Logger.Error("[Grapevine] registration failed", "error", err)
				return
			}
		}
		Logger.Info("[Grapevine] connected to hub",
			"hub", g.cfg.HubURL, "server_id", g.cfg.ServerID)
		g.heartbeatLoop()
	}()
}

// Stop signals the heartbeat loop to stop and sends a deregister to the hub.
func (g *GrapevineClient) Stop() {
	close(g.stopCh)
}

func (g *GrapevineClient) register() error {
	maxMembers := g.cfg.MaxMembers
	if maxMembers <= 0 {
		maxMembers = 1000
	}
	payload := map[string]interface{}{
		"name":        g.serverName,
		"description": g.cfg.Description,
		"category":    g.cfg.Category,
		"tags":        g.cfg.Tags,
		"host":        g.publicHost,
		"port":        g.publicPort,
		"max_members": maxMembers,
	}
	body, _ := json.Marshal(payload)

	resp, err := g.httpClient.Post(g.cfg.HubURL+"/v1/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("POST /v1/servers: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}

	var reg struct {
		ServerID           string `json:"server_id"`
		RegistrationSecret string `json:"registration_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil {
		return fmt.Errorf("decode register response: %w", err)
	}
	if reg.ServerID == "" || reg.RegistrationSecret == "" {
		return fmt.Errorf("hub returned empty credentials")
	}

	g.cfg.ServerID = reg.ServerID
	g.cfg.RegistrationSecret = reg.RegistrationSecret
	Logger.Info("[Grapevine] registered", "server_id", reg.ServerID)

	if g.onSaveConfig != nil {
		if err := g.onSaveConfig(); err != nil {
			Logger.Error("[Grapevine] could not persist registration to config", "error", err)
		}
	}
	return nil
}

func (g *GrapevineClient) heartbeatLoop() {
	interval := 30 * time.Second
	if g.cfg.HeartbeatInterval > 0 {
		interval = time.Duration(g.cfg.HeartbeatInterval) * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	g.sendHeartbeat() // immediate first beat

	for {
		select {
		case <-g.stopCh:
			g.deregister()
			return
		case <-ticker.C:
			g.sendHeartbeat()
		}
	}
}

func (g *GrapevineClient) sendHeartbeat() {
	payload := map[string]int{
		"member_count": g.getMemberCount(),
		"online_count": g.getOnlineCount(),
	}
	body, _ := json.Marshal(payload)

	url := fmt.Sprintf("%s/v1/servers/%s/heartbeat", g.cfg.HubURL, g.cfg.ServerID)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grapevine-Sig", g.sign(body))

	resp, err := g.httpClient.Do(req)
	if err != nil {
		Logger.Warn("[Grapevine] heartbeat failed", "error", err)
		return
	}
	resp.Body.Close()

	// The hub no longer recognises us (404: database reset; 401: secret
	// mismatch). Re-register from scratch to obtain fresh credentials;
	// next tick retries on failure.
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusUnauthorized {
		Logger.Warn("[Grapevine] hub lost our registration, re-registering",
			"server_id", g.cfg.ServerID)
		if err := g.register(); err != nil {
			Logger.Error("[Grapevine] re-registration failed", "error", err)
		}
		return
	}

	Logger.Debug("[Grapevine] heartbeat sent",
		"members", payload["member_count"], "online", payload["online_count"])
}

func (g *GrapevineClient) deregister() {
	body := []byte("{}")
	url := fmt.Sprintf("%s/v1/servers/%s", g.cfg.HubURL, g.cfg.ServerID)
	req, err := http.NewRequest(http.MethodDelete, url, bytes.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grapevine-Sig", g.sign(body))

	resp, err := g.httpClient.Do(req)
	if err != nil {
		Logger.Warn("[Grapevine] deregister failed", "error", err)
		return
	}
	resp.Body.Close()
	Logger.Info("[Grapevine] deregistered from hub")
}

func (g *GrapevineClient) sign(body []byte) string {
	mac := hmac.New(sha256.New, []byte(g.cfg.RegistrationSecret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// ── Hub → Server HTTP handlers ─────────────────────────────────────────────────

// handleGrapevinePing handles GET /v1/grapevine/ping — liveness check for the hub.
func (s *Server) handleGrapevinePing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":      "ok",
		"server_name": s.config.ServerName,
	})
}

// handleGrapevineSignal handles POST /v1/grapevine/signal.
// The hub calls this to pre-authorize a join token before giving connection details to a client.
// Body: {"join_token": "<token>"}
// Header: X-Grapevine-Sig: HMAC-SHA256(registration_secret, body) — the same
// scheme the server uses for heartbeats, so no extra shared secret is needed.
func (s *Server) handleGrapevineSignal(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	raw, err := io.ReadAll(io.LimitReader(r.Body, 4096))
	if err != nil {
		http.Error(w, "cannot read body", http.StatusBadRequest)
		return
	}

	// Verify the hub's HMAC. The registration secret is only known to this
	// server and the hub it registered with.
	secret := s.config.Grapevine.RegistrationSecret
	if secret == "" {
		http.Error(w, "not registered", http.StatusForbidden)
		return
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(raw)
	expected := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(r.Header.Get("X-Grapevine-Sig"))) {
		http.Error(w, "invalid signature", http.StatusForbidden)
		return
	}

	var body struct {
		JoinToken string `json:"join_token"`
	}
	if err := json.Unmarshal(raw, &body); err != nil || body.JoinToken == "" {
		http.Error(w, "missing join_token", http.StatusBadRequest)
		return
	}

	// The client redeems the token right after the hub's join response, so a
	// short TTL is plenty; the hub-side token record expires in 2 minutes.
	s.grapevineTokens.add(body.JoinToken, time.Now().Add(35*time.Second))
	Logger.Debug("[Grapevine] join token pre-authorized", "prefix", body.JoinToken[:min8(body.JoinToken)])
	w.WriteHeader(http.StatusOK)
}

// handleGrapevineJoin handles POST /v1/grapevine/join.
// A client redeems the join token it received from the hub. The token is
// single-use and only exists if the hub signaled us moments earlier, which
// verifies the client obtained this server's address through the hub.
// Body: {"join_token": "<token>"}
func (s *Server) handleGrapevineJoin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body struct {
		JoinToken string `json:"join_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil || body.JoinToken == "" {
		http.Error(w, "missing join_token", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if !s.grapevineTokens.consume(body.JoinToken) {
		Logger.Warn("[Grapevine] join with invalid or expired token")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "invalid or expired join token"})
		return
	}

	Logger.Info("[Grapevine] client joined via hub referral")
	json.NewEncoder(w).Encode(map[string]string{
		"status":      "ok",
		"server_name": s.config.ServerName,
	})
}

// min8 returns the min of 8 and len(s) for safe prefix logging.
func min8(s string) int {
	if len(s) < 8 {
		return len(s)
	}
	return 8
}
