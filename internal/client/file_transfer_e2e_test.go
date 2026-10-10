package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/server"
	"github.com/google/uuid"
)

func init() {
	// internal/server's package-level component loggers (AuthLog, DBLog, ...)
	// are normally set up by cmd/server/main.go's InitLogger call; its own
	// _test.go files have their own init() for this, invisible from here
	// since this test lives in a different package. Without it, server.New()
	// panics on its first Logger.Info call.
	server.InitLogger(os.Stderr, log.FatalLevel)
}

// startFileTransferTestServer starts a real Concord server (the exact same
// production code the real binary runs) on a random localhost port, using
// only its exported API -- internal/server's own test helpers are unexported
// and live in _test.go files, invisible outside that package.
func startFileTransferTestServer(t *testing.T) string {
	t.Helper()
	// Deliberately not t.TempDir(): internal/server has no exported
	// Close/Shutdown, so the sqlite file handle is still open (and locked,
	// on Windows) when the test ends. t.TempDir()'s automatic RemoveAll
	// would fail the test over that leftover handle. Best-effort cleanup
	// instead -- a stray temp dir is a fine trade for not touching Concord
	// core just for test teardown.
	tmpDir, err := os.MkdirTemp("", "concord-filetransfer-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmpDir) })

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to reserve a port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	config := server.DefaultConfig()
	config.Host = "127.0.0.1"
	config.Port = port
	config.DatabasePath = filepath.Join(tmpDir, "concord.db")
	config.PluginsDir = filepath.Join(tmpDir, "Plugins")
	config.ServerName = "FileTransfer E2E Test"
	config.MessagePruning.Enabled = false

	srv, err := server.New(config)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	go func() { _ = srv.Run() }()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
			conn.Close()
			return addr
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not start listening in time")
	return ""
}

// testFileClient drives a real Connection (the same type the TUI client
// uses) plus a real FileTransferEngine, wired together the same way app.go
// wires them -- just without bubbletea in the loop.
type testFileClient struct {
	t      *testing.T
	conn   *Connection
	userID uuid.UUID

	engine   *FileTransferEngine
	sigOut   chan FileTransferSignalOut
	eventOut chan interface{}

	ready        chan struct{}
	readyPayload protocol.ReadyPayload
	all          chan *protocol.Message // every OpDispatch, for ad hoc waits
}

func newTestFileClient(t *testing.T, httpAddr, username, downloadDir string) *testFileClient {
	t.Helper()
	conn := NewConnection(httpAddr)
	user, token, err := conn.Register(username, username+"@filetransfer.test", "password12345")
	if err != nil {
		t.Fatalf("register(%s): %v", username, err)
	}

	tc := &testFileClient{
		t:      t,
		conn:   conn,
		userID: user.ID,
		ready:  make(chan struct{}),
		all:    make(chan *protocol.Message, 128),
	}
	tc.sigOut = make(chan FileTransferSignalOut, 32)
	tc.eventOut = make(chan interface{}, 32)
	tc.engine = NewFileTransferEngine(user.ID, downloadDir, nil, tc.sigOut, tc.eventOut)

	readyClosed := false
	conn.SetHandlers(func(msg *protocol.Message) {
		if msg.Op == protocol.OpReady && !readyClosed {
			readyClosed = true
			_ = json.Unmarshal(msg.Data, &tc.readyPayload)
			close(tc.ready)
		}
		if msg.Op != protocol.OpDispatch {
			return
		}
		// Route file transfer signals into the engine, exactly as
		// app.go's EventFileTransferSignal dispatch case does.
		if msg.Type == protocol.EventFileTransferSignal {
			var sig protocol.FileTransferSignalRelayPayload
			if err := json.Unmarshal(msg.Data, &sig); err == nil {
				tc.engine.HandleSignal(sig.SourceUserID, sig.AttachmentID, sig.Type, sig.SDP, sig.Candidate)
			}
		}
		select {
		case tc.all <- msg:
		default:
		}
	}, nil, nil, nil)

	if err := conn.Connect(); err != nil {
		t.Fatalf("connect(%s): %v", username, err)
	}
	if err := conn.Identify(token); err != nil {
		t.Fatalf("identify(%s): %v", username, err)
	}
	select {
	case <-tc.ready:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: timed out waiting for READY", username)
	}

	// Forward outbound file transfer signals over the real WebSocket
	// connection, exactly as app.go's FileTransferSignalOut case does.
	go func() {
		for sig := range tc.sigOut {
			payload := &protocol.FileTransferSignalPayload{
				TargetUserID: sig.TargetUserID,
				AttachmentID: sig.AttachmentID,
				Type:         sig.Type,
				SDP:          sig.SDP,
				Candidate:    sig.Candidate,
			}
			wsMsg, err := protocol.NewMessage(protocol.OpFileTransferSignal, payload)
			if err != nil {
				continue
			}
			_ = conn.Send(wsMsg)
		}
	}()

	t.Cleanup(func() {
		tc.engine.Stop()
		conn.Disconnect()
	})

	return tc
}

func (tc *testFileClient) waitForDispatch(timeout time.Duration, match func(*protocol.Message) bool) *protocol.Message {
	tc.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case msg := <-tc.all:
			if match(msg) {
				return msg
			}
		case <-deadline:
			tc.t.Fatal("timed out waiting for dispatch message")
			return nil
		}
	}
}

func (tc *testFileClient) waitForTransferDone(timeout time.Duration, attachmentID uuid.UUID) FileTransferDoneMsg {
	tc.t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case evt := <-tc.eventOut:
			if done, ok := evt.(FileTransferDoneMsg); ok && done.AttachmentID == attachmentID {
				return done
			}
		case <-deadline:
			tc.t.Fatal("timed out waiting for file transfer completion")
			return FileTransferDoneMsg{}
		}
	}
}

// TestFileTransferPeerToPeerEndToEnd is the live wire-level pass for Part 5:
// two real Connections against a real running server, a real attachment
// manifest sent over OpSendMessage, and a real WebRTC peer connection
// negotiated purely through the production signaling relay -- the actual
// file bytes never touch the server, only these two client processes.
func TestFileTransferPeerToPeerEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping wire-level P2P integration test in short mode")
	}

	httpAddr := startFileTransferTestServer(t)

	// Alice registers first, so she's already the server owner (see
	// first_admin_test.go in internal/server) -- hasChannelPermission's
	// server.OwnerID short-circuit means she can attach files without any
	// extra role setup.
	sender := newTestFileClient(t, httpAddr, "alice-sender", t.TempDir())
	if len(sender.readyPayload.Servers) == 0 {
		t.Fatal("expected sender to be auto-joined to the default server")
	}
	serverID := sender.readyPayload.Servers[0].ID

	channelID := createTestChannel(t, sender, serverID)

	// Bob connects (and gets auto-joined to every existing channel, including
	// the one just created above) only after the channel exists -- a client
	// only receives broadcasts for channels it was joined to at identify
	// time (see the "CRITICAL FIX: Auto-join all channels" comment in
	// internal/server/client.go), so creating the channel first here mirrors
	// a real second member joining a server that already has channels.
	receiver := newTestFileClient(t, httpAddr, "bob-receiver", t.TempDir())

	content := []byte("hello from the Concord peer-to-peer file transfer end-to-end test -- these bytes travel client to client, never touching the server.")
	sum := sha256.Sum256(content)
	contentHash := hex.EncodeToString(sum[:])

	srcPath := filepath.Join(t.TempDir(), "hello.txt")
	if err := os.WriteFile(srcPath, content, 0644); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	attachmentID := uuid.New()
	sendPayload := &protocol.SendMessagePayload{
		ChannelID: channelID,
		Content:   "here's a file",
		Nonce:     uuid.New().String(),
		Attachments: []models.Attachment{{
			ID:          attachmentID,
			Filename:    "hello.txt",
			Size:        int64(len(content)),
			ContentHash: contentHash,
			SenderID:    sender.userID,
		}},
	}
	wsMsg, err := protocol.NewMessage(protocol.OpSendMessage, sendPayload)
	if err != nil {
		t.Fatalf("failed to build send message: %v", err)
	}
	if err := sender.conn.Send(wsMsg); err != nil {
		t.Fatalf("failed to send attachment message: %v", err)
	}

	// Confirm the receiver actually saw the attachment manifest over the wire.
	created := receiver.waitForDispatch(5*time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.EventMessageCreate
	})
	var createPayload protocol.MessageCreatePayload
	if err := json.Unmarshal(created.Data, &createPayload); err != nil {
		t.Fatalf("failed to decode message create payload: %v", err)
	}
	if len(createPayload.Message.Attachments) != 1 || createPayload.Message.Attachments[0].ID != attachmentID {
		t.Fatalf("expected the attachment manifest on the received message, got %+v", createPayload.Message.Attachments)
	}

	// Mirror what handleAttach does after a successful send: remember where
	// the source file lives so the engine can serve download requests.
	sender.engine.RegisterSharedFile(attachmentID, srcPath)

	// Bob requests the real peer-to-peer download.
	receiver.engine.RequestDownload(attachmentID, sender.userID, "hello.txt", int64(len(content)), contentHash, "")

	receiverDone := receiver.waitForTransferDone(20*time.Second, attachmentID)
	if receiverDone.Err != nil {
		t.Fatalf("download failed: %v", receiverDone.Err)
	}
	if receiverDone.Direction != TransferReceiving {
		t.Fatalf("expected TransferReceiving on the downloader's side, got %v", receiverDone.Direction)
	}

	gotBytes, err := os.ReadFile(receiverDone.DestPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(gotBytes) != string(content) {
		t.Fatalf("downloaded content mismatch:\ngot:  %q\nwant: %q", gotBytes, content)
	}

	senderDone := sender.waitForTransferDone(20*time.Second, attachmentID)
	if senderDone.Err != nil {
		t.Fatalf("expected a clean send completion, got error: %v", senderDone.Err)
	}
	if senderDone.Direction != TransferSending {
		t.Fatalf("expected TransferSending on the sharer's side, got %v", senderDone.Direction)
	}
}

// createTestChannel creates a real text channel over the wire (OpChannelCreate)
// and waits for the resulting CHANNEL_CREATE dispatch, returning its ID.
func createTestChannel(t *testing.T, tc *testFileClient, serverID uuid.UUID) uuid.UUID {
	t.Helper()
	req := &protocol.ChannelCreateRequest{
		ServerID: serverID,
		Name:     "file-transfer-test",
		Type:     models.ChannelTypeText,
	}
	wsMsg, err := protocol.NewMessage(protocol.OpChannelCreate, req)
	if err != nil {
		t.Fatalf("failed to build channel create message: %v", err)
	}
	if err := tc.conn.Send(wsMsg); err != nil {
		t.Fatalf("failed to send channel create: %v", err)
	}
	created := tc.waitForDispatch(5*time.Second, func(m *protocol.Message) bool {
		return m.Type == protocol.EventChannelCreate
	})
	var payload protocol.ChannelCreatePayload
	if err := json.Unmarshal(created.Data, &payload); err != nil {
		t.Fatalf("failed to decode channel create payload: %v", err)
	}
	return payload.Channel.ID
}
