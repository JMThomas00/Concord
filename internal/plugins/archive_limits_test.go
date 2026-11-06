package plugins

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Finding 15 (security audit 2026-11-05): a plugin archive had no limit on
// what it unpacks to, so a small zip could fill the disk.
func TestArchivesCantUnpackTooMuch(t *testing.T) {
	old := maxUnpackedBytes
	maxUnpackedBytes = 1 << 20
	t.Cleanup(func() { maxUnpackedBytes = old })

	dir := t.TempDir()
	write := func(name string, files map[string]int) string {
		p := filepath.Join(dir, name)
		f, _ := os.Create(p)
		zw := zip.NewWriter(f)
		for n, size := range files {
			w, _ := zw.Create(n)
			w.Write(bytes.Repeat([]byte{0}, size))
		}
		zw.Close()
		f.Close()
		return p
	}

	bomb := write("bomb.zip", map[string]int{"plugin.toml": 10, "a": 600 << 10, "b": 600 << 10})
	if fi, _ := os.Stat(bomb); fi.Size() > 64<<10 {
		t.Fatalf("the test zip isn't small: %d bytes", fi.Size())
	}
	err := extractZip(bomb, filepath.Join(dir, "out-bomb"))
	if err == nil || !strings.Contains(err.Error(), "more than") {
		t.Fatalf("1.2 MB unpacked under a 1 MB limit: %v", err)
	}

	ok := write("ok.zip", map[string]int{"plugin.toml": 10, "a": 300 << 10})
	if err := extractZip(ok, filepath.Join(dir, "out-ok")); err != nil {
		t.Fatalf("a small plugin was refused: %v", err)
	}
}
