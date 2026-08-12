// Peer-to-peer file attachment transfer.
//
// Transport: github.com/pion/webrtc/v3 (already a dependency of voice_engine.go;
// pure-Go, no CGO). Unlike voice's audio DataChannel (Ordered:false, unreliable
// -- UDP-like, fine for dropped audio packets), file transfer uses a reliable,
// ordered channel so bytes arrive complete and in sequence.
//
// Unlike voice's symmetric mesh (every peer both sends and receives audio, so
// offer/answer roles are resolved by a UUID tie-break), a file transfer is
// inherently one-directional: the client that has the file always creates the
// offer and the DataChannel; the downloading client always answers and simply
// receives. No tie-break is needed.
//
// Signaling mirrors OpVoiceSignal's dumb-relay shape exactly (see
// HandleFileTransferSignal, internal/server/handlers.go) -- the server only
// ever sees small SDP/ICE/request messages, never file bytes.

package client

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
)

const (
	fileChunkSize                  = 16 * 1024        // bytes per DataChannel message
	fileBufferedAmountLowThreshold = 512 * 1024        // resume sending below this
	fileBufferedAmountPauseAt      = 4 * 1024 * 1024   // pause sending above this
)

// FileTransferDirection distinguishes which side of a transfer this client is on.
type FileTransferDirection int

const (
	TransferSending FileTransferDirection = iota
	TransferReceiving
)

// FileTransferSignalOut is queued by the FileTransferEngine when it needs to
// send a signaling message (request/accept/reject/offer/answer/candidate) to
// a remote peer via the server relay.
type FileTransferSignalOut struct {
	TargetUserID uuid.UUID
	AttachmentID uuid.UUID
	Type         string // "request", "accept", "reject", "offer", "answer", "candidate"
	SDP          string
	Candidate    json.RawMessage
}

// FileTransferProgressMsg reports incremental progress for the bubbletea Update loop.
type FileTransferProgressMsg struct {
	AttachmentID uuid.UUID
	PeerUserID   uuid.UUID
	Direction    FileTransferDirection
	BytesDone    int64
	TotalBytes   int64
}

// FileTransferDoneMsg reports that a transfer finished, successfully or not.
type FileTransferDoneMsg struct {
	AttachmentID uuid.UUID
	PeerUserID   uuid.UUID
	Direction    FileTransferDirection
	Filename     string
	DestPath     string // set on a successful receive
	Err          error
}

// FileTransferOfflineMsg reports that a download was requested but the file's
// sender has no active connection to relay the request to.
type FileTransferOfflineMsg struct {
	AttachmentID uuid.UUID
}

// FileTransferRejectedMsg reports that the sender declined the request (most
// commonly: this client restarted and no longer has the source file registered).
type FileTransferRejectedMsg struct {
	AttachmentID uuid.UUID
}

func transferKey(attachmentID, peerUserID uuid.UUID) string {
	return attachmentID.String() + ":" + peerUserID.String()
}

// filePeer holds the WebRTC connection for one in-progress transfer.
type filePeer struct {
	pc                *webrtc.PeerConnection
	mu                sync.Mutex
	pendingCandidates []webrtc.ICECandidateInit
	remoteDescSet     bool
}

func (p *filePeer) bufferCandidate(c webrtc.ICECandidateInit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.remoteDescSet {
		p.pendingCandidates = append(p.pendingCandidates, c)
		return
	}
	if err := p.pc.AddICECandidate(c); err != nil {
		log.Printf("filetransfer: AddICECandidate: %v", err)
	}
}

func (p *filePeer) flushCandidates() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remoteDescSet = true
	for _, c := range p.pendingCandidates {
		if err := p.pc.AddICECandidate(c); err != nil {
			log.Printf("filetransfer: AddICECandidate(flush): %v", err)
		}
	}
	p.pendingCandidates = nil
}

// activeReceive tracks an in-progress download, written to a temp file and
// renamed into place only once its SHA-256 has been verified.
type activeReceive struct {
	filename   string
	tempPath   string
	finalPath  string
	destFile   *os.File
	hasher     hash.Hash
	expectSize int64
	expectHash string
	received   int64
}

// FileTransferEngine manages peer-to-peer WebRTC connections for sending and
// receiving file attachments. One engine is shared for the lifetime of the
// app; each transfer gets its own short-lived PeerConnection.
type FileTransferEngine struct {
	localUserID uuid.UUID
	api         *webrtc.API
	downloadDir string

	sigOut   chan<- FileTransferSignalOut
	eventOut chan<- interface{}

	// sharedFiles maps an attachment ID to the local path of the file this
	// client originally shared, so it can serve download requests. Loaded
	// from ~/.concord/shared_files.json at startup and updated by RegisterSharedFile.
	sharedFiles   map[uuid.UUID]string
	sharedFilesMu sync.RWMutex

	peers   map[string]*filePeer // key: transferKey(attachmentID, peerUserID)
	peersMu sync.RWMutex

	receives   map[string]*activeReceive
	receivesMu sync.Mutex

	quit    chan struct{}
	mu      sync.Mutex
	stopped bool
}

// NewFileTransferEngine constructs an engine. downloadDir is where completed
// downloads are saved (created if it doesn't exist).
func NewFileTransferEngine(
	localUserID uuid.UUID,
	downloadDir string,
	sharedFiles map[uuid.UUID]string,
	sigOut chan<- FileTransferSignalOut,
	eventOut chan<- interface{},
) *FileTransferEngine {
	se := webrtc.SettingEngine{}
	me := &webrtc.MediaEngine{}
	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(me),
	)
	if sharedFiles == nil {
		sharedFiles = make(map[uuid.UUID]string)
	}
	return &FileTransferEngine{
		localUserID: localUserID,
		api:         api,
		downloadDir: downloadDir,
		sharedFiles: sharedFiles,
		sigOut:      sigOut,
		eventOut:    eventOut,
		peers:       make(map[string]*filePeer),
		receives:    make(map[string]*activeReceive),
		quit:        make(chan struct{}),
	}
}

// Stop closes all in-progress peer connections. Safe to call multiple times.
func (e *FileTransferEngine) Stop() {
	e.mu.Lock()
	if e.stopped {
		e.mu.Unlock()
		return
	}
	e.stopped = true
	close(e.quit)
	e.mu.Unlock()

	e.peersMu.Lock()
	for _, p := range e.peers {
		_ = p.pc.Close()
	}
	e.peers = make(map[string]*filePeer)
	e.peersMu.Unlock()
}

// RegisterSharedFile records that this client is offering attachmentID,
// backed by localPath, so it can respond to future download requests.
func (e *FileTransferEngine) RegisterSharedFile(attachmentID uuid.UUID, localPath string) {
	e.sharedFilesMu.Lock()
	e.sharedFiles[attachmentID] = localPath
	e.sharedFilesMu.Unlock()
}

func (e *FileTransferEngine) iceServers() []webrtc.ICEServer {
	return []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
}

// ── Requesting a download (receiver side) ────────────────────────────────────

// RequestDownload asks the file's sender to start a transfer. filename/size/
// hash come from the Attachment manifest already received in chat.
func (e *FileTransferEngine) RequestDownload(attachmentID, senderUserID uuid.UUID, filename string, size int64, contentHash string) {
	key := transferKey(attachmentID, senderUserID)
	e.receivesMu.Lock()
	e.receives[key] = &activeReceive{
		filename:   filename,
		expectSize: size,
		expectHash: contentHash,
	}
	e.receivesMu.Unlock()

	e.sigOut <- FileTransferSignalOut{
		TargetUserID: senderUserID,
		AttachmentID: attachmentID,
		Type:         "request",
	}
}

// ── Signal routing ────────────────────────────────────────────────────────────

// HandleSignal routes an incoming relayed signal to the appropriate step.
func (e *FileTransferEngine) HandleSignal(fromUserID, attachmentID uuid.UUID, sigType, sdp string, candidateJSON []byte) {
	switch sigType {
	case "request":
		go e.handleRequest(fromUserID, attachmentID)
	case "offline":
		select {
		case e.eventOut <- FileTransferOfflineMsg{AttachmentID: attachmentID}:
		default:
		}
	case "reject":
		e.receivesMu.Lock()
		delete(e.receives, transferKey(attachmentID, fromUserID))
		e.receivesMu.Unlock()
		select {
		case e.eventOut <- FileTransferRejectedMsg{AttachmentID: attachmentID}:
		default:
		}
	case "offer":
		go e.handleOffer(fromUserID, attachmentID, sdp)
	case "answer":
		go e.handleAnswer(fromUserID, attachmentID, sdp)
	case "candidate":
		go e.handleCandidate(fromUserID, attachmentID, candidateJSON)
	}
}

// handleRequest is invoked on the sending side when someone asks to download
// an attachment. v1 auto-accepts if the file is still registered locally.
func (e *FileTransferEngine) handleRequest(requesterUserID, attachmentID uuid.UUID) {
	e.sharedFilesMu.RLock()
	localPath, ok := e.sharedFiles[attachmentID]
	e.sharedFilesMu.RUnlock()
	if !ok {
		e.sigOut <- FileTransferSignalOut{TargetUserID: requesterUserID, AttachmentID: attachmentID, Type: "reject"}
		return
	}
	if _, err := os.Stat(localPath); err != nil {
		e.sigOut <- FileTransferSignalOut{TargetUserID: requesterUserID, AttachmentID: attachmentID, Type: "reject"}
		return
	}

	peer := e.newPeer(attachmentID, requesterUserID)
	if peer == nil {
		e.sigOut <- FileTransferSignalOut{TargetUserID: requesterUserID, AttachmentID: attachmentID, Type: "reject"}
		return
	}
	key := transferKey(attachmentID, requesterUserID)
	e.peersMu.Lock()
	e.peers[key] = peer
	e.peersMu.Unlock()

	e.sendOffer(peer, attachmentID, requesterUserID, localPath)
}

func (e *FileTransferEngine) newPeer(attachmentID, peerUserID uuid.UUID) *filePeer {
	pc, err := e.api.NewPeerConnection(webrtc.Configuration{ICEServers: e.iceServers()})
	if err != nil {
		log.Printf("filetransfer: NewPeerConnection(%s): %v", peerUserID, err)
		return nil
	}
	peer := &filePeer{pc: pc}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		b, err := json.Marshal(c.ToJSON())
		if err != nil {
			return
		}
		e.sigOut <- FileTransferSignalOut{
			TargetUserID: peerUserID,
			AttachmentID: attachmentID,
			Type:         "candidate",
			Candidate:    b,
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		switch s {
		case webrtc.PeerConnectionStateFailed, webrtc.PeerConnectionStateClosed:
			key := transferKey(attachmentID, peerUserID)
			e.peersMu.Lock()
			delete(e.peers, key)
			e.peersMu.Unlock()
		}
	})

	// Receiving side only: the sender creates the data channel; we just accept it.
	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		e.handleIncomingDataChannel(attachmentID, peerUserID, dc)
	})

	return peer
}

func (e *FileTransferEngine) sendOffer(peer *filePeer, attachmentID, peerUserID uuid.UUID, localPath string) {
	dc, err := peer.pc.CreateDataChannel("file", &webrtc.DataChannelInit{Ordered: boolPtr(true)})
	if err != nil {
		log.Printf("filetransfer: CreateDataChannel: %v", err)
		return
	}
	dc.OnOpen(func() {
		go e.streamFile(attachmentID, peerUserID, localPath, dc)
	})

	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		log.Printf("filetransfer: CreateOffer: %v", err)
		return
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		log.Printf("filetransfer: SetLocalDescription offer: %v", err)
		return
	}
	e.sigOut <- FileTransferSignalOut{TargetUserID: peerUserID, AttachmentID: attachmentID, Type: "offer", SDP: offer.SDP}
}

func (e *FileTransferEngine) handleOffer(fromUserID, attachmentID uuid.UUID, sdp string) {
	key := transferKey(attachmentID, fromUserID)
	e.peersMu.Lock()
	peer, ok := e.peers[key]
	if !ok {
		peer = e.newPeer(attachmentID, fromUserID)
		if peer == nil {
			e.peersMu.Unlock()
			return
		}
		e.peers[key] = peer
	}
	e.peersMu.Unlock()

	if err := peer.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeOffer, SDP: sdp}); err != nil {
		log.Printf("filetransfer: SetRemoteDescription offer: %v", err)
		return
	}
	peer.flushCandidates()

	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("filetransfer: CreateAnswer: %v", err)
		return
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		log.Printf("filetransfer: SetLocalDescription answer: %v", err)
		return
	}
	e.sigOut <- FileTransferSignalOut{TargetUserID: fromUserID, AttachmentID: attachmentID, Type: "answer", SDP: answer.SDP}
}

func (e *FileTransferEngine) handleAnswer(fromUserID, attachmentID uuid.UUID, sdp string) {
	key := transferKey(attachmentID, fromUserID)
	e.peersMu.RLock()
	peer, ok := e.peers[key]
	e.peersMu.RUnlock()
	if !ok {
		return
	}
	if err := peer.pc.SetRemoteDescription(webrtc.SessionDescription{Type: webrtc.SDPTypeAnswer, SDP: sdp}); err != nil {
		log.Printf("filetransfer: SetRemoteDescription answer: %v", err)
		return
	}
	peer.flushCandidates()
}

func (e *FileTransferEngine) handleCandidate(fromUserID, attachmentID uuid.UUID, candidateJSON []byte) {
	key := transferKey(attachmentID, fromUserID)
	e.peersMu.RLock()
	peer, ok := e.peers[key]
	e.peersMu.RUnlock()
	if !ok {
		return
	}
	var init webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateJSON, &init); err != nil {
		log.Printf("filetransfer: bad ICE candidate JSON: %v", err)
		return
	}
	peer.bufferCandidate(init)
}

// ── Sending ────────────────────────────────────────────────────────────────────

func (e *FileTransferEngine) streamFile(attachmentID, peerUserID uuid.UUID, localPath string, dc *webrtc.DataChannel) {
	f, err := os.Open(localPath)
	if err != nil {
		e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferSending, Err: err})
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferSending, Err: err})
		return
	}
	total := info.Size()

	resume := make(chan struct{}, 1)
	dc.SetBufferedAmountLowThreshold(fileBufferedAmountLowThreshold)
	dc.OnBufferedAmountLow(func() {
		select {
		case resume <- struct{}{}:
		default:
		}
	})

	buf := make([]byte, fileChunkSize)
	var sent int64
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			for dc.BufferedAmount() > fileBufferedAmountPauseAt {
				select {
				case <-resume:
				case <-e.quit:
					return
				}
			}
			if sendErr := dc.Send(buf[:n]); sendErr != nil {
				e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferSending, Err: sendErr})
				return
			}
			sent += int64(n)
			select {
			case e.eventOut <- FileTransferProgressMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferSending, BytesDone: sent, TotalBytes: total}:
			default:
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferSending, Err: readErr})
			return
		}
	}

	e.emitDone(FileTransferDoneMsg{
		AttachmentID: attachmentID,
		PeerUserID:   peerUserID,
		Direction:    TransferSending,
		Filename:     filepath.Base(localPath),
	})
}

// ── Receiving ──────────────────────────────────────────────────────────────────

func (e *FileTransferEngine) handleIncomingDataChannel(attachmentID, peerUserID uuid.UUID, dc *webrtc.DataChannel) {
	key := transferKey(attachmentID, peerUserID)
	e.receivesMu.Lock()
	recv, ok := e.receives[key]
	e.receivesMu.Unlock()
	if !ok {
		return
	}

	if err := os.MkdirAll(e.downloadDir, 0755); err != nil {
		e.failReceive(attachmentID, peerUserID, key, recv, err)
		return
	}
	recv.finalPath = uniqueDownloadPath(e.downloadDir, recv.filename)
	recv.tempPath = recv.finalPath + ".part"
	f, err := os.Create(recv.tempPath)
	if err != nil {
		e.failReceive(attachmentID, peerUserID, key, recv, err)
		return
	}
	recv.destFile = f
	recv.hasher = sha256.New()

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		if msg.IsString || len(msg.Data) == 0 {
			return
		}
		n, err := recv.destFile.Write(msg.Data)
		if err != nil {
			e.failReceive(attachmentID, peerUserID, key, recv, err)
			return
		}
		recv.hasher.Write(msg.Data[:n])
		recv.received += int64(n)

		select {
		case e.eventOut <- FileTransferProgressMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferReceiving, BytesDone: recv.received, TotalBytes: recv.expectSize}:
		default:
		}

		if recv.received >= recv.expectSize {
			e.finalizeReceive(attachmentID, peerUserID, key, recv)
		}
	})
}

func (e *FileTransferEngine) finalizeReceive(attachmentID, peerUserID uuid.UUID, key string, recv *activeReceive) {
	e.receivesMu.Lock()
	delete(e.receives, key)
	e.receivesMu.Unlock()

	recv.destFile.Close()
	sum := hex.EncodeToString(recv.hasher.Sum(nil))
	if sum != recv.expectHash {
		os.Remove(recv.tempPath)
		e.emitDone(FileTransferDoneMsg{
			AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferReceiving,
			Filename: recv.filename,
			Err:      fmt.Errorf("integrity check failed: hash mismatch"),
		})
		return
	}
	if err := os.Rename(recv.tempPath, recv.finalPath); err != nil {
		e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferReceiving, Filename: recv.filename, Err: err})
		return
	}
	e.emitDone(FileTransferDoneMsg{
		AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferReceiving,
		Filename: recv.filename, DestPath: recv.finalPath,
	})
}

func (e *FileTransferEngine) failReceive(attachmentID, peerUserID uuid.UUID, key string, recv *activeReceive, err error) {
	e.receivesMu.Lock()
	delete(e.receives, key)
	e.receivesMu.Unlock()
	if recv.destFile != nil {
		recv.destFile.Close()
		os.Remove(recv.tempPath)
	}
	e.emitDone(FileTransferDoneMsg{AttachmentID: attachmentID, PeerUserID: peerUserID, Direction: TransferReceiving, Filename: recv.filename, Err: err})
}

func (e *FileTransferEngine) emitDone(msg FileTransferDoneMsg) {
	select {
	case e.eventOut <- msg:
	default:
	}
}

// uniqueDownloadPath returns dir/filename, or dir/filename (2), dir/filename (3)...
// if a file already exists at that path.
func uniqueDownloadPath(dir, filename string) string {
	candidate := filepath.Join(dir, filename)
	if _, err := os.Stat(candidate); err != nil {
		return candidate
	}
	ext := filepath.Ext(filename)
	base := filename[:len(filename)-len(ext)]
	for i := 2; ; i++ {
		candidate = filepath.Join(dir, fmt.Sprintf("%s (%d)%s", base, i, ext))
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}

func boolPtr(b bool) *bool { return &b }
