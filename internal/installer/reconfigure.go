package installer

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/concord-chat/concord/internal/official"
	"github.com/pelletier/go-toml/v2"
)

// Configuring again (the client's Updates page, or `concord-install
// --configure`): the form starts from the server's and hub's current
// settings, and installing changes only the settings the form asks about,
// leaving the rest of each file (Grapevine registration, mail, pruning…)
// as it was.

// LoadSettings fills the plan's questions from an installed server's and
// hub's settings files.
func (pl *Plan) LoadSettings() {
	if m := readTOML(filepath.Join(pl.ServerDir, ServerConfigFile)); m != nil {
		pl.ServerName = str(m["server_name"], pl.ServerName)
		if p := num(m["port"]); p > 0 {
			pl.ServerPort, pl.oldServerPort = strconv.Itoa(p), strconv.Itoa(p)
		}
		pl.AdminEmail = str(m["admin_email"], "")
		if g, ok := m["grapevine"].(map[string]any); ok {
			pl.ServerOnHub, _ = g["enabled"].(bool)
			pl.PublicHost = str(g["public_host"], "")
			pl.Description = str(g["description"], "")
			var other []string
			pl.Tags = nil
			if tags, ok := g["tags"].([]any); ok {
				for _, t := range tags {
					if s := str(t, ""); s != "" {
						if isPresetTag(s) {
							pl.Tags = append(pl.Tags, s)
						} else {
							other = append(other, s)
						}
					}
				}
			}
			pl.OtherTags = strings.Join(other, ", ")
		}
		pl.TermsAccepted, _ = m["terms_accepted"].(bool)
	}
	if m := readTOML(filepath.Join(pl.HubDir, HubConfigFile)); m != nil {
		pl.HubName = str(m["hub_name"], pl.HubName)
		if p := num(m["port"]); p > 0 {
			pl.HubPort, pl.oldHubPort = strconv.Itoa(p), strconv.Itoa(p)
		}
		pl.HubAddress = str(m["public_url"], pl.HubAddress)
		pl.HubFederate = false
		if peers, ok := m["peer_hubs"].([]any); ok {
			for _, p := range peers {
				if pm, ok := p.(map[string]any); ok && str(pm["url"], "") == official.HubURL {
					pl.HubFederate = true
				}
			}
		}
	}
}

// PortUnchanged reports whether port is the one the installed component
// already listens on (so "something already uses it" is itself).
func (pl *Plan) PortUnchanged(component, port string) bool {
	old := pl.oldServerPort
	if component == Hub {
		old = pl.oldHubPort
	}
	return pl.Reconfigure && old != "" && strings.TrimSpace(port) == old
}

// PresetTags are the Grapevine tags the form offers; any others a server
// has are shown as typed ones.
var PresetTags = []string{"Gaming", "Technology", "Programming", "Art", "Music", "Tabletop", "Anime", "Education", "Science", "Community", "Friends", "Chill"}

func isPresetTag(s string) bool {
	for _, t := range PresetTags {
		if t == s {
			return true
		}
	}
	return false
}

// mergeServerConfig changes the form's settings in an existing server's
// settings file.
func (pl *Plan) mergeServerConfig(hubURL string) ([]byte, error) {
	path := filepath.Join(pl.ServerDir, ServerConfigFile)
	m := readTOML(path)
	if m == nil {
		m = map[string]any{}
	}
	m["server_name"] = pl.ServerName
	m["port"] = pl.ServerPortNum()
	if pl.AdminEmail != "" {
		m["admin_email"] = strings.ToLower(pl.AdminEmail)
	} else {
		delete(m, "admin_email")
	}
	g, _ := m["grapevine"].(map[string]any)
	if g == nil {
		g = map[string]any{}
	}
	g["enabled"] = pl.ServerOnHub
	if pl.ServerOnHub {
		if str(g["hub_url"], "") == "" {
			g["hub_url"] = hubURL
		}
		g["public_host"] = pl.PublicHost
		g["public_port"] = pl.ServerPortNum()
		g["description"] = pl.Description
		tags := pl.AllTags()
		g["tags"] = tags
		g["category"] = "General"
		if len(tags) > 0 {
			g["category"] = tags[0]
		}
	}
	m["grapevine"] = g
	return toml.Marshal(m)
}

// mergeHubConfig changes the form's settings in an existing hub's
// settings file, adding or removing the official hub as a peer.
func (pl *Plan) mergeHubConfig(officialName, officialURL string) ([]byte, error) {
	m := readTOML(filepath.Join(pl.HubDir, HubConfigFile))
	if m == nil {
		m = map[string]any{}
	}
	m["hub_name"] = pl.HubName
	m["port"] = pl.HubPortNum()
	var peers []any
	if old, ok := m["peer_hubs"].([]any); ok {
		for _, p := range old {
			if pm, ok := p.(map[string]any); ok && str(pm["url"], "") == officialURL {
				continue
			}
			peers = append(peers, p)
		}
	}
	if pl.HubFederate {
		peers = append(peers, map[string]any{"name": officialName, "url": officialURL})
		if u := pl.HubPublicURL(); u != "" {
			m["public_url"] = u
		}
	}
	m["peer_hubs"] = peers
	return toml.Marshal(m)
}

func readTOML(path string) map[string]any {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	m := map[string]any{}
	if toml.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

func str(v any, fallback string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return fallback
}

func num(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	}
	return 0
}
