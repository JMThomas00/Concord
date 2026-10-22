package hub

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

// A server's description, tags and category used to reach the hub only
// when it registered, which happens once. Its heartbeat now carries them.
func TestAHeartbeatUpdatesTheListing(t *testing.T) {
	_, ts := newTestHub(t, "official")
	id, secret := registerServer(t, ts.URL, RegisterRequest{
		Name: "Grapes", Description: "old words", Category: "gaming", Tags: []string{"chess"},
		Host: "grapes.example.com", Port: 8080,
	})

	beat(t, ts.URL, id, secret, HeartbeatRequest{MemberCount: 3, OnlineCount: 1, Listing: &ListingUpdate{
		Name: "Grapes & Co", Description: "new words", Category: "community", Tags: []string{"chess", "go"},
	}})
	got := listServers(t, ts.URL)
	if len(got) != 1 {
		t.Fatalf("want one server, got %+v", got)
	}
	l := got[0]
	if l.Name != "Grapes & Co" || l.Description != "new words" || l.Category != "community" ||
		!slices.Equal(l.Tags, []string{"chess", "go"}) || l.MaxMembers != 1000 || l.MemberCount != 3 {
		t.Fatalf("listing after the heartbeat: %+v", l)
	}

	// A server from before v0.1.4 sends only its counts: the listing stays.
	beat(t, ts.URL, id, secret, HeartbeatRequest{MemberCount: 4, OnlineCount: 2})
	if l := listServers(t, ts.URL)[0]; l.Description != "new words" || l.MemberCount != 4 {
		t.Fatalf("listing after a counts-only heartbeat: %+v", l)
	}

	// Only the server's own secret can change it.
	body, _ := json.Marshal(HeartbeatRequest{Listing: &ListingUpdate{Name: "Hijacked"}})
	if code := post(t, ts.URL+"/v1/servers/"+id+"/heartbeat", body, sign("wrong", body)); code != http.StatusUnauthorized {
		t.Fatalf("a wrongly signed heartbeat: HTTP %d", code)
	}
	if l := listServers(t, ts.URL)[0]; l.Name != "Grapes & Co" {
		t.Fatalf("a wrongly signed heartbeat renamed the server: %+v", l)
	}
}

func registerServer(t *testing.T, base string, req RegisterRequest) (id, secret string) {
	t.Helper()
	body, _ := json.Marshal(req)
	resp, err := http.Post(base+"/v1/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var reg RegisterResponse
	if err := json.NewDecoder(resp.Body).Decode(&reg); err != nil || reg.ServerID == "" {
		t.Fatalf("register: HTTP %d, %v", resp.StatusCode, err)
	}
	return reg.ServerID, reg.RegistrationSecret
}

func beat(t *testing.T, base, id, secret string, req HeartbeatRequest) {
	t.Helper()
	body, _ := json.Marshal(req)
	if code := post(t, base+"/v1/servers/"+id+"/heartbeat", body, sign(secret, body)); code != http.StatusNoContent {
		t.Fatalf("heartbeat: HTTP %d", code)
	}
}

func post(t *testing.T, url string, body []byte, sig string) int {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Grapevine-Sig", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}
