package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/concord-chat/concord/internal/installer"
	"github.com/concord-chat/concord/internal/protocol"
)

// fakeRelease serves tag with this platform's archive holding a
// concord-server that is the shell script body.
func fakeRelease(t *testing.T, tag, script string, withSum bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake server program is a shell script")
	}
	src := filepath.Join(t.TempDir(), "release.tar.gz")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	for name, body := range map[string]string{"concord-server": script, "concord-hub": "#!/bin/sh\n"} {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	f.Close()

	name := installer.Detect().ArchiveName()
	sum := ""
	if withSum {
		sum = "fake-but-present"
	}
	prevFetch, prevDownload := fetchRelease, downloadAsset
	t.Cleanup(func() { fetchRelease, downloadAsset = prevFetch, prevDownload })
	fetchRelease = func(ctx context.Context, want string) (*installer.Release, error) {
		return &installer.Release{Tag: tag, Assets: map[string]installer.Asset{name: {Name: name, Size: 1234, SHA256: sum}}}, nil
	}
	downloadAsset = func(ctx context.Context, a installer.Asset, dir string, _ func(int64, int64)) (string, error) {
		b, err := os.ReadFile(src)
		if err != nil {
			return "", err
		}
		out := filepath.Join(dir, a.Name)
		return out, os.WriteFile(out, b, 0o644)
	}
}

// running is a fake installed server program.
func running(t *testing.T) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "concord-server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\necho old\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func noProgress(string) {}

func TestInstallReleaseSwapsTheProgram(t *testing.T) {
	fakeRelease(t, "v0.1.9", "#!/bin/sh\necho new\n", true)
	exe := running(t)
	var steps []string
	tag, err := installRelease(context.Background(), exe, "0.1.8", "", func(s string) { steps = append(steps, s) })
	if err != nil || tag != "v0.1.9" {
		t.Fatalf("installRelease = %q, %v", tag, err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "#!/bin/sh\necho new\n" {
		t.Errorf("program is now %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "#!/bin/sh\necho old\n" {
		t.Errorf("the old program wasn't kept: %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm()&0o100 == 0 {
		t.Error("the new program isn't executable")
	}
	if len(steps) < 3 {
		t.Errorf("progress: %q", steps)
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".concord-update-*"))
	if len(leftovers) != 0 {
		t.Errorf("work folder left behind: %v", leftovers)
	}
}

func TestInstallReleaseRefuses(t *testing.T) {
	cases := []struct {
		name, tag, current, script string
		sum                        bool
	}{
		{"no checksum", "v0.1.9", "0.1.8", "#!/bin/sh\n", false},
		{"not newer", "v0.1.8", "0.1.8", "#!/bin/sh\n", true},
		{"older", "v0.1.7", "0.1.8", "#!/bin/sh\n", true},
		{"doesn't run here", "v0.1.9", "0.1.8", "#!/bin/sh\nexit 3\n", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fakeRelease(t, c.tag, c.script, c.sum)
			exe := running(t)
			if _, err := installRelease(context.Background(), exe, c.current, "", noProgress); err == nil {
				t.Fatal("installed anyway")
			}
			if b, _ := os.ReadFile(exe); string(b) != "#!/bin/sh\necho old\n" {
				t.Errorf("the running program was touched: %q", b)
			}
			if _, err := os.Stat(exe + ".old"); err == nil {
				t.Error("moved aside anyway")
			}
		})
	}
}

// A build from source isn't updated from releases; a release build is.
func TestUpdateHow(t *testing.T) {
	t.Setenv("INVOCATION_ID", "")
	if fileExists("/.dockerenv") || fileExists("/run/.containerenv") {
		t.Skip("running in a container")
	}
	s := &Server{version: "dev"}
	if how, _ := s.updateHow(); how != protocol.UpdateSource {
		t.Errorf("dev build: %q", how)
	}
	s.version = "0.1.8"
	if how, note := s.updateHow(); how != protocol.UpdateInPlace {
		t.Errorf("release build: %q (%s)", how, note) // the test binary's folder is writable
	}
	s.dashboardDisplay = dashboardDisplayFull
	if how, _ := s.updateHow(); how != protocol.UpdateNone {
		t.Errorf("--dashboard: %q", how)
	}
}
