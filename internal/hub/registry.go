package hub

import (
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
			RegLog.Error("mark-offline sweep failed", "error", err)
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
			RegLog.Error("token cleanup failed", "error", err)
		}
		if n, err := h.db.PurgeStaleServers(time.Now().Add(-stalePurgeAge)); err != nil {
			RegLog.Error("stale server purge failed", "error", err)
		} else if n > 0 {
			RegLog.Info("purged stale servers", "count", n, "offline_for", stalePurgeAge)
		}
	}
}
