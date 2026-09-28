package plugins

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"runtime"
	"strings"
	"time"
)

// ResolvedSource is a concrete archive to download, with the checksum to
// verify it against when one is published.
type ResolvedSource struct {
	URL    string
	SHA256 string // "" when the source publishes none (the download is still https)
	// ChecksumFrom says where SHA256 came from, for the admin's result
	// message: "you", "GitHub", "<file>.sha256", or "" (none published).
	ChecksumFrom string
}

// githubAPI is the GitHub REST API base; a variable so tests can point it
// at a fake.
var githubAPI = "https://api.github.com"

var (
	// "owner/repo", optionally with a leading "github.com/" or full https URL.
	githubRepoPattern = regexp.MustCompile(`^(?:https?://)?(?:www\.)?(?:github\.com/)?([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)
	// A release asset link: https://github.com/<owner>/<repo>/releases/download/<tag>/<file>
	githubAssetPattern = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/releases/download/([^/]+)/([^/]+)$`)
	sha256HexPattern   = regexp.MustCompile(`^[0-9a-fA-F]{64}$`)
)

// ResolveSource turns what an admin typed into something to download:
//
//   - "owner/repo" or a github.com repo URL → the latest release's .zip for
//     this server's OS and CPU (asset names containing e.g. "linux" and
//     "amd64"/"x86_64"), verified against GitHub's published digest.
//   - a GitHub release-asset link → that file, with GitHub's digest.
//   - any other https URL → that file, checked against "<url>.sha256" if
//     the host publishes one.
//
// checksum, when given, overrides whatever would be looked up.
func ResolveSource(ctx context.Context, source, checksum string) (ResolvedSource, error) {
	source = strings.TrimSpace(source)
	checksum = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(checksum), "sha256:")))
	if checksum != "" && !sha256HexPattern.MatchString(checksum) {
		return ResolvedSource{}, fmt.Errorf("that checksum isn't a SHA-256 (64 hex characters)")
	}
	withChecksum := func(r ResolvedSource) ResolvedSource {
		if checksum != "" {
			r.SHA256, r.ChecksumFrom = checksum, "you"
		}
		return r
	}

	if m := githubAssetPattern.FindStringSubmatch(source); m != nil {
		owner, repo, tag, file := m[1], m[2], m[3], m[4]
		r := ResolvedSource{URL: source}
		if checksum == "" {
			if rel, err := githubRelease(ctx, owner, repo, "tags/"+url.PathEscape(tag)); err == nil {
				for _, a := range rel.Assets {
					if a.Name == file {
						r.SHA256, r.ChecksumFrom = digestHex(a.Digest), "GitHub"
					}
				}
			}
		}
		return withChecksum(r), nil
	}

	if !strings.Contains(source, "://") || strings.HasPrefix(source, "https://github.com/") || strings.HasPrefix(source, "http://github.com/") {
		if m := githubRepoPattern.FindStringSubmatch(source); m != nil {
			rel, err := githubRelease(ctx, m[1], m[2], "latest")
			if err != nil {
				return ResolvedSource{}, err
			}
			asset, err := pickAsset(rel.Assets, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return ResolvedSource{}, fmt.Errorf("%s/%s %s: %w", m[1], m[2], rel.TagName, err)
			}
			r := ResolvedSource{URL: asset.URL, SHA256: digestHex(asset.Digest)}
			if r.SHA256 != "" {
				r.ChecksumFrom = "GitHub"
			}
			return withChecksum(r), nil
		}
	}

	if err := validateSourceURL(source); err != nil {
		return ResolvedSource{}, fmt.Errorf("enter a GitHub repo (owner/name) or a link to a plugin .zip: %w", err)
	}
	r := ResolvedSource{URL: source}
	if checksum == "" {
		if sum := fetchSidecarChecksum(ctx, source); sum != "" {
			r.SHA256, r.ChecksumFrom = sum, "a .sha256 file"
		}
	}
	return withChecksum(r), nil
}

type githubAsset struct {
	Name   string `json:"name"`
	URL    string `json:"browser_download_url"`
	Digest string `json:"digest"` // "sha256:<hex>"; absent on assets uploaded before GitHub added digests
}

type githubReleaseInfo struct {
	TagName string        `json:"tag_name"`
	Assets  []githubAsset `json:"assets"`
}

func githubRelease(ctx context.Context, owner, repo, which string) (*githubReleaseInfo, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("%s/repos/%s/%s/releases/%s", githubAPI, owner, repo, which), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("%s/%s has no published release on GitHub (or the repo is private)", owner, repo)
	default:
		return nil, fmt.Errorf("GitHub answered %s for %s/%s", resp.Status, owner, repo)
	}
	var rel githubReleaseInfo
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&rel); err != nil {
		return nil, fmt.Errorf("unexpected reply from GitHub: %w", err)
	}
	return &rel, nil
}

// archAliases are the CPU names release assets commonly use.
var archAliases = map[string][]string{
	"amd64": {"amd64", "x86_64", "x64"},
	"arm64": {"arm64", "aarch64"},
	"386":   {"386", "i386", "x86"},
	"arm":   {"armv7", "armhf", "arm"},
}

var osAliases = map[string][]string{
	"linux":   {"linux"},
	"windows": {"windows", "win"},
	"darwin":  {"darwin", "macos", "mac", "osx"},
}

// pickAsset finds the release's .zip built for goos/goarch.
func pickAsset(assets []githubAsset, goos, goarch string) (githubAsset, error) {
	var zips []string
	var match []githubAsset
	for _, a := range assets {
		name := strings.ToLower(a.Name)
		if !strings.HasSuffix(name, ".zip") {
			continue
		}
		zips = append(zips, a.Name)
		if nameHasToken(name, osAliases[goos]) && nameHasToken(name, archAliases[goarch]) {
			match = append(match, a)
		}
	}
	switch {
	case len(match) == 1:
		return match[0], nil
	case len(match) > 1:
		return githubAsset{}, fmt.Errorf("several downloads look right for %s/%s (%s); paste the exact link instead", goos, goarch, names(match))
	case len(zips) == 1 && !anyPlatformToken(strings.ToLower(zips[0])):
		// A single platform-neutral zip (e.g. a plugin shipping binaries
		// for every OS inside one archive).
		for _, a := range assets {
			if a.Name == zips[0] {
				return a, nil
			}
		}
	}
	if len(zips) == 0 {
		return githubAsset{}, fmt.Errorf("the latest release has no .zip download")
	}
	return githubAsset{}, fmt.Errorf("no download for this server (%s/%s) among: %s", goos, goarch, strings.Join(zips, ", "))
}

// nameHasToken reports whether one of tokens appears in name as its own
// word ("linux_amd64", "linux-x86_64"), not inside another ("darwin" in
// "darwin", but "arm" not inside "arm64").
func nameHasToken(name string, tokens []string) bool {
	// "x86_64"/"x86-64" contain separators themselves; normalize first.
	name = strings.NewReplacer("x86_64", "amd64", "x86-64", "amd64").Replace(name)
	parts := strings.FieldsFunc(strings.TrimSuffix(name, ".zip"), func(r rune) bool {
		return r == '_' || r == '-' || r == '.' || r == ' '
	})
	for _, p := range parts {
		for _, t := range tokens {
			if p == t {
				return true
			}
		}
	}
	return false
}

func anyPlatformToken(name string) bool {
	for _, set := range []map[string][]string{osAliases, archAliases} {
		for _, tokens := range set {
			if nameHasToken(name, tokens) {
				return true
			}
		}
	}
	return false
}

func names(assets []githubAsset) string {
	out := make([]string, len(assets))
	for i, a := range assets {
		out[i] = a.Name
	}
	return strings.Join(out, ", ")
}

// digestHex extracts the hex from GitHub's "sha256:<hex>" digest.
func digestHex(digest string) string {
	hexPart := strings.TrimPrefix(digest, "sha256:")
	if sha256HexPattern.MatchString(hexPart) {
		return strings.ToLower(hexPart)
	}
	return ""
}

// fetchSidecarChecksum reads "<url>.sha256" (the checksum-file convention
// most release tooling follows): its first word, if that's a SHA-256.
func fetchSidecarChecksum(ctx context.Context, archiveURL string) string {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, archiveURL+".sha256", nil)
	if err != nil {
		return ""
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	fields := strings.Fields(string(body))
	if len(fields) == 0 || !sha256HexPattern.MatchString(fields[0]) {
		return ""
	}
	if _, err := hex.DecodeString(fields[0]); err != nil {
		return ""
	}
	return strings.ToLower(fields[0])
}
