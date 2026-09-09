package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestManifestRoundTripsSourceURL is a regression test for item 10/13's
// manifest schema addition: PluginDef had no field for a plugin's release
// source at all before this, so admin-triggered install/update had nothing
// to point at even declaratively.
func TestManifestRoundTripsSourceURL(t *testing.T) {
	dir := t.TempDir()
	path := writeManifest(t, dir, `
[plugin]
id = "example"
name = "Example"
version = "1.0.0"
source_url = "https://github.com/example/example/releases/download/v1.0.0/example.zip"

[process]
[process.entrypoint.windows]
bin = "example.exe"
[process.entrypoint.linux]
bin = "example"
[process.entrypoint.darwin]
bin = "example"
`)

	m, err := LoadManifest(path)
	if err != nil {
		t.Fatalf("LoadManifest failed: %v", err)
	}
	want := "https://github.com/example/example/releases/download/v1.0.0/example.zip"
	if m.Plugin.SourceURL != want {
		t.Errorf("SourceURL = %q, want %q", m.Plugin.SourceURL, want)
	}
}

// buildTestPluginZip builds an in-memory zip archive containing a valid
// plugin.toml (declaring pluginID) plus a small fake entrypoint file, and
// returns its bytes and hex-encoded SHA256.
func buildTestPluginZip(t *testing.T, pluginID string) (data []byte, sha256Hex string) {
	t.Helper()

	var buf bytes.Buffer
	w := zip.NewWriter(&buf)

	manifest := `
[plugin]
id = "` + pluginID + `"
name = "Test Plugin"
version = "1.0.0"

[process]
[process.entrypoint.windows]
bin = "test.exe"
[process.entrypoint.linux]
bin = "test"
[process.entrypoint.darwin]
bin = "test"
`
	f, err := w.Create("plugin.toml")
	if err != nil {
		t.Fatalf("zip.Create(plugin.toml): %v", err)
	}
	if _, err := f.Write([]byte(manifest)); err != nil {
		t.Fatalf("write plugin.toml: %v", err)
	}

	binFile, err := w.Create("test")
	if err != nil {
		t.Fatalf("zip.Create(test): %v", err)
	}
	if _, err := binFile.Write([]byte("#!/bin/sh\necho fake plugin binary\n")); err != nil {
		t.Fatalf("write test binary: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("zip.Close: %v", err)
	}

	sum := sha256.Sum256(buf.Bytes())
	return buf.Bytes(), hex.EncodeToString(sum[:])
}

func servePluginZip(t *testing.T, data []byte) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(data)
	}))
}

// TestInstallFromURL_Success confirms the full fetch/verify/place flow: a
// valid archive with a matching checksum gets extracted into
// pluginsDir/<PluginID>/, and the resulting plugin.toml is picked up by
// registry.Discover afterward exactly like a manually-dropped plugin
// folder would be.
func TestInstallFromURL_Success(t *testing.T) {
	data, sum := buildTestPluginZip(t, "testplug")
	srv := servePluginZip(t, data)
	defer srv.Close()

	pluginsDir := t.TempDir()
	err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
		PluginID:  "testplug",
		SourceURL: srv.URL,
		SHA256:    sum,
	})
	if err != nil {
		t.Fatalf("InstallFromURL failed: %v", err)
	}

	manifestPath := filepath.Join(pluginsDir, "testplug", "plugin.toml")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("expected plugin.toml to exist at %q: %v", manifestPath, err)
	}

	reg, errs := Discover(pluginsDir)
	if len(errs) != 0 {
		t.Fatalf("Discover returned errors: %v", errs)
	}
	if _, ok := reg.Manifest("testplug"); !ok {
		t.Error("expected Discover to pick up the newly installed plugin")
	}
}

// TestInstallFromURL_ChecksumMismatchRejected confirms a downloaded archive
// that doesn't match the expected checksum is rejected and nothing gets
// placed on disk -- the core safety property of the "verify it" half of
// "fetch, verify, place."
func TestInstallFromURL_ChecksumMismatchRejected(t *testing.T) {
	data, _ := buildTestPluginZip(t, "testplug")
	srv := servePluginZip(t, data)
	defer srv.Close()

	pluginsDir := t.TempDir()
	err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
		PluginID:  "testplug",
		SourceURL: srv.URL,
		SHA256:    "0000000000000000000000000000000000000000000000000000000000000",
	})
	if err == nil {
		t.Fatal("expected an error for a checksum mismatch")
	}

	if _, statErr := os.Stat(filepath.Join(pluginsDir, "testplug")); !os.IsNotExist(statErr) {
		t.Error("expected no plugin folder to be created after a checksum mismatch")
	}
}

// TestInstallFromURL_ManifestIDMismatchRejected confirms an archive whose
// plugin.toml declares a different [plugin].id than the requested folder
// name is rejected and cleaned up -- mirrors Discover's own
// folder-name-matches-manifest-id validation, applied at install time
// instead of at next-startup discovery time.
func TestInstallFromURL_ManifestIDMismatchRejected(t *testing.T) {
	data, sum := buildTestPluginZip(t, "actual-id")
	srv := servePluginZip(t, data)
	defer srv.Close()

	pluginsDir := t.TempDir()
	err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
		PluginID:  "requested-id", // does not match the archive's declared id
		SourceURL: srv.URL,
		SHA256:    sum,
	})
	if err == nil {
		t.Fatal("expected an error for a plugin id mismatch")
	}
	if _, statErr := os.Stat(filepath.Join(pluginsDir, "requested-id")); !os.IsNotExist(statErr) {
		t.Error("expected the extracted folder to be cleaned up after an id mismatch")
	}
}

// TestInstallFromURL_RefusesExistingFolder confirms this first slice's
// documented scope: it only supports fresh installs, never silently
// overwriting an already-installed plugin (that's the explicitly punted
// hot-swap/update case).
func TestInstallFromURL_RefusesExistingFolder(t *testing.T) {
	pluginsDir := t.TempDir()
	existing := filepath.Join(pluginsDir, "testplug")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("failed to pre-create existing plugin folder: %v", err)
	}

	err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
		PluginID:  "testplug",
		SourceURL: "http://example.invalid/should-not-be-fetched.zip",
		SHA256:    "irrelevant",
	})
	if err == nil {
		t.Fatal("expected an error when the destination folder already exists")
	}
}

// TestInstallFromURL_RejectsUnsafePluginID confirms a crafted plugin ID
// can't be used for path traversal (e.g. "../../etc") -- checked before any
// network call or filesystem write happens.
func TestInstallFromURL_RejectsUnsafePluginID(t *testing.T) {
	pluginsDir := t.TempDir()

	for _, badID := range []string{"../evil", "..\\evil", "a/b", "", "a b"} {
		err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
			PluginID:  badID,
			SourceURL: "http://example.invalid/should-not-be-fetched.zip",
			SHA256:    "irrelevant",
		})
		if err == nil {
			t.Errorf("expected an error for unsafe plugin id %q", badID)
		}
	}
}
