package plugins

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/concord-chat/concord/internal/database"
)

// Instances: a plugin whose manifest sets [plugin].instances = true can run
// as several named copies (one Mynah persona each). Every instance is its
// own plugin as far as the rest of Concord is concerned (its own ID, so its
// own service account, token, process, settings, channels and data folder)
// but runs the base plugin's files from the base's folder, so installing
// and updating happen once for all of them.
//
// The base plugin is itself the first instance. Extra ones are rows in
// plugin_instances; a row whose ID is the base's own only renames it.

// IsInstance reports whether m is an extra instance of another plugin.
func (m *Manifest) IsInstance() bool { return m.BaseID != "" }

// Base is the installed plugin m runs the files of: its own ID, or its
// base's for an instance.
func (m *Manifest) Base() string {
	if m.BaseID != "" {
		return m.BaseID
	}
	return m.Plugin.ID
}

// instanceManifest is base's manifest as run by instance inst.
func instanceManifest(base *Manifest, inst database.PluginInstance) *Manifest {
	cp := *base
	cp.Plugin.ID = inst.ID
	cp.Plugin.Name = inst.Name
	if cp.Plugin.Product == "" {
		cp.Plugin.Product = base.Plugin.Name
	}
	if inst.ID != base.Plugin.ID {
		cp.BaseID = base.Plugin.ID
	}
	// Distinct slices, so registry entries never share elements.
	cp.ChannelKinds = append([]ChannelKindDef(nil), base.ChannelKinds...)
	cp.ServerConfigFields = append([]ConfigField(nil), base.ServerConfigFields...)
	return &cp
}

// withInstances returns reg plus every instance whose base is in reg and
// allows instances. Instances of a missing base (uninstalled) are skipped,
// and come back if it's reinstalled.
func withInstances(reg *Registry, insts []database.PluginInstance) *Registry {
	out := reg
	for _, inst := range insts {
		base, ok := reg.Manifest(inst.BaseID)
		if !ok || base.IsInstance() || !base.Plugin.Instances {
			continue
		}
		out = out.with(instanceManifest(base, inst))
	}
	return out
}

// instancesOf lists the extra instances of baseID in reg.
func instancesOf(reg *Registry, baseID string) []*Manifest {
	var out []*Manifest
	for _, m := range reg.All() {
		if m.BaseID == baseID {
			out = append(out, m)
		}
	}
	return out
}

// listInstances reads the instance rows; a failure is logged and treated as
// none, so a bad table never stops plugins from loading.
func (m *Manager) listInstances() []database.PluginInstance {
	insts, err := m.db.ListPluginInstances()
	if err != nil {
		m.log.Error("failed to read plugin instances", "error", err)
	}
	return insts
}

// loadFolderWithInstances reads Plugins/<baseID> and returns its manifest
// (renamed if its instance row says so) and its extra instances'.
func (m *Manager) loadFolderWithInstances(baseID string) ([]*Manifest, error) {
	manifest, err := LoadPluginFolder(filepath.Join(m.PluginsDir(), baseID))
	if err != nil {
		return nil, err
	}
	reg := withInstances(newRegistry().with(manifest), m.listInstances())
	out := []*Manifest{}
	if own, ok := reg.Manifest(baseID); ok {
		out = append(out, own)
	}
	return append(out, instancesOf(reg, baseID)...), nil
}

var slugNonWord = regexp.MustCompile(`[^a-z0-9]+`)

// newInstanceID makes an ID for an instance named name of baseID that no
// installed plugin or instance already uses, e.g. "mynah-alice".
func (m *Manager) newInstanceID(baseID, name string) (string, error) {
	slug := strings.Trim(slugNonWord.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if slug == "" {
		slug = "instance"
	}
	id := baseID + "-" + slug
	for n := 2; ; n++ {
		if !validPluginIDPattern.MatchString(id) {
			return "", fmt.Errorf("can't make an id from %q", name)
		}
		_, inRegistry := m.Registry().Manifest(id)
		installed, err := m.db.GetInstalledPlugin(id)
		if err != nil {
			return "", err
		}
		if !inRegistry && installed == nil {
			if _, err := os.Stat(filepath.Join(m.PluginsDir(), id)); os.IsNotExist(err) {
				return id, nil
			}
		}
		id = fmt.Sprintf("%s-%s-%d", baseID, slug, n)
	}
}

// cleanInstanceName validates an instance's display name.
func cleanInstanceName(name string) (string, error) {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return "", fmt.Errorf("give the instance a name")
	case len(name) > 32:
		return "", fmt.Errorf("instance names are at most 32 characters")
	case strings.ContainsAny(name, "@#:\n\t"):
		return "", fmt.Errorf("instance names can't contain @, #, : or line breaks")
	}
	return name, nil
}

// AddInstance starts a new named instance of baseID and returns its ID.
func (m *Manager) AddInstance(baseID, name string) (string, error) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	name, err := cleanInstanceName(name)
	if err != nil {
		return "", err
	}
	base, ok := m.Registry().Manifest(baseID)
	if !ok || base.IsInstance() {
		return "", fmt.Errorf("plugin %q is not installed", baseID)
	}
	if !base.Plugin.Instances {
		return "", fmt.Errorf("%s doesn't support more than one instance", base.Plugin.Name)
	}
	id, err := m.newInstanceID(baseID, name)
	if err != nil {
		return "", err
	}
	if err := m.db.SavePluginInstance(id, baseID, name); err != nil {
		return "", err
	}
	manifest := instanceManifest(m.baseManifest(base), database.PluginInstance{ID: id, BaseID: baseID, Name: name})
	m.setRegistry(m.Registry().with(manifest))
	defer m.registryChanged()
	return id, m.startIfEnabled(manifest)
}

// baseManifest is base as read from its folder, before any rename: what
// its instances are copies of.
func (m *Manager) baseManifest(base *Manifest) *Manifest {
	if fresh, err := LoadPluginFolder(filepath.Join(m.PluginsDir(), base.Plugin.ID)); err == nil {
		return fresh
	}
	return base
}

// RenameInstance renames an instance (or a base plugin that allows
// instances), including its service account, and restarts it.
func (m *Manager) RenameInstance(id, name string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	name, err := cleanInstanceName(name)
	if err != nil {
		return err
	}
	manifest, ok := m.Registry().Manifest(id)
	if !ok {
		return fmt.Errorf("plugin %q is not installed", id)
	}
	if !manifest.IsInstance() && !manifest.Plugin.Instances {
		return fmt.Errorf("%s can't be renamed", manifest.Plugin.Name)
	}
	if err := m.db.SavePluginInstance(id, manifest.Base(), name); err != nil {
		return err
	}
	if installed, err := m.db.GetInstalledPlugin(id); err == nil && installed != nil {
		if err := m.db.RenameServiceAccount(installed.ServiceUserID, name); err != nil {
			return err
		}
	}
	renamed := *manifest
	renamed.Plugin.Name = name
	m.stop(id)
	m.setRegistry(m.Registry().with(&renamed))
	defer m.registryChanged()
	return m.startIfEnabled(&renamed)
}

// RemoveInstance stops and forgets an extra instance. Like uninstalling a
// plugin, its account, settings, channels and data folder are kept.
func (m *Manager) RemoveInstance(id string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	manifest, ok := m.Registry().Manifest(id)
	if !ok {
		return fmt.Errorf("plugin %q is not installed", id)
	}
	if !manifest.IsInstance() {
		return fmt.Errorf("%s is the installed plugin itself; uninstall it instead", manifest.Plugin.Name)
	}
	m.stop(id)
	m.setRegistry(m.Registry().without(id))
	defer m.registryChanged()
	return m.db.DeletePluginInstance(id)
}

// AdoptInstance turns legacyID, a separately installed copy of a plugin
// (an old Mynah persona folder), into an instance of baseID. It keeps its
// ID, so its account, settings, channels and data stay; its own folder is
// moved to Plugins/.backup/<id>.adopted.
func (m *Manager) AdoptInstance(legacyID, baseID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	legacy, ok := m.Registry().Manifest(legacyID)
	if !ok || legacy.IsInstance() {
		return fmt.Errorf("plugin %q is not a separate install", legacyID)
	}
	base, ok := m.Registry().Manifest(baseID)
	if !ok || base.IsInstance() || !base.Plugin.Instances || baseID == legacyID {
		return fmt.Errorf("plugin %q can't take instances", baseID)
	}
	if !validPluginIDPattern.MatchString(legacyID) {
		return fmt.Errorf("invalid plugin id %q", legacyID)
	}
	m.stop(legacyID)
	backup := filepath.Join(m.PluginsDir(), ".backup", legacyID+".adopted")
	_ = os.MkdirAll(filepath.Dir(backup), 0o755)
	_ = os.RemoveAll(backup)
	if err := os.Rename(filepath.Join(m.PluginsDir(), legacyID), backup); err != nil {
		_ = m.startIfEnabled(legacy)
		return fmt.Errorf("failed to set the old install aside: %w", err)
	}
	if err := m.db.SavePluginInstance(legacyID, baseID, legacy.Plugin.Name); err != nil {
		return err
	}
	manifest := instanceManifest(m.baseManifest(base), database.PluginInstance{ID: legacyID, BaseID: baseID, Name: legacy.Plugin.Name})
	m.setRegistry(m.Registry().with(manifest))
	defer m.registryChanged()
	return m.startIfEnabled(manifest)
}
