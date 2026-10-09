package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// One server reached through two peers (here one hub that announced itself
// at two tunnel addresses) used to be listed twice on concordchat.cc.
func TestAServerFromTwoPeersIsListedOnce(t *testing.T) {
	h, ts := newTestHub(t, "official")
	srv := &ServerListing{ID: "87fd6137-8d98-4ae2-ab9a-5f3d3d16b424", Name: "Sequoia", IsOnline: true, OnlineCount: 8}
	for _, url := range []string{"https://old.example.com", "https://new.example.com"} {
		if err := h.db.UpsertPeerHub(&PeerHub{Name: "Sequoia Hub", URL: url, IsActive: true}); err != nil {
			t.Fatal(err)
		}
	}
	peers, _ := h.db.ListPeerHubs()
	for _, p := range peers {
		if err := h.db.UpsertFederatedServer(p.ID, srv); err != nil {
			t.Fatal(err)
		}
	}

	resp, err := http.Get(ts.URL + "/v1/servers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []ServerListing
	json.NewDecoder(resp.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "Sequoia" {
		t.Fatalf("want Sequoia once, got %+v", list)
	}
}

// A peer that stops answering (Jordan's PC shut down for the night) left its
// servers listed as online forever. After three missed syncs they're gone.
func TestAQuietPeersServersStopBeingListed(t *testing.T) {
	h, ts := newTestHub(t, "official")
	if err := h.db.UpsertPeerHub(&PeerHub{Name: "Sequoia Hub", URL: "https://sequoia.example.com", IsActive: true}); err != nil {
		t.Fatal(err)
	}
	peers, _ := h.db.ListPeerHubs()
	if err := h.db.UpsertFederatedServer(peers[0].ID, &ServerListing{ID: "s1", Name: "Sequoia", IsOnline: true}); err != nil {
		t.Fatal(err)
	}
	if n := len(listServers(t, ts.URL)); n != 1 {
		t.Fatalf("a fresh listing: %d servers, want 1", n)
	}
	old := time.Now().Add(-time.Hour).UTC()
	if _, err := h.db.Exec(`UPDATE federated_servers SET cached_at=?`, old); err != nil {
		t.Fatal(err)
	}
	if list := listServers(t, ts.URL); len(list) != 0 {
		t.Fatalf("an hour without a sync, still listed: %+v", list)
	}
}

// A server that goes offline drops out of its own hub's listing; the next
// sync used to leave the old copy here, still online.
func TestASyncDropsWhatThePeerNoLongerLists(t *testing.T) {
	h, ts := newTestHub(t, "official")
	listing := `[{"id":"s1","name":"Sequoia","is_online":true},{"id":"s2","name":"Oak","is_online":true}]`
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(listing))
	}))
	defer peer.Close()
	if err := h.db.UpsertPeerHub(&PeerHub{Name: "Home Hub", URL: peer.URL, IsActive: true}); err != nil {
		t.Fatal(err)
	}
	peers, _ := h.db.ListPeerHubs()
	h.syncHub(peers[0])
	if n := len(listServers(t, ts.URL)); n != 2 {
		t.Fatalf("after the first sync: %d servers, want 2", n)
	}
	listing = `[{"id":"s2","name":"Oak","is_online":true}]`
	time.Sleep(10 * time.Millisecond)
	h.syncHub(peers[0])
	if list := listServers(t, ts.URL); len(list) != 1 || list[0].Name != "Oak" {
		t.Fatalf("after Sequoia went offline: %+v, want only Oak", list)
	}
}

func listServers(t *testing.T, base string) []ServerListing {
	t.Helper()
	resp, err := http.Get(base + "/v1/servers")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var list []ServerListing
	json.NewDecoder(resp.Body).Decode(&list)
	return list
}
