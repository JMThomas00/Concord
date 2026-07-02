package hub

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

// FederationLoop periodically syncs server listings from all active peer hubs.
// Runs as a goroutine inside Hub.Start().
func (h *Hub) FederationLoop(syncInterval time.Duration) {
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for range ticker.C {
		hubs, err := h.db.ListPeerHubs()
		if err != nil {
			log.Printf("[hub/federation] list peer hubs: %v", err)
			continue
		}
		for _, ph := range hubs {
			if !ph.IsActive {
				continue
			}
			go h.syncHub(ph)
		}
	}
}

// syncHub fetches the server listing from a peer hub and caches it locally.
// federation=1 asks the peer for its local servers only, so listings don't
// echo back and forth between hubs.
func (h *Hub) syncHub(ph *PeerHub) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(ph.URL + "/v1/servers?federation=1")
	if err != nil {
		log.Printf("[hub/federation] sync %s (%s): %v", ph.Name, ph.URL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[hub/federation] sync %s: HTTP %d", ph.Name, resp.StatusCode)
		return
	}

	var listings []ServerListing
	if err := json.NewDecoder(resp.Body).Decode(&listings); err != nil {
		log.Printf("[hub/federation] decode from %s: %v", ph.Name, err)
		return
	}

	for i := range listings {
		listings[i].FromHub = ph.Name
		if err := h.db.UpsertFederatedServer(ph.ID, &listings[i]); err != nil {
			log.Printf("[hub/federation] upsert from %s: %v", ph.Name, err)
		}
	}

	if err := h.db.MarkHubSynced(ph.ID, time.Now()); err != nil {
		log.Printf("[hub/federation] mark synced %s: %v", ph.Name, err)
	}
	h.stats.FederationSyncs.Add(1)
	log.Printf("[hub/federation] synced %d servers from %s", len(listings), ph.Name)
}
