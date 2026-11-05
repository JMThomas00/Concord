package installer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

func TestServerTorSettings(t *testing.T) {
	pl := NewPlan(Platform{OS: Linux, Home: t.TempDir()})
	pl.ServerDir = t.TempDir()
	pl.ServerOnHub, pl.PublicHost = true, "chat.example.com"
	for _, c := range []struct {
		mode          string
		enabled, only bool
		grapevine     bool
	}{
		{TorOff, false, false, true},
		{TorAlso, true, false, false},
		{TorOnly, true, true, false},
	} {
		pl.ServerTor = c.mode
		data, err := pl.ServerConfigTOML("https://hub.example")
		if err != nil {
			t.Fatal(err)
		}
		var m struct {
			Tor       struct{ Enabled, Only bool }
			Grapevine struct{ Enabled bool }
		}
		if err := toml.Unmarshal(data, &m); err != nil {
			t.Fatal(err)
		}
		if m.Tor.Enabled != c.enabled || m.Tor.Only != c.only || m.Grapevine.Enabled != c.grapevine {
			t.Errorf("%q: %+v", c.mode, m)
		}

		// Configuring again reads it back and keeps other [tor] settings.
		os.WriteFile(filepath.Join(pl.ServerDir, ServerConfigFile), append(data, []byte("\n")...), 0o600)
		again := NewPlan(pl.Platform)
		again.ServerDir = pl.ServerDir
		again.LoadSettings()
		if again.ServerTor != c.mode {
			t.Errorf("read back %q as %q", c.mode, again.ServerTor)
		}
	}
	os.WriteFile(filepath.Join(pl.ServerDir, ServerConfigFile), []byte("[tor]\nenabled = true\nonly = true\ntor_path = \"/opt/tor\"\n"), 0o600)
	pl.ServerTor = TorAlso
	data, err := pl.mergeServerConfig("https://hub.example")
	if err != nil {
		t.Fatal(err)
	}
	if s := string(data); !strings.Contains(s, "/opt/tor") || strings.Contains(s, "only") {
		t.Errorf("merged:\n%s", s)
	}
}

func TestTorInstallCommand(t *testing.T) {
	for pm, want := range map[string]string{"apt": "apt-get install", "dnf": "dnf install", "pacman": "pacman -S", "zypper": "zypper"} {
		cmd, err := TorInstallCommand(Platform{OS: Linux, PkgManager: pm})
		if err != nil || !strings.HasPrefix(strings.Join(cmd, " "), want) || cmd[len(cmd)-1] != "tor" {
			t.Errorf("%s: %v %v", pm, cmd, err)
		}
	}
	if _, err := TorInstallCommand(Platform{OS: Windows}); err == nil || !strings.Contains(err.Error(), "Expert Bundle") {
		t.Errorf("windows: %v", err)
	}
}
