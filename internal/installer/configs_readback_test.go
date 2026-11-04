package installer_test

// These read the installer's settings files with the real server and hub,
// from outside the package: the server imports the installer (it updates
// itself with it), so package installer's own tests can't import the server.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/hub"
	"github.com/concord-chat/concord/internal/installer"
	"github.com/concord-chat/concord/internal/official"
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

// The server reads the installer's settings file as the installer meant.
func TestServerConfigReadsBack(t *testing.T) {
	b, err := installer.TestPlan().ServerConfigTOML("https://hub.example")
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
	b, token, err := installer.TestPlan().HubConfigTOML("Official", "https://official.example")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), installer.HubConfigFile)
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

// Configuring again changes only what the form asks about: a server's
// Grapevine registration and mail settings survive.
func TestReconfigureKeepsTheRest(t *testing.T) {
	pl := installer.TestPlan()
	pl.ServerDir, pl.HubDir = t.TempDir(), t.TempDir()
	b, _ := pl.ServerConfigTOML("https://hub.example")
	extra := "\n[mail]\nsmtp_host = \"smtp.example\"\n"
	b = []byte(strings.Replace(string(b), "[grapevine]\n", "[grapevine]\nserver_id = 'abc'\nregistration_secret = 's3cret'\n", 1) + extra)
	if err := os.WriteFile(filepath.Join(pl.ServerDir, installer.ServerConfigFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	hb, token, _ := pl.HubConfigTOML(official.HubName, official.HubURL)
	if err := os.WriteFile(filepath.Join(pl.HubDir, installer.HubConfigFile), hb, 0o600); err != nil {
		t.Fatal(err)
	}

	again := installer.NewPlan(pl.Platform)
	again.ServerDir, again.HubDir, again.Reconfigure = pl.ServerDir, pl.HubDir, true
	again.LoadSettings()
	if again.ServerName != "Grape Hall" || again.ServerPort != "9090" || !again.ServerOnHub || again.PublicHost != "chat.example.com" ||
		strings.Join(again.Tags, ",") != "Gaming" || again.OtherTags != "AI, AI Research" || !again.HubFederate || again.HubName != "My Hub" {
		t.Fatalf("didn't read the settings back: %+v", again)
	}
	if !again.PortUnchanged(installer.Server, "9090") || again.PortUnchanged(installer.Server, "9091") {
		t.Fatal("the server's own port should count as free, and only that one")
	}

	again.ServerName, again.ServerPort, again.OtherTags, again.HubFederate = "Grape Hall II", "9191", "Wine", false
	out, err := again.MergeServerConfig("https://other.example")
	if err != nil {
		t.Fatal(err)
	}
	c := server.DefaultConfig()
	if err := toml.Unmarshal(out, c); err != nil {
		t.Fatal(err)
	}
	g := c.Grapevine
	if c.ServerName != "Grape Hall II" || c.Port != 9191 || g.PublicPort != 9191 || g.ServerID != "abc" || g.RegistrationSecret != "s3cret" ||
		g.HubURL != "https://hub.example" || c.Mail.SMTPHost != "smtp.example" || strings.Join(g.Tags, ",") != "Gaming,Wine" || !c.TermsAccepted {
		t.Fatalf("merged server settings wrong: %+v", c)
	}

	out, err = again.MergeHubConfig(official.HubName, official.HubURL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), installer.HubConfigFile)
	os.WriteFile(path, out, 0o600)
	h, err := hub.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.PeerHubs) != 0 || h.AdminToken != token || h.HubName != "My Hub" {
		t.Fatalf("merged hub settings wrong: %+v", h)
	}
}
