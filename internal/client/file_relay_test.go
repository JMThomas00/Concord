package client

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// Tor mode: a file goes through the server in chunks, both when the
// downloader is on Tor (it asks for a relay) and when only the sender is
// (it answers a direct request with use_relay).
func TestFileRelayThroughTheServer(t *testing.T) {
	if testing.Short() {
		t.Skip("wire-level test")
	}
	httpAddr := startFileTransferTestServer(t)
	sender := newTestFileClient(t, httpAddr, "relay-sender", t.TempDir())
	receiver := newTestFileClient(t, httpAddr, "relay-receiver", t.TempDir())

	content := make([]byte, 700*1024+123) // many chunks, past the window, a short last one
	rand.Read(content)
	sum := sha256.Sum256(content)
	hash := hex.EncodeToString(sum[:])
	src := filepath.Join(t.TempDir(), "photo.bin")
	os.WriteFile(src, content, 0o644)

	for i, c := range []struct {
		name          string
		senderOnTor   bool
		receiverRelay bool
	}{
		{"the downloader is on Tor", false, true},
		{"only the sender is on Tor", true, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			onTor := c.senderOnTor
			sender.engine.SetRelayOnly(func() bool { return onTor })
			att := uuid.New()
			sender.engine.RegisterSharedFile(att, src)
			dest := filepath.Join(t.TempDir(), "got.bin")
			if c.receiverRelay {
				receiver.engine.RequestRelayDownload(att, sender.userID, "photo.bin", int64(len(content)), hash, dest)
			} else {
				receiver.engine.RequestDownload(att, sender.userID, "photo.bin", int64(len(content)), hash, dest)
			}
			done := receiver.waitForTransferDone(30*time.Second, att)
			if done.Err != nil {
				t.Fatalf("run %d: %v", i, done.Err)
			}
			got, _ := os.ReadFile(done.DestPath)
			if !bytes.Equal(got, content) {
				t.Fatalf("got %d bytes, want %d (same: %v)", len(got), len(content), bytes.Equal(got, content))
			}
			if sd := sender.waitForTransferDone(30*time.Second, att); sd.Err != nil {
				t.Fatalf("sender: %v", sd.Err)
			}
		})
	}

	// A file the sender doesn't have: refused, not hung.
	att := uuid.New()
	receiver.engine.RequestRelayDownload(att, sender.userID, "gone.bin", 10, hash, filepath.Join(t.TempDir(), "x"))
	if done := receiver.waitForTransferDone(10*time.Second, att); done.Err == nil {
		t.Fatal("a missing file downloaded")
	}

	// An empty file.
	empty := filepath.Join(t.TempDir(), "empty.txt")
	os.WriteFile(empty, nil, 0o644)
	e := sha256.Sum256(nil)
	att = uuid.New()
	sender.engine.RegisterSharedFile(att, empty)
	receiver.engine.RequestRelayDownload(att, sender.userID, "empty.txt", 0, hex.EncodeToString(e[:]), filepath.Join(t.TempDir(), "empty.txt"))
	if done := receiver.waitForTransferDone(10*time.Second, att); done.Err != nil {
		t.Fatalf("empty file: %v", done.Err)
	}
}
