package hub

import (
	"bytes"
	"os"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"
)

func init() { InitLogger(os.Stderr, log.WarnLevel) }

func newTestHub(t *testing.T, name string) (*Hub, *httptest.Server) {
	t.Helper()
	cfg := DefaultConfig()
	cfg.HubName = name
	cfg.DatabasePath = filepath.Join(t.TempDir(), "hub.db")
	cfg.AdminToken = "admin-" + name
	h, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(h.mux)
	t.Cleanup(func() { ts.Close(); h.db.Close() })
	h.config.PublicURL = ts.URL
	return h, ts
}

func announce(t *testing.T, to, url string) int {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"name": "ignored", "url": url})
	resp, err := http.Post(to+"/v1/hubs/announce", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func listHubs(t *testing.T, base string) []HubListing {
	t.Helper()
	resp, err := http.Get(base + "/v1/hubs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out []HubListing
	json.NewDecoder(resp.Body).Decode(&out)
	return out
}

func withPrivatePeers(t *testing.T) {
	allowPrivatePeers = true
	t.Cleanup(func() { allowPrivatePeers = false })
}

// A hub announcing itself is checked and added, under the name its health
// endpoint gives; announcing again changes nothing.
func TestAnnounceAddsHub(t *testing.T) {
	withPrivatePeers(t)
	_, official := newTestHub(t, "Official")
	_, newer := newTestHub(t, "Newer Hub")

	if code := announce(t, official.URL, newer.URL+"/"); code != http.StatusCreated {
		t.Fatalf("announce: HTTP %d", code)
	}
	hubs := listHubs(t, official.URL)
	if len(hubs) != 1 || hubs[0].URL != newer.URL || hubs[0].Name != "Newer Hub" {
		t.Fatalf("hubs after announce: %+v", hubs)
	}
	if code := announce(t, official.URL, newer.URL); code != http.StatusOK {
		t.Fatalf("second announce: HTTP %d", code)
	}
	if n := len(listHubs(t, official.URL)); n != 1 {
		t.Fatalf("second announce added a duplicate: %d hubs", n)
	}
}

// announceToPeers is what a new hub does: it tells the hub it connected to,
// which adds it back.
func TestAnnounceToPeers(t *testing.T) {
	withPrivatePeers(t)
	_, official := newTestHub(t, "Official")
	newer, _ := newTestHub(t, "Newer Hub")
	if err := newer.db.UpsertPeerHub(&PeerHub{Name: "Official", URL: official.URL, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	newer.announceToPeers()
	deadline := time.Now().Add(5 * time.Second)
	for len(listHubs(t, official.URL)) == 0 && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if hubs := listHubs(t, official.URL); len(hubs) != 1 || hubs[0].URL != newer.config.PublicURL {
		t.Fatalf("official hub didn't add the new one: %+v", hubs)
	}
}

// Without the test hook, a hub on a private or loopback address is refused,
// so announcements can't point this hub at its own network.
func TestAnnounceRefusesPrivateAddresses(t *testing.T) {
	_, official := newTestHub(t, "Official")
	_, local := newTestHub(t, "Local")
	if code := announce(t, official.URL, local.URL); code != http.StatusBadRequest {
		t.Fatalf("loopback hub accepted: HTTP %d", code)
	}
	if n := len(listHubs(t, official.URL)); n != 0 {
		t.Fatalf("loopback hub listed: %d hubs", n)
	}
	for _, a := range []string{"127.0.0.1", "10.0.0.5", "192.168.1.56", "172.16.0.1", "169.254.169.254", "100.64.0.1", "::1", "fd00::1", "0.0.0.0"} {
		if publicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s counted as public", a)
		}
	}
	for _, a := range []string{"179.236.240.211", "1.1.1.1", "2606:4700::1111"} {
		if !publicAddr(netip.MustParseAddr(a)) {
			t.Errorf("%s counted as private", a)
		}
	}
}

// A hub an admin blocked stays blocked when it announces again.
func TestBlockedHubStaysBlocked(t *testing.T) {
	withPrivatePeers(t)
	_, official := newTestHub(t, "Official")
	_, newer := newTestHub(t, "Newer Hub")
	announce(t, official.URL, newer.URL)
	id := listHubs(t, official.URL)[0].ID

	req, _ := http.NewRequest(http.MethodDelete, official.URL+"/v1/hubs/"+id, nil)
	req.Header.Set("Authorization", "Bearer admin-Official")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("block: HTTP %d", resp.StatusCode)
	}
	announce(t, official.URL, newer.URL)
	if n := len(listHubs(t, official.URL)); n != 0 {
		t.Fatalf("blocked hub came back: %d hubs", n)
	}
}

func TestAnnounceTurnedOff(t *testing.T) {
	withPrivatePeers(t)
	h, official := newTestHub(t, "Official")
	_, newer := newTestHub(t, "Newer Hub")
	h.config.AcceptPeerAnnouncements = false
	if code := announce(t, official.URL, newer.URL); code != http.StatusForbidden {
		t.Fatalf("announce with the setting off: HTTP %d", code)
	}
}

// Hubs that added themselves and stopped answering are dropped after a week;
// hubs an admin added aren't.
func TestPruneAnnouncedPeers(t *testing.T) {
	h, _ := newTestHub(t, "Official")
	if _, err := h.db.AddAnnouncedPeer("Gone", "https://gone.example"); err != nil {
		t.Fatal(err)
	}
	h.db.UpsertPeerHub(&PeerHub{Name: "Static", URL: "https://static.example", IsActive: true})
	h.db.Exec(`UPDATE peer_hubs SET registered_at=?`, time.Now().Add(-8*24*time.Hour).UTC())
	if n, err := h.db.PruneAnnouncedPeers(time.Now().Add(-stalePeerAfter)); err != nil || n != 1 {
		t.Fatalf("pruned %d, %v", n, err)
	}
	hubs, _ := h.db.ListPeerHubs()
	if len(hubs) != 1 || hubs[0].Name != "Static" {
		t.Fatalf("left: %+v", hubs)
	}
}

func TestNormalizeHubURL(t *testing.T) {
	good := map[string]string{"https://Hub.Example.com/": "https://hub.example.com", "http://1.2.3.4:7777": "http://1.2.3.4:7777"}
	for in, want := range good {
		if got, err := normalizeHubURL(in); err != nil || got != want {
			t.Errorf("%q: %q, %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "ftp://x", "https://", "https://u:p@hub.example", "https://hub.example/path", "https://hub.example?x=1", "hub.example"} {
		if _, err := normalizeHubURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
