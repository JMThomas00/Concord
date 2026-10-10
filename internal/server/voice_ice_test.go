package server

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestVoiceWithoutTURNIsSTUNOnly(t *testing.T) {
	got := newICEProvider(VoiceConfig{}).servers(uuid.New(), time.Now())
	if len(got) != 1 || got[0].URLs[0] != defaultSTUN || got[0].Username != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestStaticTURNLogin(t *testing.T) {
	got := newICEProvider(VoiceConfig{TURNURLs: []string{"turn:t.example.com:3478"}, TURNUsername: "u", TURNPassword: "p"}).servers(uuid.New(), time.Now())
	if len(got) != 2 || got[1].URLs[0] != "turn:t.example.com:3478" || got[1].Username != "u" || got[1].Credential != "p" {
		t.Fatalf("got %+v", got)
	}
}

// coturn's use-auth-secret: the login is "<expiry>:<user>", the password
// base64(HMAC-SHA1(secret, login)), and the expiry about a day ahead.
func TestCoturnSharedSecretLogin(t *testing.T) {
	now, user := time.Unix(1_800_000_000, 0), uuid.New()
	got := newICEProvider(VoiceConfig{TURNURLs: []string{"turn:t.example.com"}, TURNSecret: "s3cret"}).servers(user, now)
	if len(got) != 2 {
		t.Fatalf("got %+v", got)
	}
	login := got[1].Username
	if !strings.HasSuffix(login, ":"+user.String()) || !strings.HasPrefix(login, "1800086400:") {
		t.Errorf("login %q", login)
	}
	mac := hmac.New(sha1.New, []byte("s3cret"))
	mac.Write([]byte(login))
	if got[1].Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("password isn't the login's HMAC-SHA1")
	}
}

// Cloudflare: the server asks for a login with its key, keeps it until two
// hours before it expires, leaves out port-53 addresses, and falls back to
// STUN when Cloudflare can't be reached and nothing's cached.
func TestCloudflareTURN(t *testing.T) {
	var calls atomic.Int32
	fail := atomic.Bool{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if fail.Load() {
			http.Error(w, "nope", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "Bearer tok" || !strings.Contains(r.URL.Path, "/keys/kid/") {
			t.Errorf("request %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var body map[string]int
		json.NewDecoder(r.Body).Decode(&body)
		if body["ttl"] != 86400 {
			t.Errorf("ttl %v", body)
		}
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"iceServers":[{"urls":["stun:stun.cloudflare.com:3478","stun:stun.cloudflare.com:53"]},
			{"urls":["turn:turn.cloudflare.com:3478?transport=udp","turn:turn.cloudflare.com:53?transport=udp","turns:turn.cloudflare.com:443?transport=tcp"],"username":"cfu","credential":"cfp"}]}`))
	}))
	defer api.Close()
	p := newICEProvider(VoiceConfig{CloudflareTURNKeyID: "kid", CloudflareTURNToken: "tok"})
	p.apiURL = api.URL + "/keys/%s/credentials/generate-ice-servers"

	now := time.Now()
	got := p.servers(uuid.New(), now)
	if len(got) != 2 || got[1].Username != "cfu" || got[1].Credential != "cfp" {
		t.Fatalf("got %+v", got)
	}
	for _, s := range got {
		for _, u := range s.URLs {
			if strings.Contains(u, ":53") {
				t.Errorf("a port-53 address was kept: %s", u)
			}
		}
	}
	p.servers(uuid.New(), now.Add(time.Hour))
	if calls.Load() != 1 {
		t.Errorf("%d requests within the login's life, want 1 (cached)", calls.Load())
	}
	fail.Store(true)
	if got := p.servers(uuid.New(), now.Add(23*time.Hour)); len(got) != 2 {
		t.Errorf("a failed refresh dropped a login that's still valid: %+v", got)
	}
	fresh := newICEProvider(VoiceConfig{CloudflareTURNKeyID: "kid", CloudflareTURNToken: "tok"})
	fresh.apiURL = p.apiURL
	if got := fresh.servers(uuid.New(), now); len(got) != 1 || got[0].URLs[0] != defaultSTUN {
		t.Errorf("unreachable Cloudflare, nothing cached: %+v, want STUN only", got)
	}
}

// TestCloudflareTURNLive asks the real Cloudflare for a login, through the
// same code a voice join uses. It runs only with a key in the environment:
// CONCORD_CF_TURN_KEY_ID and CONCORD_CF_TURN_TOKEN.
func TestCloudflareTURNLive(t *testing.T) {
	id, tok := os.Getenv("CONCORD_CF_TURN_KEY_ID"), os.Getenv("CONCORD_CF_TURN_TOKEN")
	if id == "" || tok == "" {
		t.Skip("set CONCORD_CF_TURN_KEY_ID and CONCORD_CF_TURN_TOKEN to try the real Cloudflare")
	}
	got := newICEProvider(VoiceConfig{CloudflareTURNKeyID: id, CloudflareTURNToken: tok}).servers(uuid.New(), time.Now())
	turn := false
	for _, s := range got {
		for _, u := range s.URLs {
			if strings.HasPrefix(u, "turn") && s.Username != "" && s.Credential != "" {
				turn = true
			}
		}
	}
	if !turn {
		t.Fatalf("no TURN server with a login: %+v", got)
	}
	t.Logf("%d ICE server entries, TURN with a login: ok", len(got))
}
