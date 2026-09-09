package plugins

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// InstallRequest is what an admin supplies (via Settings > Plugins > install
// new plugin, OpPluginInstall) to have the server fetch and place a new
// plugin. First slice only -- see InstallFromURL's doc comment for what's
// explicitly out of scope.
type InstallRequest struct {
	PluginID  string // becomes the folder name under pluginsDir; must match plugin.toml's [plugin].id
	SourceURL string // a release archive (.zip) URL
	SHA256    string // expected hex-encoded SHA256 of the downloaded archive
}

// validPluginIDPattern mirrors the folder-name safety the registry already
// implicitly relies on (Discover matches [plugin].id against the actual
// folder name) -- rejecting anything that isn't a plain identifier here,
// before it ever becomes a filesystem path, is what actually prevents path
// traversal via a crafted PluginID (e.g. "../../etc").
var validPluginIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// InstallFromURL downloads a plugin release archive, verifies its SHA256
// checksum, and extracts it into pluginsDir/<PluginID>/, then confirms the
// extracted plugin.toml actually declares that same ID -- mirroring
// Discover's own folder-name-matches-manifest-id check, so a newly
// installed plugin passes the exact same validation an already-installed
// one does.
//
// Explicitly out of scope for this first slice (see the pre-v0.1.0 plan):
// signature verification (checksum only), hot-swapping or updating an
// already-installed plugin (this refuses to overwrite an existing folder),
// and picking the new plugin up without a server restart -- Manager.LoadAll
// isn't safe to re-invoke live while other plugins' supervisors are already
// running (it unconditionally rotates every existing plugin's auth token
// and starts a fresh supervisor without stopping the old one first, which
// would orphan the running process and immediately invalidate its token).
// A restart is the honest, safe way to activate a freshly installed plugin
// until that gap is closed as its own follow-up.
func InstallFromURL(ctx context.Context, pluginsDir string, req InstallRequest) error {
	if !validPluginIDPattern.MatchString(req.PluginID) {
		return fmt.Errorf("invalid plugin id %q: must contain only letters, digits, '-', and '_'", req.PluginID)
	}
	if req.SourceURL == "" {
		return fmt.Errorf("source URL is required")
	}
	if req.SHA256 == "" {
		return fmt.Errorf("a SHA256 checksum is required")
	}

	destDir := filepath.Join(pluginsDir, req.PluginID)
	if _, err := os.Stat(destDir); err == nil {
		return fmt.Errorf("a plugin folder already exists at %q -- this first-slice install flow only supports fresh installs, not updating an existing one", destDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to check destination folder: %w", err)
	}

	archivePath, err := downloadAndVerify(ctx, req.SourceURL, req.SHA256)
	if err != nil {
		return err
	}
	defer os.Remove(archivePath)

	if err := extractZip(archivePath, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return err
	}

	manifest, err := LoadManifest(filepath.Join(destDir, "plugin.toml"))
	if err != nil {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("extracted archive has no valid plugin.toml: %w", err)
	}
	if err := manifest.Validate(); err != nil {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("extracted plugin.toml is invalid: %w", err)
	}
	if manifest.Plugin.ID != req.PluginID {
		_ = os.RemoveAll(destDir)
		return fmt.Errorf("archive's plugin.toml declares id %q, which does not match the requested plugin id %q", manifest.Plugin.ID, req.PluginID)
	}

	return nil
}

// downloadAndVerify streams sourceURL to a temp file while hashing it, and
// returns the temp file's path only if the hash matches expectedSHA256
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
	if !strings.EqualFold(got, expectedSHA256) {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("checksum mismatch: expected %s, got %s", expectedSHA256, got)
	}

	return tmp.Name(), nil
}

// extractZip unpacks a zip archive into destDir, which must not already
// exist. Guards against zip-slip (an archive entry whose path escapes
// destDir via "../" components or an absolute path) by resolving every
// entry's target path and refusing any that lands outside destDir.
func extractZip(archivePath, destDir string) error {
	r, err := zip.OpenReader(archivePath)
	if err != nil {
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
