package client

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

// TestEmitDoneDeliversEvenWhenEventChannelIsFull is a regression test for a real
// bug: emitDone used to share the same non-blocking "drop if the channel isn't
// immediately ready" pattern as progress ticks. Dropping a progress update is
// harmless (another follows almost immediately), but dropping the one-shot done
// event permanently sticks the UI's status bar at whatever percentage last made
// it through -- which is exactly what happened on a fast transfer where the
// event buffer backed up with progress messages faster than the UI could drain
// it. emitDone must block until there's room, not silently drop.
func TestEmitDoneDeliversEvenWhenEventChannelIsFull(t *testing.T) {
	eventOut := make(chan interface{}, 1)
	sigOut := make(chan FileTransferSignalOut, 1)
	engine := NewFileTransferEngine(uuid.New(), t.TempDir(), nil, sigOut, eventOut)

	// Saturate the buffer, mirroring a burst of progress ticks arriving faster
	// than the UI's single-message-at-a-time read loop can drain them.
	eventOut <- FileTransferProgressMsg{}

	attachmentID := uuid.New()
	done := make(chan struct{})
	go func() {
		engine.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, Filename: "test.txt"})
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("emitDone returned while the channel was still full -- it must have dropped the event")
	case <-time.After(50 * time.Millisecond):
	}

	// Drain the stale progress message, exactly like the UI's read loop
	// eventually catching up and re-subscribing.
	<-eventOut

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("emitDone never returned after the channel had room -- it's stuck")
	}

	select {
	case evt := <-eventOut:
		got, ok := evt.(FileTransferDoneMsg)
		if !ok || got.AttachmentID != attachmentID {
			t.Fatalf("expected the done event to be delivered once room freed up, got %#v", evt)
		}
	default:
		t.Fatal("done event was never actually placed on the channel")
	}
}

func TestResolveDownloadPath(t *testing.T) {
	t.Run("explicit destPath is used as-is and its parent dir is created", func(t *testing.T) {
		dir := t.TempDir()
		dest := filepath.Join(dir, "nested", "picked-by-dialog.png")

		got, err := resolveDownloadPath(dest, filepath.Join(dir, "downloads"), "original-name.png")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != dest {
			t.Errorf("expected the exact chosen path %q, got %q", dest, got)
		}
		if _, statErr := os.Stat(filepath.Dir(dest)); statErr != nil {
			t.Errorf("expected parent directory to be created: %v", statErr)
		}
	})

	t.Run("empty destPath falls back to downloadDir with collision-safe naming", func(t *testing.T) {
		dir := t.TempDir()
		downloadDir := filepath.Join(dir, "downloads")

		got, err := resolveDownloadPath("", downloadDir, "file.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := filepath.Join(downloadDir, "file.txt")
		if got != want {
			t.Errorf("expected %q, got %q", want, got)
		}

		// Occupy that path, then confirm the fallback path disambiguates
		// exactly like uniqueDownloadPath already does on its own.
		if err := os.WriteFile(got, []byte("existing"), 0644); err != nil {
			t.Fatalf("failed to seed existing file: %v", err)
		}
		got2, err := resolveDownloadPath("", downloadDir, "file.txt")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want2 := filepath.Join(downloadDir, "file (2).txt")
		if got2 != want2 {
			t.Errorf("expected the collision-safe name %q, got %q", want2, got2)
		}
	})
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
