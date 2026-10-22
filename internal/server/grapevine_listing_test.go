package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

// The heartbeat carries the server's listing, so a description, category
// or tag changed in concord-server.toml reaches the hub after a restart
// (registering happens only once).
func TestGrapevineHeartbeatCarriesTheListing(t *testing.T) {
	type listing struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Category    string   `json:"category"`
		Tags        []string `json:"tags"`
		MaxMembers  int      `json:"max_members"`
	}
	var got struct {
		MemberCount int      `json:"member_count"`
		OnlineCount int      `json:"online_count"`
		Listing     *listing `json:"listing"`
	}
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/servers/s1/heartbeat" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("heartbeat body: %v", err)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer hub.Close()

	cfg := &GrapevineConfig{
		HubURL: hub.URL, ServerID: "s1", RegistrationSecret: "secret",
		Description: "Board games every night", Category: "gaming", Tags: []string{"chess", "tak"},
	}
	g := newGrapevineClient(cfg, "Grapes", "grapes.example.com", 8080,
		func() int { return 12 }, func() int { return 3 }, nil)
	g.sendHeartbeat()

	if got.MemberCount != 12 || got.OnlineCount != 3 {
		t.Fatalf("counts: %+v", got)
	}
	l := got.Listing
	if l == nil || l.Name != "Grapes" || l.Description != "Board games every night" || l.Category != "gaming" ||
		!slices.Equal(l.Tags, []string{"chess", "tak"}) || l.MaxMembers != 1000 {
		t.Fatalf("listing: %+v", l)
	}
}
