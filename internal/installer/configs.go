package installer

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pelletier/go-toml/v2"
)

// The files the server and hub read from their folder on startup. When one
// is there they skip their first-run wizard, which is what the installer
// relies on.
const (
	ServerConfigFile = "concord-server.toml"
	HubConfigFile    = "grapevine-hub.toml"
)

// serverConfig is the part of internal/server's Config the installer
// writes (configs_test.go holds the two together). Mirrored rather than
// imported, so the installer doesn't carry the server's database.
type serverConfig struct {
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	ServerName     string `toml:"server_name"`
	DatabasePath   string `toml:"database_path"`
	MaxConnections int    `toml:"max_connections"`
	MessagePruning struct {
		Enabled       bool `toml:"enabled"`
		IntervalHours int  `toml:"interval_hours"`
	} `toml:"message_pruning"`
	TermsAccepted bool   `toml:"terms_accepted"`
	AdminEmail    string `toml:"admin_email,omitempty"`
	Grapevine     struct {
		Enabled     bool   `toml:"enabled"`
		HubURL      string `toml:"hub_url,omitempty"`
		PublicHost  string `toml:"public_host,omitempty"`
		PublicPort  int    `toml:"public_port,omitempty"`
		Description string `toml:"description,omitempty"`
		Category    string `toml:"category,omitempty"`
	} `toml:"grapevine"`
	PluginsDir string `toml:"plugins_dir"`
}

// hubConfig mirrors internal/hub's Config.
type hubConfig struct {
	Host             string    `toml:"host"`
	Port             int       `toml:"port"`
	HubName          string    `toml:"hub_name"`
	DatabasePath     string    `toml:"database_path"`
	HeartbeatTimeout int       `toml:"heartbeat_timeout_seconds"`
	FederationSync   int       `toml:"federation_sync_minutes"`
	AdminToken       string    `toml:"admin_token"`
	PeerHubs         []hubPeer `toml:"peer_hubs"`
}

type hubPeer struct {
	Name string `toml:"name"`
	URL  string `toml:"url"`
}

// ServerConfigTOML is the server's settings file for this plan.
func (pl *Plan) ServerConfigTOML(hubURL string) ([]byte, error) {
	var c serverConfig
	c.Host = "0.0.0.0"
	c.Port = pl.ServerPortNum()
	c.ServerName = pl.ServerName
	c.DatabasePath = "concord.db"
	c.MaxConnections = 1000
	c.MessagePruning.Enabled = true
	c.MessagePruning.IntervalHours = 24
	c.TermsAccepted = pl.TermsAccepted
	c.AdminEmail = strings.ToLower(pl.AdminEmail)
	if pl.ServerOnHub {
		c.Grapevine.Enabled = true
		c.Grapevine.HubURL = hubURL
		c.Grapevine.PublicHost = pl.PublicHost
		c.Grapevine.PublicPort = c.Port
		c.Grapevine.Description = pl.Description
		c.Grapevine.Category = pl.Category
	}
	c.PluginsDir = "Plugins"
	return toml.Marshal(c)
}

// HubConfigTOML is the hub's settings file for this plan, and the admin
// token it was given (shown once, at the end).
func (pl *Plan) HubConfigTOML(officialName, officialURL string) ([]byte, string, error) {
	token, err := randomToken()
	if err != nil {
		return nil, "", err
	}
	c := hubConfig{
		Host:             "0.0.0.0",
		Port:             pl.HubPortNum(),
		HubName:          pl.HubName,
		DatabasePath:     "grapevine.db",
		HeartbeatTimeout: 90,
		FederationSync:   5,
		AdminToken:       token,
	}
	if pl.HubFederate {
		c.PeerHubs = []hubPeer{{Name: officialName, URL: officialURL}}
	}
	b, err := toml.Marshal(c)
	return b, token, err
}

func randomToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// ClientServer is one entry for the client's server list.
type ClientServer struct {
	Name    string
	Address string
	Port    int
	TLS     bool
}

// ClientConfigDir is where the client keeps its settings (~/.concord).
func ClientConfigDir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".concord")
}

// AddClientServers adds servers to the client's list in dir
// (servers.json), keeping everything already there. A server already in
// the list (same address and port) isn't added twice. It returns how many
// were added.
func AddClientServers(dir string, servers []ClientServer) (int, error) {
	path := filepath.Join(dir, "servers.json")
	// Read it loosely, so fields the installer doesn't know survive.
	doc := map[string]any{}
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &doc); err != nil {
			return 0, fmt.Errorf("%s isn't valid JSON, so it was left alone: %w", path, err)
		}
	}
	list, _ := doc["servers"].([]any)
	added := 0
	for _, s := range servers {
		dup := false
		for _, e := range list {
			m, _ := e.(map[string]any)
			if m != nil && strings.EqualFold(fmt.Sprint(m["address"]), s.Address) && fmt.Sprint(m["port"]) == fmt.Sprint(s.Port) {
				dup = true
			}
		}
		if dup {
			continue
		}
		list = append(list, map[string]any{
			"id":          newUUID(),
			"name":        s.Name,
			"address":     s.Address,
			"port":        s.Port,
			"use_tls":     s.TLS,
			"order":       len(list),
			"added_at":    time.Now().Format(time.RFC3339Nano),
			"icon_letter": iconLetter(s.Name),
			"icon_color":  iconColor(s.Name),
		})
		added++
	}
	if added == 0 {
		return 0, nil
	}
	doc["servers"] = list
	if _, ok := doc["version"]; !ok {
		doc["version"] = 1
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return 0, err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return 0, err
	}
	return added, os.Rename(tmp, path)
}

// iconLetter and iconColor match the client's own choices
// (NewClientServerInfo, getColorForName).
func iconLetter(name string) string {
	switch {
	case len(name) > 1:
		return name[:2]
	case len(name) == 1:
		return name
	}
	return "S"
}

func iconColor(name string) string {
	colors := []string{"#7289DA", "#99AAB5", "#43B581", "#FAA61A", "#F26522", "#F04747", "#E91E63", "#9B59B6", "#3498DB", "#1ABC9C"}
	hash := len(name)
	if len(name) > 0 {
		hash += int(name[0])
	}
	return colors[hash%len(colors)]
}

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
