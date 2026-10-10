package installer

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Repo is where releases come from.
const Repo = "JMThomas00/Concord"

// Release is one published version.
type Release struct {
	Tag    string
	Assets map[string]Asset
}

// Asset is one downloadable file of a release.
type Asset struct {
	Name   string
	URL    string
	Size   int64
	SHA256 string // from GitHub's digest, or the release's SHA256SUMS
}

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// FetchRelease looks up a release on GitHub: tag "" is the latest.
func FetchRelease(ctx context.Context, tag string) (*Release, error) {
	url := "https://api.github.com/repos/" + Repo + "/releases/latest"
	if tag != "" {
		url = "https://api.github.com/repos/" + Repo + "/releases/tags/" + tag
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("couldn't reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound && tag == "":
		return nil, fmt.Errorf("there's no published Concord release yet")
	case resp.StatusCode == http.StatusNotFound:
		return nil, fmt.Errorf("there's no release called %s", tag)
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("GitHub is rate-limiting this address; try again in a few minutes")
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("GitHub answered %s", resp.Status)
	}
	var body struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Size   int64  `json:"size"`
			Digest string `json:"digest"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("GitHub's answer didn't make sense: %w", err)
	}
	r := &Release{Tag: body.TagName, Assets: map[string]Asset{}}
	for _, a := range body.Assets {
		r.Assets[a.Name] = Asset{Name: a.Name, URL: a.URL, Size: a.Size, SHA256: strings.TrimPrefix(a.Digest, "sha256:")}
	}
	if sums, ok := r.Assets["SHA256SUMS"]; ok {
		r.fillSums(ctx, sums.URL)
	}
	return r, nil
}

// fillSums takes checksums GitHub didn't give from the SHA256SUMS file.
func (r *Release) fillSums(ctx context.Context, url string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	s := bufio.NewScanner(io.LimitReader(resp.Body, 1<<20))
	for s.Scan() {
		f := strings.Fields(s.Text())
		if len(f) != 2 {
			continue
		}
		name := strings.TrimPrefix(f[1], "*")
		if a, ok := r.Assets[name]; ok && a.SHA256 == "" {
			a.SHA256 = strings.ToLower(f[0])
			r.Assets[name] = a
		}
	}
}

// Download fetches an asset into dir, checking its size and checksum, and
// reports progress as it goes.
func Download(ctx context.Context, a Asset, dir string, progress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download failed: %s", resp.Status)
	}
	total := a.Size
	if total <= 0 {
		total = resp.ContentLength
	}
	path := filepath.Join(dir, a.Name)
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	var done int64
	buf := make([]byte, 64<<10)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return "", err
			}
			h.Write(buf[:n])
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return "", fmt.Errorf("download interrupted: %w", rerr)
		}
	}
	if a.SHA256 != "" {
		if got := hex.EncodeToString(h.Sum(nil)); !strings.EqualFold(got, a.SHA256) {
			return "", fmt.Errorf("the download doesn't match its published checksum, so it wasn't used (try again)")
		}
	}
	return path, nil
}

// Unpack finds the Concord programs in an archive (or a folder of them)
// and copies them to dir. It returns the components it found.
func Unpack(src, dir string) ([]string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	want := map[string]string{} // file name → component
	for _, c := range []string{Client, Server, Hub} {
		want["concord-"+c] = c
		want["concord-"+c+".exe"] = c
	}
	found := map[string]bool{}
	keep := func(name string, r io.Reader) error {
		c, ok := want[filepath.Base(filepath.ToSlash(name))]
		if !ok || found[c] {
			return nil
		}
		out, err := os.OpenFile(filepath.Join(dir, filepath.Base(name)), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		if _, err := io.Copy(out, r); err != nil {
			out.Close()
			return err
		}
		found[c] = true
		return out.Close()
	}

	st, err := os.Stat(src)
	if err != nil {
		return nil, err
	}
	switch {
	case st.IsDir():
		entries, err := os.ReadDir(src)
		if err != nil {
			return nil, err
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			f, err := os.Open(filepath.Join(src, e.Name()))
			if err != nil {
				return nil, err
			}
			err = keep(e.Name(), f)
			f.Close()
			if err != nil {
				return nil, err
			}
		}
	case strings.HasSuffix(src, ".zip"):
		zr, err := zip.OpenReader(src)
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %w", err)
		}
		defer zr.Close()
		for _, f := range zr.File {
			if f.FileInfo().IsDir() {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			err = keep(f.Name, rc)
			rc.Close()
			if err != nil {
				return nil, err
			}
		}
	default: // .tar.gz
		f, err := os.Open(src)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, fmt.Errorf("the archive is damaged: %w", err)
		}
		tr := tar.NewReader(gz)
		for {
			hd, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("the archive is damaged: %w", err)
			}
			if hd.Typeflag == tar.TypeReg {
				if err := keep(hd.Name, tr); err != nil {
					return nil, err
				}
			}
		}
	}
	var comps []string
	for _, c := range []string{Client, Server, Hub} {
		if found[c] {
			comps = append(comps, c)
		}
	}
	return comps, nil
}
