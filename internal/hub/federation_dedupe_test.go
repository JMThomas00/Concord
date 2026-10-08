package hub

import (
	"encoding/json"
	"net/http"
	"testing"
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
