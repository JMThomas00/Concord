package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// allowInstances turns on [plugin].instances in a helper plugin's manifest.
func allowInstances(t *testing.T, folder string) {
	t.Helper()
	path := filepath.Join(folder, "plugin.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = []byte(strings.Replace(string(data), "[process]", "instances = true\n\n[process]", 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func serviceName(t *testing.T, m *Manager, id string) string {
	t.Helper()
	uid, err := m.ServiceUserIDFor(id)
	if err != nil {
		t.Fatal(err)
	}
	u, err := m.db.GetUserByID(uid)
	if err != nil {
		t.Fatal(err)
	}
	return u.Username
}

func TestInstancesAddRenameRemove(t *testing.T) {
	m, dir := newTestManager(t)
	allowInstances(t, writeHelperPlugin(t, dir, "mynah", "1.0.0", "sleep"))
	if err := m.Load("mynah"); err != nil {
		t.Fatal(err)
	}

	id, err := m.AddInstance("mynah", "Alice")
	if err != nil {
		t.Fatal(err)
	}
	if id != "mynah-alice" {
		t.Fatalf("instance id %q", id)
	}
	inst, ok := m.Registry().Manifest(id)
	if !ok || inst.BaseID != "mynah" || inst.Plugin.Name != "Alice" || inst.Plugin.Product != "mynah" || inst.Dir != filepath.Join(dir, "mynah") {
		t.Fatalf("instance manifest %+v", inst)
	}
	if m.currentSupervisor(id) == nil || m.currentSupervisor("mynah") == nil {
		t.Fatal("instance and base should both be running")
	}
	if got := serviceName(t, m, id); got != "Alice" {
		t.Fatalf("instance account %q", got)
	}
	if again, _ := m.AddInstance("mynah", "Alice"); again != "mynah-alice-2" {
		t.Fatalf("a second Alice got id %q", again)
	}

	if err := m.RenameInstance(id, "Alicia"); err != nil {
		t.Fatal(err)
	}
	if got := serviceName(t, m, id); got != "Alicia" {
		t.Fatalf("renamed account %q", got)
	}
	if inst, _ := m.Registry().Manifest(id); inst.Plugin.Name != "Alicia" {
		t.Fatalf("renamed manifest %q", inst.Plugin.Name)
	}
	// The base plugin can be renamed too (it's the first persona).
	if err := m.RenameInstance("mynah", "Burt"); err != nil {
		t.Fatal(err)
	}
	if got := serviceName(t, m, "mynah"); got != "Burt" {
		t.Fatalf("renamed base account %q", got)
	}

	// Instances survive a restart of Concord.
	m2 := NewManager(m.db, "ws://127.0.0.1:1/ws", quietLogger())
	m.Shutdown()
	if err := m2.LoadAll(dir); err != nil {
		t.Fatal(err)
	}
	defer m2.Shutdown()
	if inst, ok := m2.Registry().Manifest(id); !ok || inst.Plugin.Name != "Alicia" {
		t.Fatalf("after a restart: %+v, %v", inst, ok)
	}
	if base, _ := m2.Registry().Manifest("mynah"); base.Plugin.Name != "Burt" || base.IsInstance() {
		t.Fatalf("renamed base after a restart: %+v", base.Plugin)
	}

	if err := m2.RemoveInstance("mynah"); err == nil {
		t.Fatal("removed the base plugin as if it were an instance")
	}
	if err := m2.RemoveInstance(id); err != nil {
		t.Fatal(err)
	}
	if _, ok := m2.Registry().Manifest(id); ok {
		t.Fatal("removed instance still registered")
	}
	if _, ok, _ := m2.db.GetPluginInstance(id); ok {
		t.Fatal("removed instance still stored")
	}
}

func TestInstancesNeedThePluginToAllowThem(t *testing.T) {
	m, dir := newTestManager(t)
	writeHelperPlugin(t, dir, "tukan", "1.0.0", "sleep")
	if err := m.Load("tukan"); err != nil {
		t.Fatal(err)
	}
	if _, err := m.AddInstance("tukan", "Second"); err == nil {
		t.Fatal("added an instance to a plugin that doesn't allow them")
	}
	if err := m.RenameInstance("tukan", "Other"); err == nil {
		t.Fatal("renamed a plugin that doesn't allow instances")
	}
}

// An old separately installed persona becomes an instance, keeping its ID
// and account (so its settings and channels).
func TestAdoptTurnsASeparateInstallIntoAnInstance(t *testing.T) {
	m, dir := newTestManager(t)
	allowInstances(t, writeHelperPlugin(t, dir, "mynah", "2.0.0", "sleep"))
	writeHelperPlugin(t, dir, "Burt", "0.1.0", "sleep")
	if err := m.Load("mynah"); err != nil {
		t.Fatal(err)
	}
	if err := m.Load("Burt"); err != nil {
		t.Fatal(err)
	}
	before, _ := m.ServiceUserIDFor("Burt")

	if err := m.AdoptInstance("Burt", "mynah"); err != nil {
		t.Fatal(err)
	}
	inst, ok := m.Registry().Manifest("Burt")
	if !ok || inst.BaseID != "mynah" || inst.Plugin.Version != "2.0.0" || inst.Plugin.Name != "Burt" {
		t.Fatalf("adopted manifest %+v", inst)
	}
	if after, _ := m.ServiceUserIDFor("Burt"); after != before {
		t.Fatal("adopting changed the persona's account")
	}
	if _, err := os.Stat(filepath.Join(dir, "Burt")); !os.IsNotExist(err) {
		t.Fatal("the old install's folder is still in Plugins")
	}
	if _, err := os.Stat(filepath.Join(dir, ".backup", "Burt.adopted")); err != nil {
		t.Fatal("the old install wasn't kept in .backup")
	}

	// Uninstalling the base takes its instances down; reinstalling brings
	// them back.
	if err := m.Uninstall("Burt"); err == nil {
		t.Fatal("uninstalled an instance as if it were a plugin")
	}
	allowInstances(t, writeHelperPlugin(t, filepath.Join(dir, ".keep"), "mynah", "2.0.0", "sleep"))
	if err := m.Uninstall("mynah"); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Registry().Manifest("Burt"); ok {
		t.Fatal("instance outlived its uninstalled base")
	}
	if err := os.Rename(filepath.Join(dir, ".keep", "mynah"), filepath.Join(dir, "mynah")); err != nil {
		t.Fatal(err)
	}
	if err := m.Load("mynah"); err != nil {
		t.Fatal(err)
	}
	if inst, ok := m.Registry().Manifest("Burt"); !ok || inst.BaseID != "mynah" {
		t.Fatal("reinstalling the base didn't bring its instance back")
	}
}
