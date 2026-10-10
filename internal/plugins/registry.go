package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Registry is the set of plugins discovered under the Plugins directory.
// Like the theme system, the directory listing IS the registry — no separate
// index file. A Registry is immutable once built: the Manager publishes a new
// one (Registry.with/without, or a fresh Discover) when plugins are loaded,
// unloaded or rescanned live.
type Registry struct {
	manifests    map[string]*Manifest       // by plugin id
	channelKinds map[string]*ChannelKindDef // by "pluginID:kind"
	owners       map[string]string          // "pluginID:kind" -> pluginID (for lookups without a split)
}

// channelKindKey builds the registry key for a plugin's channel kind.
func channelKindKey(pluginID, kind string) string {
	return pluginID + ":" + kind
}

// Discover scans pluginsDir for <Name>/plugin.toml manifests, parses and
// validates each one, and returns a Registry. A malformed plugin is skipped
// with an error logged by the caller (via the returned per-plugin errors),
// not fatal to discovery of the rest.
func Discover(pluginsDir string) (*Registry, map[string]error) {
	reg := newRegistry()
	errs := make(map[string]error)

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return reg, errs // No Plugins directory yet — nothing installed, not an error
		}
		errs["<root>"] = fmt.Errorf("failed to read plugins directory: %w", err)
		return reg, errs
	}

	for _, entry := range entries {
		// Dot-folders are the installer's own working space (.staging,
		// .backup), never plugins.
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		folderName := entry.Name()
		if _, err := os.Stat(filepath.Join(pluginsDir, folderName, "plugin.toml")); err != nil {
			continue // Not a plugin folder (no plugin.toml)
		}
		m, err := LoadPluginFolder(filepath.Join(pluginsDir, folderName))
		if err != nil {
			errs[folderName] = err
			continue
		}
		reg.add(m)
	}

	return reg, errs
}

// LoadPluginFolder parses and validates dir/plugin.toml, and checks the
// manifest's [plugin].id matches the folder's name.
func LoadPluginFolder(dir string) (*Manifest, error) {
	m, err := LoadManifest(filepath.Join(dir, "plugin.toml"))
	if err != nil {
		return nil, err
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	if folder := filepath.Base(dir); m.Plugin.ID != folder {
		return nil, fmt.Errorf("plugin.toml [plugin].id %q does not match folder name %q", m.Plugin.ID, folder)
	}
	return m, nil
}

func newRegistry() *Registry {
	return &Registry{
		manifests:    make(map[string]*Manifest),
		channelKinds: make(map[string]*ChannelKindDef),
		owners:       make(map[string]string),
	}
}

func (r *Registry) add(m *Manifest) {
	r.manifests[m.Plugin.ID] = m
	for i := range m.ChannelKinds {
		key := channelKindKey(m.Plugin.ID, m.ChannelKinds[i].Kind)
		r.channelKinds[key] = &m.ChannelKinds[i]
		r.owners[key] = m.Plugin.ID
	}
}

// with returns a copy of r with m added (or replacing the same id). A
// Registry is never modified once published, so readers holding the old
// one are unaffected.
func (r *Registry) with(m *Manifest) *Registry {
	out := newRegistry()
	for id, existing := range r.manifests {
		if id != m.Plugin.ID {
			out.add(existing)
		}
	}
	out.add(m)
	return out
}

// without returns a copy of r with pluginID removed.
func (r *Registry) without(pluginID string) *Registry {
	out := newRegistry()
	for id, existing := range r.manifests {
		if id != pluginID {
			out.add(existing)
		}
	}
	return out
}

// Manifest returns the manifest for a plugin id, if discovered.
func (r *Registry) Manifest(pluginID string) (*Manifest, bool) {
	m, ok := r.manifests[pluginID]
	return m, ok
}

// All returns every discovered manifest, sorted by plugin id.
func (r *Registry) All() []*Manifest {
	ids := make([]string, 0, len(r.manifests))
	for id := range r.manifests {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*Manifest, len(ids))
	for i, id := range ids {
		out[i] = r.manifests[id]
	}
	return out
}

// Lookup resolves a plugin-provided channel kind.
func (r *Registry) Lookup(pluginID, kind string) (*ChannelKindDef, bool) {
	ck, ok := r.channelKinds[channelKindKey(pluginID, kind)]
	return ck, ok
}

// AllChannelKinds returns every channel kind every discovered plugin
// provides, in a stable order — for the channel-creation type selector.
func (r *Registry) AllChannelKinds() []*ChannelKindDef {
	var out []*ChannelKindDef
	for _, m := range r.All() {
		for i := range m.ChannelKinds {
			out = append(out, &m.ChannelKinds[i])
		}
	}
	return out
}

// OwnerOf returns which plugin id owns a given "pluginID:kind" registry key.
func (r *Registry) OwnerOf(pluginID, kind string) (string, bool) {
	owner, ok := r.owners[channelKindKey(pluginID, kind)]
	return owner, ok
}
