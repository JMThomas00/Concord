package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Voice relays (2026-10-09). Voice is peer-to-peer WebRTC: STUN lets most
// home connections reach each other directly, but two people behind strict
// routers (symmetric NAT, many offices and schools) need a TURN server to
// relay between them. [voice] in concord-server.toml says which, in any of
// three ways; with none set, voice is STUN-only, as before.
//
//	[voice]
//	stun_urls = ["stun:stun.l.google.com:19302"]   # optional; this is the default
//
//	# 1. Any TURN service with a fixed login:
//	turn_urls     = ["turn:turn.example.com:3478", "turns:turn.example.com:5349"]
//	turn_username = "concord"
//	turn_password = "…"
//
//	# 2. coturn with use-auth-secret (static-auth-secret = the same value):
//	turn_urls   = ["turn:turn.example.com:3478"]
//	turn_secret = "…"
//
//	# 3. Cloudflare's TURN service (Realtime > TURN in the dashboard; works
//	#    wherever the server is hosted):
//	cloudflare_turn_key_id = "…"
//	cloudflare_turn_token  = "…"
type VoiceConfig struct {
	STUNURLs            []string `toml:"stun_urls,omitempty"`
	TURNURLs            []string `toml:"turn_urls,omitempty"`
	TURNUsername        string   `toml:"turn_username,omitempty"`
	TURNPassword        string   `toml:"turn_password,omitempty"`
	TURNSecret          string   `toml:"turn_secret,omitempty"`
	CloudflareTURNKeyID string   `toml:"cloudflare_turn_key_id,omitempty"`
	CloudflareTURNToken string   `toml:"cloudflare_turn_token,omitempty"`
}

// Relayed reports whether the settings include a TURN relay.
func (v VoiceConfig) Relayed() bool {
	return v.CloudflareTURNKeyID != "" || len(v.TURNURLs) > 0
}

const (
	defaultSTUN = "stun:stun.l.google.com:19302"
	// iceTTL is how long a minted TURN login lasts: longer than any call,
	// since a login that expires mid-call can drop it.
	iceTTL = 24 * time.Hour
	// cloudflareTURNAPI is Cloudflare's credential endpoint (%s: the key ID).
	cloudflareTURNAPI = "https://rtc.live.cloudflare.com/v1/turn/keys/%s/credentials/generate-ice-servers"
)

// iceProvider hands out the ICE servers for a voice call.
type iceProvider struct {
	cfg    VoiceConfig
	client *http.Client
	apiURL string // cloudflareTURNAPI, or a test server

	mu      sync.Mutex
	cached  []protocol.ICEServer // Cloudflare's, until refreshAt
	refresh time.Time
}

func newICEProvider(cfg VoiceConfig) *iceProvider {
	return &iceProvider{cfg: cfg, client: &http.Client{Timeout: 5 * time.Second}, apiURL: cloudflareTURNAPI}
}

// servers is the ICE server list for userID's call.
func (p *iceProvider) servers(userID uuid.UUID, now time.Time) []protocol.ICEServer {
	stun := p.cfg.STUNURLs
	if len(stun) == 0 {
		stun = []string{defaultSTUN}
	}
	out := []protocol.ICEServer{{URLs: stun}}

	switch {
	case p.cfg.CloudflareTURNKeyID != "" && p.cfg.CloudflareTURNToken != "":
		if cf := p.cloudflare(now); len(cf) > 0 {
			return cf // Cloudflare's list carries its own STUN too
		}
	case len(p.cfg.TURNURLs) > 0 && p.cfg.TURNSecret != "":
		// coturn's REST-API scheme: the login is "<expiry>:<who>", the
		// password its HMAC-SHA1 under the shared secret.
		user := fmt.Sprintf("%d:%s", now.Add(iceTTL).Unix(), userID)
		mac := hmac.New(sha1.New, []byte(p.cfg.TURNSecret))
		mac.Write([]byte(user))
		out = append(out, protocol.ICEServer{URLs: p.cfg.TURNURLs, Username: user, Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	case len(p.cfg.TURNURLs) > 0:
		out = append(out, protocol.ICEServer{URLs: p.cfg.TURNURLs, Username: p.cfg.TURNUsername, Credential: p.cfg.TURNPassword})
	}
	return out
}

// cloudflare returns Cloudflare's ICE servers, fetching a fresh login when
// the cached one is within two hours of expiring. On failure it keeps an
// older login that's still valid, or returns nil (STUN only).
func (p *iceProvider) cloudflare(now time.Time) []protocol.ICEServer {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.cached) > 0 && now.Before(p.refresh) {
		return p.cached
	}
	fresh, err := p.fetchCloudflare()
	if err != nil {
		Logger.Warn("Voice: couldn't get TURN credentials from Cloudflare; calls behind strict routers may not connect", "error", err)
		if len(p.cached) > 0 && now.Before(p.refresh.Add(2*time.Hour-time.Minute)) {
			return p.cached
		}
		return nil
	}
	p.cached, p.refresh = fresh, now.Add(iceTTL-2*time.Hour)
	return fresh
}

func (p *iceProvider) fetchCloudflare() ([]protocol.ICEServer, error) {
	body, _ := json.Marshal(map[string]int{"ttl": int(iceTTL / time.Second)})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf(p.apiURL, p.cfg.CloudflareTURNKeyID), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.CloudflareTURNToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	// {"iceServers": [ {urls, username, credential}, … ]}, or (older) one
	// object rather than a list.
	var wrap struct {
		ICEServers json.RawMessage `json:"iceServers"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	type cfServer struct {
		URLs       json.RawMessage `json:"urls"`
		Username   string          `json:"username"`
		Credential string          `json:"credential"`
	}
	var list []cfServer
	if err := json.Unmarshal(wrap.ICEServers, &list); err != nil {
		var one cfServer
		if err := json.Unmarshal(wrap.ICEServers, &one); err != nil {
			return nil, fmt.Errorf("unexpected reply: %s", strings.TrimSpace(string(raw)))
		}
		list = []cfServer{one}
	}
	var out []protocol.ICEServer
	for _, s := range list {
		var urls []string
		if json.Unmarshal(s.URLs, &urls) != nil {
			var u string
			if json.Unmarshal(s.URLs, &u) == nil && u != "" {
				urls = []string{u}
			}
		}
		// Port 53 is often blocked or intercepted on home networks, and
		// Cloudflare suggests leaving those addresses out.
		kept := urls[:0]
		for _, u := range urls {
			if !strings.Contains(u, ":53?") && !strings.HasSuffix(u, ":53") {
				kept = append(kept, u)
			}
		}
		if len(kept) > 0 {
			out = append(out, protocol.ICEServer{URLs: kept, Username: s.Username, Credential: s.Credential})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no ICE servers in the reply")
	}
	return out, nil
}

// SetVoiceConfig sets where voice calls find STUN and TURN servers.
func (h *Handlers) SetVoiceConfig(cfg VoiceConfig) {
	h.ice = newICEProvider(cfg)
}

// iceServers is the ICE server list for a user joining voice.
func (h *Handlers) iceServers(userID uuid.UUID) []protocol.ICEServer {
	if h.ice == nil {
		return []protocol.ICEServer{{URLs: []string{defaultSTUN}}}
	}
	return h.ice.servers(userID, time.Now())
}
