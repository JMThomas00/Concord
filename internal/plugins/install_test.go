package plugins

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

	// One entrypoint per OS the manifest names, so the install is valid
	// wherever the test runs.
	for _, name := range []string{"test", "test.exe"} {
		binFile, err := w.Create(name)
		if err != nil {
			t.Fatalf("zip.Create(%s): %v", name, err)
		}
		if _, err := binFile.Write([]byte("#!/bin/sh\necho fake plugin binary\n")); err != nil {
			t.Fatalf("write test binary: %v", err)
		}
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
	_, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
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
	_, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
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
	_, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{
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

// A fresh install never overwrites an installed plugin -- replacing one is
// Manager.Update's job, with its rollback.
func TestInstallFromURL_RefusesExistingPlugin(t *testing.T) {
	data, sum := buildTestPluginZip(t, "testplug")
	srv := servePluginZip(t, data)
	defer srv.Close()
	pluginsDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pluginsDir, "testplug"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{SourceURL: srv.URL, SHA256: sum})
	if err == nil || !strings.Contains(err.Error(), "already installed") {
		t.Fatalf("installing over an existing plugin = %v, want an already-installed error", err)
	}
}

// The plugin ID becomes a folder name, so one that could escape the plugins
// folder is refused -- whether an admin typed it or the archive declares it.
func TestInstallFromURL_RejectsUnsafePluginID(t *testing.T) {
	pluginsDir := t.TempDir()
	for _, badID := range []string{"../evil", "..\\evil", "a/b", "a b"} {
		if _, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{PluginID: badID, SourceURL: "https://example.invalid/p.zip"}); err == nil {
			t.Errorf("expected an error for unsafe plugin id %q", badID)
		}
	}

	data, sum := buildTestPluginZip(t, "../evil")
	srv := servePluginZip(t, data)
	defer srv.Close()
	if _, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{SourceURL: srv.URL, SHA256: sum}); err == nil {
		t.Fatal("an archive declaring id ../evil was installed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(pluginsDir), "evil")); err == nil {
		t.Fatal("something was written outside the plugins folder")
	}
}

// With no checksum typed, a plain link is still installed (it's https in
// real use), and a "<url>.sha256" file next to it is used when present.
func TestInstallFromURL_ChecksumIsOptionalAndSidecarIsUsed(t *testing.T) {
	data, sum := buildTestPluginZip(t, "sidecar")
	var sidecar string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".sha256") {
			if sidecar == "" {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(sidecar + "  plugin.zip\n"))
			return
		}
		_, _ = w.Write(data)
	}))
	defer srv.Close()

	f, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: srv.URL + "/plugin.zip"})
	if err != nil || f.ID != "sidecar" || f.Verified != "" {
		t.Fatalf("no-checksum install = %+v, %v", f, err)
	}

	sidecar = sum
	f, err = InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: srv.URL + "/plugin.zip"})
	if err != nil || f.Verified != "a .sha256 file" {
		t.Fatalf("sidecar install = %+v, %v", f, err)
	}

	sidecar = strings.Repeat("0", 64) // tampered
	if _, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: srv.URL + "/plugin.zip"}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("a download not matching its .sha256 file = %v, want a checksum mismatch", err)
	}
}

// "owner/repo" installs the latest release's zip for this OS/CPU, verified
// against the digest GitHub publishes for it.
func TestInstallFromGitHubRepoPicksPlatformAssetAndUsesDigest(t *testing.T) {
	data, sum := buildTestPluginZip(t, "concord-chess")
	var digest = "sha256:" + sum
	var files *httptest.Server
	files = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write(data) }))
	defer files.Close()
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/jordan/concord-chess/releases/latest" && r.URL.Path != "/repos/jordan/concord-chess/releases/tags/v1.2.0" {
			http.NotFound(w, r)
			return
		}
		mine := fmt.Sprintf("concord-chess_%s_%s.zip", runtime.GOOS, runtime.GOARCH)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"tag_name": "v1.2.0",
			"assets": []map[string]string{
				{"name": "concord-chess_plan9_mips.zip", "browser_download_url": files.URL + "/other.zip"},
				{"name": mine, "browser_download_url": files.URL + "/" + mine, "digest": digest},
				{"name": "checksums.txt", "browser_download_url": files.URL + "/checksums.txt"},
			},
		})
	}))
	defer api.Close()
	defer func(orig string) { githubAPI = orig }(githubAPI)
	githubAPI = api.URL

	for _, source := range []string{"jordan/concord-chess", "https://github.com/jordan/concord-chess", "github.com/jordan/concord-chess.git",
		// Links copied from the browser: the releases list, a release's page, a file.
		"https://github.com/jordan/concord-chess/releases", "https://github.com/jordan/concord-chess/releases/tag/v1.2.0",
		"https://github.com/jordan/concord-chess/blob/main/README.md"} {
		f, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: source})
		if err != nil || f.ID != "concord-chess" || f.Verified != "GitHub" {
			t.Fatalf("%s: %+v, %v", source, f, err)
		}
	}

	digest = "sha256:" + strings.Repeat("a", 64)
	if _, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: "jordan/concord-chess"}); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("a download not matching GitHub's digest = %v", err)
	}
	if _, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: "jordan/no-such-repo"}); err == nil || !strings.Contains(err.Error(), "no published release") {
		t.Fatalf("missing repo = %v", err)
	}
	if _, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: "https://github.com/jordan/concord-chess/releases/tag/v9.9.9"}); err == nil {
		t.Fatal("a release that doesn't exist was installed")
	}
}

func TestPickAsset(t *testing.T) {
	assets := []githubAsset{
		{Name: "tak_linux_x86_64.zip"}, {Name: "tak_linux_arm64.zip"},
		{Name: "tak_windows_amd64.zip"}, {Name: "tak_darwin_arm64.zip"}, {Name: "tak.tar.gz"},
	}
	for _, tc := range []struct{ goos, goarch, want string }{
		{"linux", "amd64", "tak_linux_x86_64.zip"},
		{"linux", "arm64", "tak_linux_arm64.zip"},
		{"windows", "amd64", "tak_windows_amd64.zip"},
		{"darwin", "arm64", "tak_darwin_arm64.zip"},
	} {
		if a, err := pickAsset(assets, tc.goos, tc.goarch); err != nil || a.Name != tc.want {
			t.Errorf("%s/%s: got %q, %v; want %s", tc.goos, tc.goarch, a.Name, err, tc.want)
		}
	}
	if _, err := pickAsset(assets, "freebsd", "amd64"); err == nil || !strings.Contains(err.Error(), "tak_linux_x86_64.zip") {
		t.Errorf("no match should list what's available: %v", err)
	}
	if a, err := pickAsset([]githubAsset{{Name: "tukan-plugin.zip"}}, "linux", "amd64"); err != nil || a.Name != "tukan-plugin.zip" {
		t.Errorf("a single platform-neutral zip should be used: %v %v", a, err)
	}
}

func TestValidateSourceURL(t *testing.T) {
	for raw, ok := range map[string]bool{
		"https://github.com/x/y/releases/download/v1/p.zip": true,
		"http://127.0.0.1:8123/p.zip":                        true,
		"http://localhost/p.zip":                             true,
		"http://[::1]:9000/p.zip":                            true,
		"http://example.com/p.zip":                           false,
		"http://192.168.1.66/p.zip":                          false,
		"ftp://example.com/p.zip":                            false,
		"file:///etc/passwd":                                 false,
		"not a url":                                          false,
	} {
		if err := validateSourceURL(raw); (err == nil) != ok {
			t.Errorf("validateSourceURL(%q) = %v, want ok=%v", raw, err, ok)
		}
	}
}

// Zips made on Windows (or by zip.Writer.Create, as here) carry no Unix
// permission bits; the installed entrypoint must still be executable.
func TestInstallFromURL_MakesEntrypointExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits don't apply on Windows")
	}
	data, sum := buildTestPluginZip(t, "execplug")
	srv := servePluginZip(t, data)
	defer srv.Close()

	pluginsDir := t.TempDir()
	if _, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{PluginID: "execplug", SourceURL: srv.URL, SHA256: sum}); err != nil {
		t.Fatalf("InstallFromURL failed: %v", err)
	}
	info, err := os.Stat(filepath.Join(pluginsDir, "execplug", "test"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o100 == 0 {
		t.Errorf("entrypoint installed with mode %v, want it executable", info.Mode().Perm())
	}
}

// Zipping a plugin's folder (the usual way to make a release archive) puts
// everything under one top-level folder, usually holding a binary with the
// folder's own name (release.go's layout); that must install the same.
func TestInstallFromURL_UnwrapsSingleTopLevelFolder(t *testing.T) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	f, _ := w.Create("wrapped/plugin.toml")
	_, _ = f.Write([]byte("[plugin]\nid = \"wrapped\"\nname = \"W\"\nversion = \"1.0.0\"\n\n[process]\n" +
		"[process.entrypoint.windows]\nbin = \"wrapped.exe\"\n[process.entrypoint.linux]\nbin = \"wrapped\"\n[process.entrypoint.darwin]\nbin = \"wrapped\"\n"))
	for _, name := range []string{"wrapped/wrapped", "wrapped/wrapped.exe"} {
		b, _ := w.Create(name)
		_, _ = b.Write([]byte("bin"))
	}
	_ = w.Close()
	sum := sha256.Sum256(buf.Bytes())
	srv := servePluginZip(t, buf.Bytes())
	defer srv.Close()

	pluginsDir := t.TempDir()
	if _, err := InstallFromURL(context.Background(), pluginsDir, InstallRequest{PluginID: "wrapped", SourceURL: srv.URL, SHA256: hex.EncodeToString(sum[:])}); err != nil {
		t.Fatalf("InstallFromURL: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "wrapped", "plugin.toml")); err != nil {
		t.Fatalf("plugin.toml not at the plugin folder's root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, ".staging")); err == nil {
		entries, _ := os.ReadDir(filepath.Join(pluginsDir, ".staging"))
		if len(entries) != 0 {
			t.Errorf("staging left behind: %v", entries)
		}
	}
}

// Live check against the real GitHub API (opt-in: CONCORD_LIVE_GITHUB=1):
// a public repo's latest release resolves to this platform's .zip, with
// GitHub's published digest.
func TestResolveSourceLiveGitHub(t *testing.T) {
	if os.Getenv("CONCORD_LIVE_GITHUB") == "" {
		t.Skip("set CONCORD_LIVE_GITHUB=1 to hit the real GitHub API")
	}
	if runtime.GOOS != "windows" {
		t.Skip("charmbracelet/vhs only ships .zip builds for Windows")
	}
	src, err := ResolveSource(context.Background(), "charmbracelet/vhs", "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("resolved %s (checksum from %s: %s)", src.URL, src.ChecksumFrom, src.SHA256)
	if !strings.Contains(strings.ToLower(src.URL), "windows") || src.ChecksumFrom != "GitHub" || len(src.SHA256) != 64 {
		t.Fatalf("unexpected resolution: %+v", src)
	}
}

// A link to a web page (not a .zip) says so, instead of "not a valid zip".
func TestInstallFromAWebPageExplainsWhatToEnter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("\n<!DOCTYPE html><html><body>releases</body></html>"))
	}))
	defer srv.Close()
	_, err := InstallFromURL(context.Background(), t.TempDir(), InstallRequest{SourceURL: srv.URL + "/page"})
	if err == nil || !strings.Contains(err.Error(), "web page") {
		t.Fatalf("err = %v", err)
	}
}
