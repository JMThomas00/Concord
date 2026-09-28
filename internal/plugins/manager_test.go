package plugins

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/database"
)

// writeHelperPlugin writes dir/<id>/plugin.toml for a plugin whose process
// is this test binary running TestHelperProcess in mode.
func writeHelperPlugin(t *testing.T, dir, id, version, mode string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, id)
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	ep := "bin = '" + exe + "'\nargs = ['-test.run=^TestHelperProcess$']\n"
	manifest := "[plugin]\nid = \"" + id + "\"\nname = \"" + id + "\"\nversion = \"" + version + "\"\n\n" +
		"[process]\nrestart_on_crash = true\nmax_restarts = 1\nrestart_backoff_seconds = 1\nstartup_timeout_seconds = 5\n\n" +
		"[process.env]\nPLUGIN_HELPER_MODE = \"" + mode + "\"\n\n" +
		"[process.entrypoint.windows]\n" + ep + "[process.entrypoint.linux]\n" + ep + "[process.entrypoint.darwin]\n" + ep
	if err := os.WriteFile(filepath.Join(folder, "plugin.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

func newTestManager(t *testing.T) (*Manager, string) {
	t.Helper()
	root := t.TempDir()
	db, err := database.New(filepath.Join(root, "concord.db"))
	if err != nil {
		t.Fatal(err)
	}
	pluginsDir := filepath.Join(root, "Plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewManager(db, "ws://127.0.0.1:1/ws", quietLogger())
	if err := m.LoadAll(pluginsDir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		m.Shutdown()
		db.Close()
	})
	return m, pluginsDir
}

func (m *Manager) currentSupervisor(id string) *Supervisor {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.supervisors[id]
}

func TestManagerLoadsLiveAndToggleNeverOrphansAProcess(t *testing.T) {
	m, dir := newTestManager(t)
	var registryChanges, stops int
	m.SetHooks(func(string) { stops++ }, func() { registryChanges++ })

	writeHelperPlugin(t, dir, "p", "1.0.0", "sleep")
	if err := m.Load("p"); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if _, ok := m.Registry().Manifest("p"); !ok || registryChanges != 1 {
		t.Fatalf("after Load: in registry=%v, registry changes=%d", ok, registryChanges)
	}

	for i := 0; i < 10; i++ {
		prev := m.currentSupervisor("p")
		if err := m.SetEnabled("p", i%2 == 1); err != nil {
			t.Fatalf("SetEnabled #%d: %v", i, err)
		}
		if prev != nil {
			select {
			case <-prev.Done():
			default:
				t.Fatalf("toggle #%d left the previous run's supervisor alive", i)
			}
		}
	}
	// Ended enabled: exactly one live run.
	if m.currentSupervisor("p") == nil {
		t.Fatal("plugin should be running after the last enable")
	}

	prev := m.currentSupervisor("p")
	if err := m.Restart("p"); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	<-prev.Done()
	if m.currentSupervisor("p") == prev {
		t.Fatal("Restart reused the old supervisor")
	}

	m.Unload("p")
	if _, ok := m.Registry().Manifest("p"); ok || m.currentSupervisor("p") != nil {
		t.Fatal("Unload left the plugin registered or running")
	}
	if stops == 0 {
		t.Fatal("onStopped hook never ran")
	}
}

func stageHelperPlugin(t *testing.T, pluginsDir, id, version, mode string) string {
	t.Helper()
	slot, err := os.MkdirTemp(filepath.Join(pluginsDir), ".staging-")
	if err != nil {
		t.Fatal(err)
	}
	return writeHelperPlugin(t, slot, id, version, mode)
}

func TestManagerUpdateSwapsVersionWhenItIdentifies(t *testing.T) {
	m, dir := newTestManager(t)
	writeHelperPlugin(t, dir, "p", "1.0.0", "sleep")
	if err := m.Load("p"); err != nil {
		t.Fatal(err)
	}
	staged := stageHelperPlugin(t, dir, "p", "2.0.0", "sleep")

	// Stand in for the server: the new process "identifies" shortly after it starts.
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			case <-time.After(50 * time.Millisecond):
				_ = m.MarkRunning("p")
			}
		}
	}()

	if err := m.Update("p", staged); err != nil {
		t.Fatalf("Update: %v", err)
	}
	manifest, _ := m.Registry().Manifest("p")
	if manifest.Plugin.Version != "2.0.0" {
		t.Fatalf("registry has version %s after update", manifest.Plugin.Version)
	}
	if _, err := os.Stat(filepath.Join(dir, ".backup", "p")); !os.IsNotExist(err) {
		t.Error("backup of the old version should be removed after a successful update")
	}
}

func TestManagerUpdateRollsBackWhenNewVersionCrashes(t *testing.T) {
	m, dir := newTestManager(t)
	writeHelperPlugin(t, dir, "p", "1.0.0", "sleep")
	if err := m.Load("p"); err != nil {
		t.Fatal(err)
	}
	staged := stageHelperPlugin(t, dir, "p", "2.0.0", "exit")

	err := m.Update("p", staged)
	if err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("Update of a crashing version = %v, want a rolled-back error", err)
	}
	manifest, err := LoadPluginFolder(filepath.Join(dir, "p"))
	if err != nil || manifest.Plugin.Version != "1.0.0" {
		t.Fatalf("live folder after rollback: %v %v", manifest, err)
	}
	if reg, _ := m.Registry().Manifest("p"); reg.Plugin.Version != "1.0.0" {
		t.Fatalf("registry after rollback has %s", reg.Plugin.Version)
	}
	if m.currentSupervisor("p") == nil {
		t.Fatal("the previous version should be running again after rollback")
	}
	if _, err := os.Stat(filepath.Join(dir, ".backup", "p.failed")); err != nil {
		t.Error("the failed version should be kept in .backup/p.failed")
	}
}

func TestManagerRescanPicksUpAddedAndRemovedFolders(t *testing.T) {
	m, dir := newTestManager(t)
	writeHelperPlugin(t, dir, "a", "1.0.0", "sleep")
	res := m.Rescan()
	if len(res.Added) != 1 || res.Added[0] != "a" || m.currentSupervisor("a") == nil {
		t.Fatalf("rescan after adding a: %+v", res)
	}
	if err := os.RemoveAll(filepath.Join(dir, "a")); err != nil {
		// Windows can hold the folder while the process runs; stop it first.
		m.Unload("a")
		_ = os.RemoveAll(filepath.Join(dir, "a"))
		m.Rescan()
		return
	}
	res = m.Rescan()
	if len(res.Removed) != 1 || m.currentSupervisor("a") != nil {
		t.Fatalf("rescan after removing a: %+v", res)
	}
}
