package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// Limits on one peer's listing per sync.
const (
	maxPeerListingBytes = 4 << 20
	maxPeerListings     = 2000
)

// FederationLoop periodically syncs server listings from all active peer hubs.
// Runs as a goroutine inside Hub.Start().
func (h *Hub) FederationLoop(syncInterval time.Duration) {
	ticker := time.NewTicker(syncInterval)
	defer ticker.Stop()
	for range ticker.C {
		if n, err := h.db.PruneAnnouncedPeers(time.Now().Add(-stalePeerAfter)); err != nil {
			FedLog.Error("prune peer hubs failed", "error", err)
		} else if n > 0 {
			FedLog.Info("dropped unreachable peer hubs", "count", n)
		}
		h.announceToPeers()
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

// federatedFreshSince is how recent a peer's cached listing must be to be
// shown: three missed syncs (15 minutes by default) and its hub counts as
// gone quiet, so its servers stop being listed until it answers again.
func (h *Hub) federatedFreshSince(now time.Time) time.Time {
	window := 3 * time.Duration(h.config.FederationSync) * time.Minute
	return now.Add(-max(window, 15*time.Minute))
}

// syncHub fetches the server listing from a peer hub and caches it locally.
// federation=1 asks the peer for its local servers only, so listings don't
// echo back and forth between hubs.
func (h *Hub) syncHub(ph *PeerHub) {
	start := time.Now()
	resp, err := h.peerClient(ph.URL, 10*time.Second).Get(ph.URL + "/v1/servers?federation=1")
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
	// A peer may be anyone's hub: cap what one sync can bring in.
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxPeerListingBytes)).Decode(&listings); err != nil {
		FedLog.Error("decode listing failed", "peer", ph.Name, "error", err)
		return
	}

	if len(listings) > maxPeerListings {
		listings = listings[:maxPeerListings]
	}
	for i := range listings {
		listings[i].FromHub = ph.Name
		if err := h.db.UpsertFederatedServer(ph.ID, &listings[i]); err != nil {
			FedLog.Error("upsert listing failed", "peer", ph.Name, "error", err)
		}
	}
	// What the peer no longer lists (offline, or gone) stops being listed here.
	if err := h.db.DropFederatedServersBefore(ph.ID, start); err != nil {
		FedLog.Error("drop old listings failed", "peer", ph.Name, "error", err)
	}

	if err := h.db.MarkHubSynced(ph.ID, time.Now()); err != nil {
		FedLog.Error("mark synced failed", "peer", ph.Name, "error", err)
	}
	h.stats.FederationSyncs.Add(1)
	FedLog.Info("synced listings", "count", len(listings), "peer", ph.Name)
}
