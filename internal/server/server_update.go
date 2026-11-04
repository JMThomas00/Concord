package server

// Server Settings › About › Update: install a release over this program and
// restart into it (OpServerAdmin "update").

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/concord-chat/concord/internal/installer"
	"github.com/concord-chat/concord/internal/protocol"
)

// The release source, replaced in tests.
var (
	fetchRelease  = installer.FetchRelease
	downloadAsset = installer.Download
)

// dockerUpdateCommand is how a Docker server is updated: the image is
// built from the checkout, so a new release means a new build.
const dockerUpdateCommand = "In the folder with docker-compose.yml: git pull && docker compose build && docker compose up -d"

// updateHow says how this server takes a new release, and why not when it
// can't: Docker replaces its image, a build from source is rebuilt, and
// anything else needs a folder it can write to and a restart.
func (s *Server) updateHow() (how, note string) {
	if runsAs() == protocol.RunsAsDocker {
		return protocol.UpdateDocker, dockerUpdateCommand
	}
	if _, _, ok := installer.ParseVersion(s.version); !ok {
		return protocol.UpdateSource, "Built from source: pull and rebuild it"
	}
	if ok, why := s.canRestart(); !ok {
		return protocol.UpdateNone, why
	}
	if startedFrom == "" {
		return protocol.UpdateNone, "Can't tell where this program is"
	}
	dir := filepath.Dir(startedFrom)
	f, err := os.CreateTemp(dir, ".concord-write-check-*")
	if err != nil {
		return protocol.UpdateNone, "Can't write to " + dir + ": update it with concord-install --update"
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return protocol.UpdateInPlace, ""
}

// updateServer installs release tag ("" is the latest) and restarts into
// it. progress reports each step.
func (s *Server) updateServer(tag string, progress func(string)) (string, error) {
	if how, note := s.updateHow(); how != protocol.UpdateInPlace {
		return "", fmt.Errorf("%s", note)
	}
	if !s.admin.updateMu.TryLock() {
		return "", fmt.Errorf("an update is already running")
	}
	defer s.admin.updateMu.Unlock()
	s.admin.updating.Store(true)
	defer s.admin.updating.Store(false)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	newTag, err := installRelease(ctx, startedFrom, s.version, tag, progress)
	if err != nil {
		return "", err
	}
	if err := s.requestRestart(); err != nil {
		return "", fmt.Errorf("installed %s, but couldn't restart (%v): restart the server to finish", newTag, err)
	}
	return "Updated to " + newTag + ": restarting, everyone reconnects by themselves", nil
}

// installRelease puts release tag's server program at exe, keeping the old
// one beside it as <exe>.old. The download must match a published checksum,
// and the new program must run here before it replaces anything.
func installRelease(ctx context.Context, exe, current, tag string, progress func(string)) (string, error) {
	progress("Looking up the release…")
	rel, err := fetchRelease(ctx, tag)
	if err != nil {
		return "", err
	}
	if !installer.NewerVersion(current, rel.Tag) {
		return "", fmt.Errorf("%s isn't newer than this server (%s)", rel.Tag, current)
	}
	plat := installer.Detect()
	asset, ok := rel.Assets[plat.ArchiveName()]
	if !ok {
		return "", fmt.Errorf("%s has nothing for %s", rel.Tag, plat.Name())
	}
	if asset.SHA256 == "" {
		return "", fmt.Errorf("%s has no published checksum for %s, so it wasn't installed", rel.Tag, asset.Name)
	}

	dir := filepath.Dir(exe)
	work, err := os.MkdirTemp(dir, ".concord-update-")
	if err != nil {
		return "", fmt.Errorf("can't write to %s: %w", dir, err)
	}
	defer os.RemoveAll(work)

	progress(fmt.Sprintf("Downloading %s (%s)…", rel.Tag, humanBytes(asset.Size)))
	archive, err := downloadAsset(ctx, asset, work, nil)
	if err != nil {
		return "", err
	}
	progress("Checking it…")
	unpacked := filepath.Join(work, "programs")
	if _, err := installer.Unpack(archive, unpacked); err != nil {
		return "", err
	}
	fresh := filepath.Join(unpacked, filepath.Base(exe))
	if _, err := os.Stat(fresh); err != nil {
		fresh = filepath.Join(unpacked, plat.Exe("concord-server"))
		if _, err := os.Stat(fresh); err != nil {
			return "", fmt.Errorf("%s has no server program in it", asset.Name)
		}
	}
	if err := os.Chmod(fresh, 0o755); err != nil {
		return "", err
	}
	if err := runsHere(ctx, fresh); err != nil {
		return "", err
	}

	// Swap: the running program is renamed aside (which works on a running
	// Windows exe too), then the new one takes its name.
	old := exe + ".old"
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return "", fmt.Errorf("couldn't move the old program aside: %w", err)
	}
	if err := os.Rename(fresh, exe); err != nil {
		_ = os.Rename(old, exe)
		return "", fmt.Errorf("couldn't put the new program in place: %w", err)
	}
	noteInstalled(dir, rel.Tag)
	Logger.Info("Installed a release", "tag", rel.Tag, "program", exe, "old", old)
	return rel.Tag, nil
}

// runsHere runs the new program with --help, which every release answers
// with exit 0, so a program for the wrong system or a broken download
// never replaces a working one.
func runsHere(ctx context.Context, path string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--help")
	cmd.Dir = filepath.Dir(path)
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		return fmt.Errorf("the new program doesn't run here (%v) %s", err, msg)
	}
	return nil
}

// noteInstalled updates concord-install's record, when it installed this
// server, so the client's Updates page shows the new version.
func noteInstalled(dir, tag string) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	r := installer.LoadRecord(home)
	e, ok := r.Components[installer.Server]
	if !ok || !sameDir(e.Dir, dir) {
		return
	}
	e.Version = tag
	r.Components[installer.Server] = e
	_ = r.Save(home)
}

func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
}
