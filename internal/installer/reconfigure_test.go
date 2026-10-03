package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/concord-chat/concord/internal/hub"
	"github.com/concord-chat/concord/internal/official"
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

// Configuring again changes only what the form asks about: a server's
// Grapevine registration and mail settings survive.
func TestReconfigureKeepsTheRest(t *testing.T) {
	pl := testPlan()
	pl.ServerDir, pl.HubDir = t.TempDir(), t.TempDir()
	b, _ := pl.ServerConfigTOML("https://hub.example")
	extra := "\n[mail]\nsmtp_host = \"smtp.example\"\n"
	b = []byte(strings.Replace(string(b), "[grapevine]\n", "[grapevine]\nserver_id = 'abc'\nregistration_secret = 's3cret'\n", 1) + extra)
	if err := os.WriteFile(filepath.Join(pl.ServerDir, ServerConfigFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	hb, token, _ := pl.HubConfigTOML(official.HubName, official.HubURL)
	if err := os.WriteFile(filepath.Join(pl.HubDir, HubConfigFile), hb, 0o600); err != nil {
		t.Fatal(err)
	}

	again := NewPlan(pl.Platform)
	again.ServerDir, again.HubDir, again.Reconfigure = pl.ServerDir, pl.HubDir, true
	again.LoadSettings()
	if again.ServerName != "Grape Hall" || again.ServerPort != "9090" || !again.ServerOnHub || again.PublicHost != "chat.example.com" ||
		strings.Join(again.Tags, ",") != "Gaming" || again.OtherTags != "AI, AI Research" || !again.HubFederate || again.HubName != "My Hub" {
		t.Fatalf("didn't read the settings back: %+v", again)
	}
	if !again.PortUnchanged(Server, "9090") || again.PortUnchanged(Server, "9091") {
		t.Fatal("the server's own port should count as free, and only that one")
	}

	again.ServerName, again.ServerPort, again.OtherTags, again.HubFederate = "Grape Hall II", "9191", "Wine", false
	out, err := again.mergeServerConfig("https://other.example")
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

	out, err = again.mergeHubConfig(official.HubName, official.HubURL)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), HubConfigFile)
	os.WriteFile(path, out, 0o600)
	h, err := hub.LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(h.PeerHubs) != 0 || h.AdminToken != token || h.HubName != "My Hub" {
		t.Fatalf("merged hub settings wrong: %+v", h)
	}
}

func TestRemoveRCBlock(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".bashrc")
	before := "alias ll='ls -l'\n"
	os.WriteFile(path, []byte(before+"\n# Added by concord-install\nexport PATH=\"$HOME/.local/bin:$PATH\"\n"), 0o644)
	if err := removeRCBlock(path); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != before {
		t.Fatalf("left %q", b)
	}
}
