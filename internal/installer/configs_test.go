package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/hub"
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

func testPlan() *Plan {
	pl := NewPlan(Platform{OS: Linux, Arch: "amd64", Home: "/home/you", Systemd: true})
	pl.Components = []string{Client, Server, Hub}
	pl.ServerName, pl.ServerPort, pl.AdminEmail = "Grape Hall", "9090", "Me@Example.com"
	pl.ServerOnHub, pl.PublicHost, pl.Description = true, "chat.example.com", "Friends and grapes"
	pl.Tags, pl.OtherTags = []string{"Gaming"}, " AI, AI Research, gaming"
	pl.TermsAccepted = true
	pl.HubName, pl.HubPort, pl.HubFederate = "My Hub", "7788", true
	return pl
}

// The server reads the installer's settings file as the installer meant.
func TestServerConfigReadsBack(t *testing.T) {
	b, err := testPlan().ServerConfigTOML("https://hub.example")
	if err != nil {
		t.Fatal(err)
	}
	c := server.DefaultConfig()
	if err := toml.Unmarshal(b, c); err != nil {
		t.Fatal(err)
	}
	g := c.Grapevine
	if c.Port != 9090 || c.ServerName != "Grape Hall" || !c.TermsAccepted || c.AdminEmail != "me@example.com" ||
		c.DatabasePath != "concord.db" || c.PluginsDir != "Plugins" || !c.MessagePruning.Enabled ||
		!g.Enabled || g.HubURL != "https://hub.example" || g.PublicHost != "chat.example.com" || g.PublicPort != 9090 || g.Category != "Gaming" || strings.Join(g.Tags, "/") != "Gaming/AI/AI Research" {
		t.Fatalf("server read %+v", c)
	}
}

func TestHubConfigReadsBack(t *testing.T) {
	b, token, err := testPlan().HubConfigTOML("Official", "https://official.example")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), HubConfigFile)
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := hub.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != 7788 || c.HubName != "My Hub" || c.AdminToken != token || len(token) < 32 ||
		len(c.PeerHubs) != 1 || c.PeerHubs[0].URL != "https://official.example" || c.HeartbeatTimeout != 90 {
		t.Fatalf("hub read %+v", c)
	}
}

// Adding servers keeps what the client already had, and doesn't add one
// twice.
func TestAddClientServers(t *testing.T) {
	dir := t.TempDir()
	old := `{"version":1,"servers":[{"id":"x","name":"Old","address":"old.example","port":8080,"saved_credentials":{"email":"a@b.c","token":"t"}}],"default_preferences":{"auto_connect_on_startup":true}}`
	if err := os.WriteFile(filepath.Join(dir, "servers.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := AddClientServers(dir, []ClientServer{{"Concord", "concord.example", 443, true}, {"Old again", "OLD.example", 8080, false}})
	if err != nil || n != 1 {
		t.Fatalf("added %d, %v", n, err)
	}
	if n, _ := AddClientServers(dir, []ClientServer{{"Concord", "concord.example", 443, true}}); n != 0 {
		t.Fatal("added the same server twice")
	}
	var doc struct {
		Servers []struct {
			Name, Address, IconLetter string
			Port                      int
			UseTLS                    bool           `json:"use_tls"`
			IconLetterJSON            string         `json:"icon_letter"`
			Saved                     map[string]any `json:"saved_credentials"`
		}
		Prefs map[string]any `json:"default_preferences"`
	}
	b, _ := os.ReadFile(filepath.Join(dir, "servers.json"))
	if err := json.Unmarshal(b, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Servers) != 2 || doc.Servers[0].Saved["token"] != "t" || doc.Prefs["auto_connect_on_startup"] != true {
		t.Fatalf("lost what was there: %s", b)
	}
	if s := doc.Servers[1]; s.Address != "concord.example" || s.Port != 443 || !s.UseTLS || s.IconLetterJSON != "Co" {
		t.Fatalf("new entry %+v", s)
	}
}

// A connected hub gets its own address, so it can announce itself to the
// official hub; a typed host gets a scheme and the hub's port.
func TestHubPublicURL(t *testing.T) {
	cases := []struct{ typed, port, want string }{
		{"hub.example.com", "7777", "http://hub.example.com:7777"},
		{"hub.example.com", "443", "https://hub.example.com"},
		{"https://hub.example.com/", "7777", "https://hub.example.com"},
		{"203.0.113.5:9000", "7777", "http://203.0.113.5:9000"},
		{"", "7777", ""},
	}
	for _, c := range cases {
		pl := &Plan{HubAddress: c.typed, HubPort: c.port}
		if got := pl.HubPublicURL(); got != c.want {
			t.Errorf("%q on %s: %q, want %q", c.typed, c.port, got, c.want)
		}
	}
	pl := &Plan{HubName: "Grape Hub", HubPort: "7777", HubFederate: true, HubAddress: "hub.example.com"}
	b, _, err := pl.HubConfigTOML("Official", "https://grapevine.example")
	if err != nil || !strings.Contains(string(b), `public_url = 'http://hub.example.com:7777'`) {
		t.Fatalf("hub config has no public_url (%v):\n%s", err, b)
	}
}
