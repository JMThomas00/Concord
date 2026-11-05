package client

// Files relayed through the server (Tor mode, Privacy Pack 1). A normal
// transfer connects the two computers directly over WebRTC, which shows
// each one's address to the other; over Tor that would undo the point. So
// when either side is on Tor, the file goes through the server instead, in
// chunks over the WebSocket each already has, and the server keeps none of
// it (internal/server HandleFileTransferSignal passes chunks along).
//
//	receiver → relay_request            (or the sender answers a normal
//	sender   → relay_chunk 1..n          "request" with use_relay)
//	receiver → relay_ack per chunk       (the sender keeps relayWindow out)
//	sender   → relay_end
//	either   → relay_error (reason in SDP)
//
// The receiver checks the SHA-256 from the message, as for direct
// transfers.

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

const (
	relayChunkSize = 48 * 1024 // under protocol.RelayChunkMax
	relayWindow    = 8         // chunks sent before waiting for acks
	relayStall     = 2 * time.Minute
)

// isRelaySignal reports whether a file signal belongs to a relayed transfer.
func isRelaySignal(t string) bool {
	return strings.HasPrefix(t, "relay_") || t == "use_relay"
}

// relaySend is a file this client is sending through the server.
type relaySend struct {
	acks chan int64
	stop chan struct{}
	once bool
}

// SetRelayOnly tells the engine whether this client must relay (it's on
// Tor): it then answers direct requests with use_relay.
func (e *FileTransferEngine) SetRelayOnly(f func() bool) {
	e.mu.Lock()
	e.relayOnly = f
	e.mu.Unlock()
}

func (e *FileTransferEngine) mustRelay() bool {
	e.mu.Lock()
	f := e.relayOnly
	e.mu.Unlock()
	return f != nil && f()
}

// RequestRelayDownload asks the sender to send the file through the server.
func (e *FileTransferEngine) RequestRelayDownload(attachmentID, senderUserID uuid.UUID, filename string, size int64, contentHash string, destPath string) {
	key := transferKey(attachmentID, senderUserID)
	e.receivesMu.Lock()
	e.receives[key] = &activeReceive{filename: filename, destPath: destPath, expectSize: size, expectHash: contentHash, relay: true}
	e.receivesMu.Unlock()
	e.sigOut <- FileTransferSignalOut{TargetUserID: senderUserID, AttachmentID: attachmentID, Type: "relay_request"}
}

// HandleRelaySignal takes one relayed-transfer signal; reply sends straight
// back over the connection it came on (chunks and acks don't go through
// the screen's update loop).
func (e *FileTransferEngine) HandleRelaySignal(sig protocol.FileTransferSignalRelayPayload, reply func(FileTransferSignalOut)) {
	from, att := sig.SourceUserID, sig.AttachmentID
	key := transferKey(att, from)
	switch sig.Type {
	case "use_relay": // the sender is on Tor: ask again, relayed
		e.receivesMu.Lock()
		if r := e.receives[key]; r != nil {
			r.relay = true
		}
		e.receivesMu.Unlock()
		reply(FileTransferSignalOut{TargetUserID: from, AttachmentID: att, Type: "relay_request"})
	case "relay_request":
		go e.relaySendFile(from, att, reply)
	case "relay_ack":
		e.relayMu.Lock()
		s := e.relaySends[key]
		e.relayMu.Unlock()
		if s != nil {
			select {
			case s.acks <- sig.Seq:
			default:
			}
		}
	case "relay_chunk":
		e.relayReceive(from, att, sig.Seq, sig.Data, reply)
	case "relay_end":
		e.receivesMu.Lock()
		recv := e.receives[key]
		e.receivesMu.Unlock()
		if recv == nil {
			return
		}
		if recv.destFile == nil {
			if err := e.openReceive(recv); err != nil {
				e.failReceive(att, from, key, recv, err)
				return
			}
		}
		if recv.received >= recv.expectSize {
			e.stopStallTimer(recv)
			e.finalizeReceive(att, from, key, recv)
		} else {
			e.stopStallTimer(recv)
			e.failReceive(att, from, key, recv, fmt.Errorf("the sender stopped after %d of %d bytes", recv.received, recv.expectSize))
		}
	case "relay_error":
		reason := sig.SDP
		if reason == "" {
			reason = "the transfer was stopped"
		}
		e.relayMu.Lock()
		s := e.relaySends[key]
		e.relayMu.Unlock()
		if s != nil { // the receiver gave up
			e.stopRelaySend(key, s)
			return
		}
		e.receivesMu.Lock()
		recv := e.receives[key]
		e.receivesMu.Unlock()
		if recv != nil {
			e.stopStallTimer(recv)
			e.failReceive(att, from, key, recv, errors.New(reason))
		} else {
			select {
			case e.eventOut <- FileTransferRejectedMsg{AttachmentID: att}:
			default:
			}
		}
	}
}

// openReceive opens the partial file a download is written to.
func (e *FileTransferEngine) openReceive(recv *activeReceive) error {
	finalPath, err := resolveDownloadPath(recv.destPath, e.downloadDir, recv.filename)
	if err != nil {
		return err
	}
	recv.finalPath = finalPath
	recv.tempPath = finalPath + ".part"
	f, err := os.Create(recv.tempPath)
	if err != nil {
		return err
	}
	recv.destFile = f
	recv.hasher = sha256.New()
	return nil
}

// relayReceive writes one chunk, in order, and acknowledges it.
func (e *FileTransferEngine) relayReceive(from, att uuid.UUID, seq int64, data []byte, reply func(FileTransferSignalOut)) {
	key := transferKey(att, from)
	e.receivesMu.Lock()
	recv := e.receives[key]
	e.receivesMu.Unlock()
	if recv == nil || !recv.relay {
		reply(FileTransferSignalOut{TargetUserID: from, AttachmentID: att, Type: "relay_error", SDP: "not expecting this file"})
		return
	}
	fail := func(err error) {
		e.stopStallTimer(recv)
		reply(FileTransferSignalOut{TargetUserID: from, AttachmentID: att, Type: "relay_error", SDP: err.Error()})
		e.failReceive(att, from, key, recv, err)
	}
	if recv.destFile == nil {
		if err := e.openReceive(recv); err != nil {
			fail(err)
			return
		}
	}
	if seq != recv.nextSeq+1 {
		fail(fmt.Errorf("a piece of the file went missing (got %d, expected %d)", seq, recv.nextSeq+1))
		return
	}
	if recv.received+int64(len(data)) > recv.expectSize {
		fail(errors.New("the sender sent more than the file's size"))
		return
	}
	if _, err := recv.destFile.Write(data); err != nil {
		fail(err)
		return
	}
	recv.hasher.Write(data)
	recv.received += int64(len(data))
	recv.nextSeq = seq
	e.resetStallTimer(att, from, key, recv, reply)
	reply(FileTransferSignalOut{TargetUserID: from, AttachmentID: att, Type: "relay_ack", Seq: seq})
	select {
	case e.eventOut <- FileTransferProgressMsg{AttachmentID: att, PeerUserID: from, Direction: TransferReceiving, BytesDone: recv.received, TotalBytes: recv.expectSize}:
	default:
	}
}

// resetStallTimer fails a download that hears nothing for relayStall.
func (e *FileTransferEngine) resetStallTimer(att, from uuid.UUID, key string, recv *activeReceive, reply func(FileTransferSignalOut)) {
	if recv.stall != nil {
		recv.stall.Stop()
	}
	recv.stall = time.AfterFunc(relayStall, func() {
		e.receivesMu.Lock()
		still := e.receives[key] == recv
		e.receivesMu.Unlock()
		if still {
			reply(FileTransferSignalOut{TargetUserID: from, AttachmentID: att, Type: "relay_error", SDP: "nothing arrived for two minutes"})
			e.failReceive(att, from, key, recv, errors.New("the transfer stalled: nothing arrived for two minutes"))
		}
	})
}

func (e *FileTransferEngine) stopStallTimer(recv *activeReceive) {
	if recv.stall != nil {
		recv.stall.Stop()
	}
}

func (e *FileTransferEngine) stopRelaySend(key string, s *relaySend) {
	e.relayMu.Lock()
	if e.relaySends[key] == s && !s.once {
		s.once = true
		close(s.stop)
		delete(e.relaySends, key)
	}
	e.relayMu.Unlock()
}

// relaySendFile sends a shared file through the server, relayWindow chunks
// ahead of the receiver's acks.
func (e *FileTransferEngine) relaySendFile(to, att uuid.UUID, reply func(FileTransferSignalOut)) {
	refuse := func(why string) {
		reply(FileTransferSignalOut{TargetUserID: to, AttachmentID: att, Type: "relay_error", SDP: why})
	}
	e.sharedFilesMu.RLock()
	localPath, ok := e.sharedFiles[att]
	e.sharedFilesMu.RUnlock()
	if !ok {
		refuse("the sender doesn't have this file any more")
		return
	}
	f, err := os.Open(localPath)
	if err != nil {
		refuse("the sender can't open this file any more")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		refuse(err.Error())
		return
	}
	total := fi.Size()

	key := transferKey(att, to)
	s := &relaySend{acks: make(chan int64, relayWindow*2), stop: make(chan struct{})}
	e.relayMu.Lock()
	if old := e.relaySends[key]; old != nil && !old.once {
		old.once = true
		close(old.stop)
	}
	e.relaySends[key] = s
	e.relayMu.Unlock()
	defer e.stopRelaySend(key, s)

	done := func(err error) {
		e.emitDone(FileTransferDoneMsg{AttachmentID: att, PeerUserID: to, Direction: TransferSending, Filename: filepath.Base(localPath), Err: err})
	}
	var seq, acked, sent int64
	waitAck := func() error {
		select {
		case n := <-s.acks:
			if n > acked {
				acked = n
			}
			return nil
		case <-s.stop:
			return errors.New("the receiver stopped the transfer")
		case <-e.quit:
			return errors.New("Concord is closing")
		case <-time.After(relayStall):
			return errors.New("the receiver stopped answering")
		}
	}
	buf := make([]byte, relayChunkSize)
	for {
		n, rerr := io.ReadFull(f, buf)
		if n > 0 {
			for seq-acked >= relayWindow {
				if err := waitAck(); err != nil {
					refuse(err.Error())
					done(err)
					return
				}
			}
			seq++
			chunk := append([]byte(nil), buf[:n]...)
			reply(FileTransferSignalOut{TargetUserID: to, AttachmentID: att, Type: "relay_chunk", Seq: seq, Data: chunk})
			sent += int64(n)
			select {
			case e.eventOut <- FileTransferProgressMsg{AttachmentID: att, PeerUserID: to, Direction: TransferSending, BytesDone: sent, TotalBytes: total}:
			default:
			}
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			refuse("the sender couldn't read the file")
			done(rerr)
			return
		}
	}
	for acked < seq {
		if err := waitAck(); err != nil {
			refuse(err.Error())
			done(err)
			return
		}
	}
	reply(FileTransferSignalOut{TargetUserID: to, AttachmentID: att, Type: "relay_end"})
	done(nil)
}

// fileSignalSender sends file signals straight over sc's connection.
func fileSignalSender(sc *ServerConnection) func(FileTransferSignalOut) {
	return func(sig FileTransferSignalOut) {
		if sc == nil {
			return
		}
		sc.mu.RLock()
		conn := sc.Connection
		sc.mu.RUnlock()
		if conn == nil {
			return
		}
		msg, err := protocol.NewMessage(protocol.OpFileTransferSignal, &protocol.FileTransferSignalPayload{
			TargetUserID: sig.TargetUserID, AttachmentID: sig.AttachmentID, Type: sig.Type,
			SDP: sig.SDP, Seq: sig.Seq, Data: sig.Data,
		})
		if err == nil {
			_ = conn.Send(msg)
		}
	}
}
