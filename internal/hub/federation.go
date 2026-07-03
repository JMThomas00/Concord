package hub

import (
	"encoding/json"
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
			FedLog.Error("list peer hubs failed", "error", err)
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
		FedLog.Error("sync failed", "peer", ph.Name, "url", ph.URL, "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		FedLog.Error("sync failed", "peer", ph.Name, "status", resp.StatusCode)
		return
	}

	var listings []ServerListing
	if err := json.NewDecoder(resp.Body).Decode(&listings); err != nil {
		FedLog.Error("decode listing failed", "peer", ph.Name, "error", err)
		return
	}

	for i := range listings {
		listings[i].FromHub = ph.Name
		if err := h.db.UpsertFederatedServer(ph.ID, &listings[i]); err != nil {
			FedLog.Error("upsert listing failed", "peer", ph.Name, "error", err)
		}
	}

	if err := h.db.MarkHubSynced(ph.ID, time.Now()); err != nil {
		FedLog.Error("mark synced failed", "peer", ph.Name, "error", err)
	}
	h.stats.FederationSyncs.Add(1)
	FedLog.Info("synced listings", "count", len(listings), "peer", ph.Name)
}
