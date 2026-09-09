// Package plugins implements Concord's plugin platform: discovering plugin
// folders dropped into the server's Plugins directory, spawning/supervising
// each plugin's own OS process, and issuing the credentials that let a
// plugin process connect back to Concord as a privileged client.
//
// Plugins are never compiled into or dynamically loaded by Concord itself —
// see plugin.toml's [process] section. This keeps installing a plugin to
// "drop a folder in, restart" with zero code changes on Concord's side.
package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/pelletier/go-toml/v2"
)

// EntrypointDef describes how to launch a plugin's process on one OS.
type EntrypointDef struct {
	Bin  string   `toml:"bin"`
	Args []string `toml:"args"`
}

// ProcessDef describes how Concord spawns and supervises a plugin's process.
type ProcessDef struct {
	WorkingDir            string            `toml:"working_dir"`
	RestartOnCrash        bool              `toml:"restart_on_crash"`
	MaxRestarts           int               `toml:"max_restarts"`
	RestartBackoffSeconds int               `toml:"restart_backoff_seconds"`
	StartupTimeoutSeconds int               `toml:"startup_timeout_seconds"`
	Env                   map[string]string `toml:"env"`

	Entrypoint struct {
		Windows *EntrypointDef `toml:"windows"`
		Linux   *EntrypointDef `toml:"linux"`
		Darwin  *EntrypointDef `toml:"darwin"`
	} `toml:"entrypoint"`
}

// ConfigField describes one manifest-declared, generically-rendered form
// field — used both for per-channel creation fields and server-wide config.
type ConfigField struct {
	Key      string   `toml:"key"`
	Label    string   `toml:"label"`
	Type     string   `toml:"type"` // text | number | boolean | select | channel_select
	Options  []string `toml:"options"`
	Default  string   `toml:"default"`
	Required bool     `toml:"required"`
}

// ChannelKindDef describes one channel kind a plugin provides.
type ChannelKindDef struct {
	Kind         string        `toml:"kind"`
	DisplayName  string        `toml:"display_name"`
	Icon         string        `toml:"icon"`
	RemotePane   bool          `toml:"remote_pane"`
	CreateFields []ConfigField `toml:"create_field"`
}

// PluginDef is the [plugin] identity block.
type PluginDef struct {
	ID                string `toml:"id"`
	Name              string `toml:"name"`
	// Product names the underlying plugin family this install belongs to,
	// when Name is a per-install persona rather than the whole identity —
	// e.g. a Mynah persona install sets id/name to "Burt" but product to
	// "Mynah", so Settings > Plugins can show "Mynah (Burt)" instead of an
	// unqualified "Burt" indistinguishable from an unrelated plugin. Optional;
	// most plugins (e.g. Tukan) leave it unset and are shown by Name alone.
	Product           string `toml:"product"`
	Version           string `toml:"version"`
	Author            string `toml:"author"`
	Description       string `toml:"description"`
	MinConcordVersion string `toml:"min_concord_version"`
	// SourceURL, if declared, is a release-archive URL an admin can install
	// or update this plugin from via Settings > Plugins -- see
	// InstallFromURL (install.go). Purely informational to Concord itself
	// (no auto-update polling); a plugin without one can still be
	// installed by an admin supplying a URL directly at install time.
	SourceURL string `toml:"source_url"`
}

// Manifest is the parsed, validated contents of a plugin.toml file.
type Manifest struct {
	Plugin           PluginDef        `toml:"plugin"`
	Process          ProcessDef       `toml:"process"`
	ChannelKinds     []ChannelKindDef `toml:"channel_kind"`
	ServerConfigFields []ConfigField  `toml:"server_config_field"`

	// Dir is the plugin's own folder (set by LoadManifest, not from TOML).
	Dir string `toml:"-"`
}

// LoadManifest reads and parses a plugin.toml file. It does not validate —
// call Validate() separately once the caller knows the target OS/context.
func LoadManifest(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read manifest: %w", err)
	}

	m := &Manifest{}
	if err := toml.Unmarshal(data, m); err != nil {
		return nil, fmt.Errorf("failed to parse manifest: %w", err)
	}
	m.Dir = filepath.Dir(path)
	return m, nil
}

// Entrypoint returns the entrypoint definition for the current OS.
func (m *Manifest) Entrypoint() (*EntrypointDef, error) {
	var ep *EntrypointDef
	switch runtime.GOOS {
	case "windows":
		ep = m.Process.Entrypoint.Windows
	case "linux":
		ep = m.Process.Entrypoint.Linux
	case "darwin":
		ep = m.Process.Entrypoint.Darwin
	}
	if ep == nil || ep.Bin == "" {
		return nil, fmt.Errorf("plugin %q declares no entrypoint for GOOS=%s", m.Plugin.ID, runtime.GOOS)
	}
	return ep, nil
}

// validFieldTypes are the field types the generic client-side form renderer
// understands. Adding a new type later is one case there, not a protocol change.
var validFieldTypes = map[string]bool{
	"text": true, "number": true, "boolean": true, "select": true, "channel_select": true,
}

// Validate checks a manifest is well-formed and usable on this host. It does
// NOT check filesystem existence of the entrypoint binary — the folder name
// itself (matching [plugin].id) is checked by the registry, since that's
// where the folder path is known.
func (m *Manifest) Validate() error {
	if m.Plugin.ID == "" {
		return fmt.Errorf("missing [plugin].id")
	}
	if m.Plugin.Name == "" {
		return fmt.Errorf("plugin %q: missing [plugin].name", m.Plugin.ID)
	}
	if m.Plugin.Version == "" {
		return fmt.Errorf("plugin %q: missing [plugin].version", m.Plugin.ID)
	}
	if _, err := m.Entrypoint(); err != nil {
		return err
	}

	seenKinds := make(map[string]bool)
	for _, ck := range m.ChannelKinds {
		if ck.Kind == "" {
			return fmt.Errorf("plugin %q: a channel_kind is missing 'kind'", m.Plugin.ID)
		}
		if seenKinds[ck.Kind] {
			return fmt.Errorf("plugin %q: duplicate channel_kind %q", m.Plugin.ID, ck.Kind)
		}
		seenKinds[ck.Kind] = true
		if err := validateFields(m.Plugin.ID, ck.CreateFields); err != nil {
			return err
		}
	}
	if err := validateFields(m.Plugin.ID, m.ServerConfigFields); err != nil {
		return err
	}

	return nil
}

func validateFields(pluginID string, fields []ConfigField) error {
	seen := make(map[string]bool)
	for _, f := range fields {
		if f.Key == "" {
			return fmt.Errorf("plugin %q: a config field is missing 'key'", pluginID)
		}
		if seen[f.Key] {
			return fmt.Errorf("plugin %q: duplicate config field key %q", pluginID, f.Key)
		}
		seen[f.Key] = true
		if !validFieldTypes[f.Type] {
			return fmt.Errorf("plugin %q: config field %q has unknown type %q", pluginID, f.Key, f.Type)
		}
		if f.Type == "select" && len(f.Options) == 0 {
			return fmt.Errorf("plugin %q: select field %q declares no options", pluginID, f.Key)
		}
	}
	return nil
}

// ChannelKind looks up one of this manifest's declared channel kinds by name.
func (m *Manifest) ChannelKind(kind string) (*ChannelKindDef, bool) {
	for i := range m.ChannelKinds {
		if m.ChannelKinds[i].Kind == kind {
			return &m.ChannelKinds[i], true
		}
	}
	return nil, false
}
