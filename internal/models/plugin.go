package models

import (
	"time"

	"github.com/google/uuid"
)

// Plugin process/health status values stored in installed_plugins.status.
const (
	PluginStatusStopped  = "stopped"  // Not running (disabled, or not yet started)
	PluginStatusStarting = "starting" // Process launch about to be attempted
	PluginStatusSpawned  = "spawned"  // OS process launched, awaiting OpIdentify over its own WebSocket connection
	PluginStatusRunning  = "running"  // Successfully identified to Concord and healthy
	PluginStatusCrashed  = "crashed"  // Exited unexpectedly, exhausted restart attempts
)

// InstalledPlugin represents a plugin discovered under the server's Plugins
// directory, tracked across restarts so its service-account identity and
// enabled/disabled state persist.
type InstalledPlugin struct {
	ID            string    `json:"id"` // plugin.toml [plugin].id; stable across restarts
	Name          string    `json:"name"`
	Version       string    `json:"version"`
	ManifestPath  string    `json:"manifest_path"`
	Enabled       bool      `json:"enabled"`
	AuthTokenHash string    `json:"-"` // Never serialize
	ServiceUserID uuid.UUID `json:"service_user_id,omitempty"`
	Status        string    `json:"status"`
	LastError     string    `json:"last_error,omitempty"`
	DiscoveredAt  time.Time `json:"discovered_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}
