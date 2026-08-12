package client

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
)

func TestHumanFileSize(t *testing.T) {
	cases := []struct {
		n    int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{5 * 1024 * 1024, "5.0 MiB"},
	}
	for _, c := range cases {
		if got := humanFileSize(c.n); got != c.want {
			t.Errorf("humanFileSize(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestTransferKey(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	k1 := transferKey(a, b)
	k2 := transferKey(a, b)
	if k1 != k2 {
		t.Errorf("transferKey should be deterministic: %q != %q", k1, k2)
	}
	if transferKey(a, b) == transferKey(b, a) {
		t.Errorf("transferKey should be order-sensitive (different peers/attachments shouldn't collide)")
	}
}

func TestUniqueDownloadPath(t *testing.T) {
	dir := t.TempDir()

	// No conflict: returns the plain path.
	p1 := uniqueDownloadPath(dir, "photo.png")
	if p1 != filepath.Join(dir, "photo.png") {
		t.Errorf("expected plain path with no conflict, got %q", p1)
	}

	// Create a file there, then ask again -- should get a " (2)" suffix.
	if err := os.WriteFile(p1, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create conflicting file: %v", err)
	}
	p2 := uniqueDownloadPath(dir, "photo.png")
	want := filepath.Join(dir, "photo (2).png")
	if p2 != want {
		t.Errorf("expected deduped path %q, got %q", want, p2)
	}

	// Fill (2) as well -- should skip to (3).
	if err := os.WriteFile(p2, []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create second conflicting file: %v", err)
	}
	p3 := uniqueDownloadPath(dir, "photo.png")
	want3 := filepath.Join(dir, "photo (3).png")
	if p3 != want3 {
		t.Errorf("expected deduped path %q, got %q", want3, p3)
	}
}
