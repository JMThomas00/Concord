package hub

import (
	"os"

	"github.com/pelletier/go-toml/v2"
)

// PeerHubConfig holds a statically-configured peer hub from grapevine-hub.toml.
type PeerHubConfig struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// Config is loaded from grapevine-hub.toml.
type Config struct {
	Host             string          `toml:"host"`
	Port             int             `toml:"port"`
	HubName          string          `toml:"hub_name"`
	DatabasePath     string          `toml:"database_path"`
	HeartbeatTimeout int             `toml:"heartbeat_timeout_seconds"`
	FederationSync   int             `toml:"federation_sync_minutes"`
	Debug            bool            `toml:"debug"`
	AdminToken       string          `toml:"admin_token"`
	PeerHubs         []PeerHubConfig `toml:"peer_hubs"`
	// RealIPHeader: the header a trusted proxy sets to each visitor's
	// address ("CF-Connecting-IP" behind a Cloudflare Tunnel). Leave it
	// empty unless the hub is reachable only through that proxy.
	RealIPHeader string `toml:"real_ip_header,omitempty"`
	// PublicURL is the address other hubs reach this one at, such as
	// "https://hub.example.com". When set, the hub announces itself to its
	// peer hubs, so they add it back and share its listings too.
	PublicURL string `toml:"public_url,omitempty"`
	// AcceptPeerAnnouncements lets other hubs add themselves as peers
	// (POST /v1/hubs/announce) once they answer a health check at a public
	// address. On by default; an admin can block one with DELETE /v1/hubs/{id}.
	AcceptPeerAnnouncements bool `toml:"accept_peer_announcements"`
}

func DefaultConfig() *Config {
	return &Config{
		Host:             "0.0.0.0",
		Port:             7777,
		HubName:          "Grapevine Hub",
		DatabasePath:     "grapevine.db",
		HeartbeatTimeout: 90,
		FederationSync:   5,

		AcceptPeerAnnouncements: true,
	}
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *Config) Save(path string) error {
	data, err := toml.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
