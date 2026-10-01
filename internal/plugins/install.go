package plugins

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// InstallRequest is what an admin supplies (Settings > Plugins > install or
// update) to have the server fetch a plugin.
type InstallRequest struct {
	// PluginID, when set, is the plugin the archive must contain (an
	// update). For a fresh install it's read from the archive's plugin.toml.
	PluginID string
	// SourceURL is what the admin typed: a GitHub repo ("owner/name" or its
	// URL), a GitHub release-asset link, or any https link to a .zip. See
	// ResolveSource.
	SourceURL string
	// SHA256 optionally pins the archive's checksum. Usually left empty:
	// GitHub's published digest (or a "<url>.sha256" file) is used instead.
	SHA256 string
}

// validPluginIDPattern mirrors the folder-name safety the registry already
// implicitly relies on (Discover matches [plugin].id against the actual
// folder name) -- rejecting anything that isn't a plain identifier here,
// before it ever becomes a filesystem path, is what actually prevents path
// traversal via a crafted plugin id (e.g. "../../etc").
var validPluginIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// Fetched is a plugin downloaded, verified and unpacked into staging.
type Fetched struct {
	Dir     string // pluginsDir/.staging/<random>/<id>
	ID      string
	Version string
	// Verified says what the download was checked against: "GitHub",
	// "a .sha256 file", "you", or "" (nothing published; https only).
	Verified string
}

// FetchToStaging resolves req.SourceURL (ResolveSource), downloads the
// archive -- verifying its SHA-256 whenever one is known -- and extracts it
// to pluginsDir/.staging/<random>/<id>/, where <id> comes from the
// archive's own plugin.toml. The manifest is validated exactly as
// discovery would, and the entrypoint marked executable. Nothing under
// pluginsDir/<id> is touched; the caller moves the folder into place
// (InstallFromURL for a fresh install, Manager.Update to replace one).
func FetchToStaging(ctx context.Context, pluginsDir string, req InstallRequest) (*Fetched, error) {
	if req.PluginID != "" && !validPluginIDPattern.MatchString(req.PluginID) {
		return nil, fmt.Errorf("invalid plugin id %q: must contain only letters, digits, '-', and '_'", req.PluginID)
	}
	if strings.TrimSpace(req.SourceURL) == "" {
		return nil, fmt.Errorf("enter a GitHub repo (owner/name) or a link to the plugin's .zip")
	}
	src, err := ResolveSource(ctx, req.SourceURL, req.SHA256)
	if err != nil {
		return nil, err
	}
	if err := validateSourceURL(src.URL); err != nil {
		return nil, err
	}

	stagingRoot := filepath.Join(pluginsDir, ".staging")
	if err := os.MkdirAll(stagingRoot, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create staging folder: %w", err)
	}
	slot, err := os.MkdirTemp(stagingRoot, "fetch-")
	if err != nil {
		return nil, fmt.Errorf("failed to create staging folder: %w", err)
	}
	fail := func(err error) (*Fetched, error) {
		_ = os.RemoveAll(slot)
		return nil, err
	}

	archivePath, err := downloadAndVerify(ctx, src.URL, src.SHA256)
	if err != nil {
		return fail(err)
	}
	defer os.Remove(archivePath)

	unpacked := filepath.Join(slot, "unpacked")
	if err := extractZip(archivePath, unpacked); err != nil {
		return fail(err)
	}
	if err := unwrapSingleFolder(unpacked); err != nil {
		return fail(err)
	}
	m, err := LoadManifest(filepath.Join(unpacked, "plugin.toml"))
	if err != nil {
		return fail(fmt.Errorf("that download isn't a Concord plugin (no valid plugin.toml): %w", err))
	}
	id := m.Plugin.ID
	if !validPluginIDPattern.MatchString(id) {
		return fail(fmt.Errorf("the plugin's [plugin].id %q isn't a usable folder name", id))
	}
	if req.PluginID != "" && id != req.PluginID {
		return fail(fmt.Errorf("that download is the plugin %q, not %q", id, req.PluginID))
	}
	staged := filepath.Join(slot, id)
	if err := os.Rename(unpacked, staged); err != nil {
		return fail(err)
	}
	manifest, err := LoadPluginFolder(staged)
	if err != nil {
		return fail(fmt.Errorf("plugin.toml is invalid: %w", err))
	}
	if err := ensureEntrypointExecutable(manifest); err != nil {
		return fail(err)
	}
	if err := VerifyClientCode(staged, manifest.Client); err != nil {
		return fail(err)
	}
	return &Fetched{Dir: staged, ID: id, Version: manifest.Plugin.Version, Verified: src.ChecksumFrom}, nil
}

// DiscardStaged removes a folder FetchToStaging produced (after a failed
// or abandoned update, or once it's been moved into place).
func DiscardStaged(f *Fetched) {
	if f == nil {
		return
	}
	// Only ever a .staging/fetch-* slot -- never, say, the plugins folder
	// if f.Dir has since been moved into place.
	slot := filepath.Dir(f.Dir)
	if filepath.Base(filepath.Dir(slot)) == ".staging" && strings.HasPrefix(filepath.Base(slot), "fetch-") {
		_ = os.RemoveAll(slot)
	}
}

// InstallFromURL fetches a plugin (FetchToStaging) and moves it into
// pluginsDir/<id>/ -- fresh installs only; an existing plugin is replaced
// through Manager.Update instead. The caller then starts it with
// Manager.Load.
func InstallFromURL(ctx context.Context, pluginsDir string, req InstallRequest) (*Fetched, error) {
	f, err := FetchToStaging(ctx, pluginsDir, req)
	if err != nil {
		return nil, err
	}
	defer DiscardStaged(f)
	destDir := filepath.Join(pluginsDir, f.ID)
	if _, err := os.Stat(destDir); err == nil {
		return nil, fmt.Errorf("%s is already installed -- update it instead", f.ID)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to check destination folder: %w", err)
	}
	if err := os.Rename(f.Dir, destDir); err != nil {
		return nil, fmt.Errorf("failed to move the plugin into place: %w", err)
	}
	installed := *f // the deferred DiscardStaged still sees the staging path
	installed.Dir = destDir
	return &installed, nil
}

// validateSourceURL requires https, so the archive (and, in effect, the
// code the server is about to run) can't be swapped in transit. Plain http
// is allowed only to a loopback host, for testing a plugin build served
// from the same machine.
func validateSourceURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid source URL %q", raw)
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" {
			return nil
		}
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			return nil
		}
		return fmt.Errorf("source URL must use https (plain http is only allowed from localhost)")
	default:
		return fmt.Errorf("source URL must use https, not %q", u.Scheme)
	}
}

// ensureEntrypointExecutable checks the archive really contains this OS's
// entrypoint binary -- on every OS, so a broken archive fails the same way
// everywhere (it once installed "fine" on Windows and failed only on Linux)
// -- and marks it executable. Zip archives built on Windows carry no Unix
// permission bits, so without this the extracted binary is 0600 and the
// plugin can never start on a Linux or macOS server (including Docker).
func ensureEntrypointExecutable(m *Manifest) error {
	ep, err := m.Entrypoint()
	if err != nil {
		return err
	}
	binPath := ep.Bin
	if !filepath.IsAbs(binPath) {
		binPath = filepath.Join(m.Dir, binPath)
	}
	info, err := os.Stat(binPath)
	if err != nil {
		return fmt.Errorf("archive has no entrypoint binary at %q: %w", ep.Bin, err)
	}
	if runtime.GOOS == "windows" {
		return nil // no executable bit to set
	}
	if err := os.Chmod(binPath, info.Mode().Perm()|0o755); err != nil {
		return fmt.Errorf("failed to make %q executable: %w", ep.Bin, err)
	}
	return nil
}

// downloadAndVerify streams sourceURL to a temp file while hashing it, and
// returns the temp file's path only if the hash matches expectedSHA256
// (skipped when it's empty: nothing was published to check against)
// (case-insensitive hex compare). The caller owns removing the temp file.
func downloadAndVerify(ctx context.Context, sourceURL, expectedSHA256 string) (string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to build download request: %w", err)
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("failed to download plugin archive: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download plugin archive: server returned %s", resp.Status)
	}

	tmp, err := os.CreateTemp("", "concord-plugin-install-*.zip")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmp.Close()

	hasher := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hasher), resp.Body); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("failed to save downloaded archive: %w", err)
	}

	got := hex.EncodeToString(hasher.Sum(nil))
	if expectedSHA256 != "" && !strings.EqualFold(got, expectedSHA256) {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, got)
	}

	return tmp.Name(), nil
}

// looksLikeWebPage reports whether a download is HTML: what a link to a
// GitHub page (rather than to a release file) returns.
func looksLikeWebPage(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	s := strings.ToLower(strings.TrimSpace(string(head[:n])))
	return strings.HasPrefix(s, "<!doctype html") || strings.HasPrefix(s, "<html")
}

// extractZip unpacks a zip archive into destDir, which must not already
// exist. Guards against zip-slip (an archive entry whose path escapes
// destDir via "../" components or an absolute path) by resolving every
// entry's target path and refusing any that lands outside destDir.
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
		if looksLikeWebPage(archivePath) {
			return fmt.Errorf("that link is a web page, not a .zip: enter the GitHub repo as owner/name, or a link to the .zip file itself")
		}
		return fmt.Errorf("failed to open downloaded archive as zip: %w", err)
	}
	defer r.Close()

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("failed to create plugin folder: %w", err)
	}

	for _, f := range r.File {
		targetPath := filepath.Join(destDir, f.Name)
		if !strings.HasPrefix(targetPath, filepath.Clean(destDir)+string(os.PathSeparator)) && targetPath != filepath.Clean(destDir) {
			return fmt.Errorf("archive entry %q escapes the destination folder", f.Name)
		}

		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(targetPath, 0o755); err != nil {
				return fmt.Errorf("failed to create folder %q: %w", f.Name, err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
			return fmt.Errorf("failed to create parent folder for %q: %w", f.Name, err)
		}

		if err := extractZipFile(f, targetPath); err != nil {
			return err
		}
	}

	return nil
}

func extractZipFile(f *zip.File, targetPath string) error {
	src, err := f.Open()
	if err != nil {
		return fmt.Errorf("failed to open archive entry %q: %w", f.Name, err)
	}
	defer src.Close()

	dst, err := os.OpenFile(targetPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode().Perm()|0o600)
	if err != nil {
		return fmt.Errorf("failed to write %q: %w", f.Name, err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to write %q: %w", f.Name, err)
	}
	return nil
}

// unwrapSingleFolder handles the common archive layout where everything
// sits inside one top-level folder (e.g. "concord-chess/plugin.toml", as
// zipping a folder produces): if dir has no plugin.toml of its own but
// holds exactly one folder, that folder's contents are moved up into dir.
func unwrapSingleFolder(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, "plugin.toml")); err == nil {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) != 1 || !entries[0].IsDir() {
		return nil // leave it; LoadPluginFolder reports the missing manifest
	}
	// Move the folder out of the way first: it often holds a file with its
	// own name (a "concord-chess" binary in "concord-chess/"), which can't
	// be moved up while the folder still sits at that name.
	inner := dir + ".wrapped"
	if err := os.Rename(filepath.Join(dir, entries[0].Name()), inner); err != nil {
		return fmt.Errorf("failed to unpack the archive's %s folder: %w", entries[0].Name(), err)
	}
	children, err := os.ReadDir(inner)
	if err != nil {
		return err
	}
	for _, c := range children {
		if err := os.Rename(filepath.Join(inner, c.Name()), filepath.Join(dir, c.Name())); err != nil {
			return fmt.Errorf("failed to unpack the archive's %s folder: %w", entries[0].Name(), err)
		}
	}
	return os.Remove(inner)
}
