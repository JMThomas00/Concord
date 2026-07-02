package hub

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Hub is the Grapevine hub server.
type Hub struct {
	config *Config
	db     *HubDB
	mux    *http.ServeMux
	srv    *http.Server
	stats  *Stats
}

// New creates a Hub, opens the database, and wires up all routes.
func New(config *Config) (*Hub, error) {
	db, err := NewHubDB(config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	h := &Hub{config: config, db: db, stats: newStats()}
	h.setupRoutes()
	h.srv = &http.Server{
		Addr:         fmt.Sprintf("%s:%d", config.Host, config.Port),
		Handler:      h.mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return h, nil
}

// setupRoutes registers all REST endpoints on the mux.
// Uses Go 1.22+ method+pattern routing so handlers receive only the right verbs.
func (h *Hub) setupRoutes() {
	h.mux = http.NewServeMux()

	h.mux.HandleFunc("GET /v1/health", h.handleHealth)
	h.mux.HandleFunc("POST /v1/servers", h.handleRegister)
	h.mux.HandleFunc("DELETE /v1/servers/{id}", h.handleDeregister)
	h.mux.HandleFunc("POST /v1/servers/{id}/heartbeat", h.handleHeartbeat)
	h.mux.HandleFunc("GET /v1/servers", h.handleListServers)
	h.mux.HandleFunc("GET /v1/servers/{id}", h.handleGetServer)
	h.mux.HandleFunc("POST /v1/join/{id}", h.handleJoin)
	h.mux.HandleFunc("GET /v1/hubs", h.handleListHubs)
	h.mux.HandleFunc("POST /v1/hubs", h.handleAddHub)
}

// Start launches the background maintenance goroutines and begins serving HTTP.
// It also seeds any [[peer_hubs]] from config that aren't already in the DB.
func (h *Hub) Start() error {
	h.seedPeerHubs()

	timeout := time.Duration(h.config.HeartbeatTimeout) * time.Second
	syncInterval := time.Duration(h.config.FederationSync) * time.Minute

	go h.MarkOfflineLoop(timeout)
	go h.CleanupLoop()
	// Always run: peer hubs can be added at runtime via POST /v1/hubs.
	go h.FederationLoop(syncInterval)

	log.Printf("[hub] listening on %s", h.srv.Addr)
	return h.srv.ListenAndServe()
}

// Shutdown gracefully stops the HTTP server within the given context deadline.
func (h *Hub) Shutdown(ctx context.Context) error {
	return h.srv.Shutdown(ctx)
}

// Stats exposes the hub's live counters and log buffer (for the dashboard).
func (h *Hub) Stats() *Stats {
	return h.stats
}

// seedPeerHubs inserts [[peer_hubs]] from config into the DB on first run.
func (h *Hub) seedPeerHubs() {
	for _, pc := range h.config.PeerHubs {
		if pc.URL == "" {
			continue
		}
		ph := &PeerHub{Name: pc.Name, URL: pc.URL, IsActive: true}
		if err := h.db.UpsertPeerHub(ph); err != nil {
			log.Printf("[hub] seed peer hub %s: %v", pc.URL, err)
		} else {
			log.Printf("[hub] peer hub registered: %s (%s)", pc.Name, pc.URL)
		}
	}
}
