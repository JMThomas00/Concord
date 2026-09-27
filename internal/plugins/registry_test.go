package plugins

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDiscover_NoPluginsDir(t *testing.T) {
	reg, errs := Discover(filepath.Join(t.TempDir(), "does-not-exist"))
	if len(errs) != 0 {
		t.Errorf("expected no errors for a missing Plugins directory, got %v", errs)
	}
	if len(reg.All()) != 0 {
		t.Errorf("expected no plugins discovered, got %d", len(reg.All()))
	}
}

// process.go joins manifest.Dir with the entrypoint and ALSO sets cmd.Dir to
// the plugin folder; a relative Dir is then resolved a second time from
// inside that folder, so every plugin failed to launch whenever plugins_dir
// was relative (the default "Plugins"). Found live 2026-09-26.
func TestDiscover_RelativePluginsDirYieldsAbsoluteManifestDir(t *testing.T) {
	reg, errs := Discover("testdata")
	if len(errs) != 0 {
		t.Fatalf("unexpected discovery errors: %v", errs)
	}
	m, ok := reg.Manifest("HelloPlugin")
	if !ok {
		t.Fatal("expected HelloPlugin to be discovered")
	}
	if !filepath.IsAbs(m.Dir) {
		t.Errorf("manifest.Dir = %q, want an absolute path so the plugin binary resolves independently of the process's working directory", m.Dir)
	}
}

func TestDiscover_HelloPluginFixture(t *testing.T) {
	reg, errs := Discover("testdata")
	if len(errs) != 0 {
		t.Fatalf("unexpected discovery errors: %v", errs)
	}

	m, ok := reg.Manifest("HelloPlugin")
	if !ok {
		t.Fatal("expected HelloPlugin to be discovered")
	}
	if m.Plugin.Name != "Hello Plugin" {
		t.Errorf("expected name 'Hello Plugin', got %q", m.Plugin.Name)
	}

	ck, ok := reg.Lookup("HelloPlugin", "counter")
	if !ok {
		t.Fatal("expected HelloPlugin:counter channel kind to be registered")
	}
	if !ck.RemotePane {
		t.Error("expected counter channel kind to be remote_pane")
	}
}

func TestDiscover_FolderNameMismatch(t *testing.T) {
	dir := t.TempDir()
	pluginDir := filepath.Join(dir, "ActualFolderName")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatal(err)
	}
	writeManifest(t, pluginDir, `
[plugin]
id = "DifferentID"
name = "Mismatched"
version = "1.0.0"
[process.entrypoint.windows]
bin = "x.exe"
[process.entrypoint.linux]
bin = "x"
[process.entrypoint.darwin]
bin = "x"
`)

	reg, errs := Discover(dir)
	if len(errs) == 0 {
		t.Fatal("expected a folder-name/id mismatch error")
	}
	if len(reg.All()) != 0 {
		t.Errorf("mismatched plugin should not be registered, got %d", len(reg.All()))
	}
}
