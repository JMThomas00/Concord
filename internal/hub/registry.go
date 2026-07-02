package hub

import (
	"log"
	"time"
)

// MarkOfflineLoop marks servers offline that haven't heartbeated within timeout.
// Runs as a goroutine inside Hub.Start().
func (h *Hub) MarkOfflineLoop(timeout time.Duration) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		cutoff := time.Now().Add(-timeout)
		if err := h.db.MarkOfflineIfStale(cutoff); err != nil {
			log.Printf("[hub] mark-offline sweep: %v", err)
		}
	}
}

// stalePurgeAge is how long a server may stay offline (no heartbeats) before
// its registration is deleted entirely.
const stalePurgeAge = 30 * 24 * time.Hour

// CleanupLoop removes expired join tokens and long-dead server registrations.
// Runs as a goroutine inside Hub.Start().
func (h *Hub) CleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		if err := h.db.CleanupExpiredTokens(); err != nil {
			log.Printf("[hub] token cleanup: %v", err)
		}
		if n, err := h.db.PurgeStaleServers(time.Now().Add(-stalePurgeAge)); err != nil {
			log.Printf("[hub] stale server purge: %v", err)
		} else if n > 0 {
			log.Printf("[hub] purged %d server(s) offline for over %s", n, stalePurgeAge)
		}
	}
}
