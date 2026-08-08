package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Registry is the set of plugins discovered under the Plugins directory at
// startup. Like the theme system, the directory listing IS the registry —
// no separate index file, no hot-reload; a new plugin folder is picked up on
// the next server restart.
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
	reg := &Registry{
		manifests:    make(map[string]*Manifest),
		channelKinds: make(map[string]*ChannelKindDef),
		owners:       make(map[string]string),
	}
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
		if !entry.IsDir() {
			continue
		}
		folderName := entry.Name()
		manifestPath := filepath.Join(pluginsDir, folderName, "plugin.toml")
		if _, err := os.Stat(manifestPath); err != nil {
			continue // Not a plugin folder (no plugin.toml)
		}

		m, err := LoadManifest(manifestPath)
		if err != nil {
			errs[folderName] = err
			continue
		}
		if err := m.Validate(); err != nil {
			errs[folderName] = err
			continue
		}
		if m.Plugin.ID != folderName {
			errs[folderName] = fmt.Errorf("plugin.toml [plugin].id %q does not match folder name %q", m.Plugin.ID, folderName)
			continue
		}

		reg.manifests[m.Plugin.ID] = m
		for i := range m.ChannelKinds {
			key := channelKindKey(m.Plugin.ID, m.ChannelKinds[i].Kind)
			reg.channelKinds[key] = &m.ChannelKinds[i]
			reg.owners[key] = m.Plugin.ID
		}
	}

	return reg, errs
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
