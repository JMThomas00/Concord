//go:build !novoice

// Real voice engine — compiled by default; excluded with: go build -tags novoice ./cmd/client
//
// Audio I/O:  github.com/gen2brain/malgo  (CGO wrapper for miniaudio)
// Codec:      github.com/hraban/opus      (CGO wrapper for libopus)
// Transport:  github.com/pion/webrtc/v3   (pure-Go WebRTC, no CGO)
//
// Audio format: 16-bit mono PCM at 48 kHz (voiceSampleRate). Each 20 ms frame
// goes through the processing chain in sendFrame (echo cancellation, low-cut,
// RNNoise, levelling; see voice_dsp.go), is Opus-encoded with in-band FEC, and
// is sent with a sequence number over the WebRTC SCTP data channel
// (unreliable, unordered — behaves like UDP so stale packets are dropped
// rather than blocking newer audio). Receivers conceal lost packets with
// Opus FEC and PLC (onAudioData).
//
// Offer/answer collision resolution:
//   The peer whose UUID string sorts lexicographically LOWER sends the Offer.
//   This is deterministic and avoids simultaneous-offer races.

package client

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	malgo "github.com/gen2brain/malgo"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
	"github.com/hraban/opus"
	"github.com/pion/webrtc/v3"

	"github.com/concord-chat/concord/internal/rnnoise"
)

// ── Audio constants ───────────────────────────────────────────────────────────

const (
	voiceChannels   = 1  // mono
	voiceFrameMs    = 20 // frame duration ms
	vadHoldDuration = 300 * time.Millisecond
)

// maxOpusPacketBytes is a safe upper bound for an encoded Opus frame buffer.
// A 20 ms frame at 96 kbps is at most 240 bytes; 4000 gives ample headroom.
const maxOpusPacketBytes = 4000

// opusFECLossPercent is the packet loss Opus's in-band FEC is tuned for:
// each packet carries a low-bitrate copy of the one before, so a single
// lost packet can be rebuilt from the next.
const opusFECLossPercent = 10

// opusAppVoIP is OPUS_APPLICATION_VOIP (2048) expressed as a plain Go constant
// so that gopls can evaluate it without running the C preprocessor. Using
// opus.AppVoIP directly causes a "constant unknown with invalid type" IDE error
// because gopls cannot resolve CGO-defined constants at analysis time.
const opusAppVoIP opus.Application = 2048

// opusFullband is OPUS_BANDWIDTH_FULLBAND (1105), for the same reason.
const opusFullband opus.Bandwidth = 1105

// ── peerConn ─────────────────────────────────────────────────────────────────

// peerConn holds the WebRTC connection to one remote user.
type peerConn struct {
	userID uuid.UUID
	pc     *webrtc.PeerConnection
	mu     sync.Mutex
	sendDC *webrtc.DataChannel // we write our PCM here once open
	// ICE candidates buffered before SetRemoteDescription completes.
	pendingCandidates []webrtc.ICECandidateInit
	remoteDescSet     bool
}

func (p *peerConn) bufferCandidate(c webrtc.ICECandidateInit) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.remoteDescSet {
		p.pendingCandidates = append(p.pendingCandidates, c)
		return
	}
	if err := p.pc.AddICECandidate(c); err != nil {
		log.Printf("voice: AddICECandidate: %v", err)
	}
}

func (p *peerConn) flushCandidates() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.remoteDescSet = true
	for _, c := range p.pendingCandidates {
		if err := p.pc.AddICECandidate(c); err != nil {
			log.Printf("voice: AddICECandidate(flush): %v", err)
		}
	}
	p.pendingCandidates = nil
}

// ── incomingBuffer ────────────────────────────────────────────────────────────

// incomingBuffer is a bounded ring buffer for PCM samples from one remote peer.
type incomingBuffer struct {
	mu        sync.Mutex
	data      []int16
	volume    float64
	lastLevel float32 // RMS of last pushed frame, normalised to [0,1]; read by pollLevels
}

func newIncomingBuffer(volume float64) *incomingBuffer {
	// Initial capacity: 8 × 20ms frames at 48 kHz (worst case / ultra preset).
	return &incomingBuffer{
		data:   make([]int16, 0, 960*8),
		volume: volume,
	}
}

func (b *incomingBuffer) push(samples []int16) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Cap at ~200ms of audio at 48 kHz to bound memory usage.
	const maxSamples = 960 * 10
	if len(b.data)+len(samples) > maxSamples {
		drop := len(b.data) + len(samples) - maxSamples
		b.data = b.data[drop:]
	}
	b.data = append(b.data, samples...)
	// Track RMS level for the VU meter.
	if len(samples) > 0 {
		var sum float64
		for _, s := range samples {
			f := float64(s) / 32768.0
			sum += f * f
		}
		b.lastLevel = float32(math.Sqrt(sum / float64(len(samples))))
	}
}

// pop reads up to n samples, applying per-user volume. Returns zero-padded slice.
func (b *incomingBuffer) pop(n int) []int16 {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int16, n)
	avail := len(b.data)
	if avail > n {
		avail = n
	}
	vol := b.volume
	for i := 0; i < avail; i++ {
		scaled := float64(b.data[i]) * vol
		if scaled > 32767 {
			scaled = 32767
		} else if scaled < -32768 {
			scaled = -32768
		}
		out[i] = int16(scaled)
	}
	b.data = b.data[avail:]
	return out
}

// ── VoiceEngine ───────────────────────────────────────────────────────────────

// VoiceEngine manages audio capture/playback and WebRTC peer connections.
type VoiceEngine struct {
	cfg         AudioConfig
	localUserID uuid.UUID
	channelID   uuid.UUID
	serverID    uuid.UUID
	iceList     []protocol.ICEServer // STUN and TURN, from the server (voice_ice.go)

	api *webrtc.API

	peers   map[uuid.UUID]*peerConn
	peersMu sync.RWMutex

	incoming   map[uuid.UUID]*incomingBuffer
	incomingMu sync.RWMutex

	malgoCtx   *malgo.AllocatedContext
	capDevice  *malgo.Device
	playDevice *malgo.Device

	captureC chan []byte    // capture callback → processCapture goroutine
	procWg   sync.WaitGroup // tracks processCapture goroutine; Wait()ed in Stop()

	// Opus encoder — owned exclusively by the processCapture goroutine after Start().
	// pendingBitrate carries a new bitrate set by UpdateConfig(); processCapture
	// reads and applies it atomically before the next Encode call so we never
	// call SetBitrate concurrently with Encode.
	enc            *opus.Encoder
	pendingBitrate int32 // target bps; 0 = no pending change

	// opusBuf is the reusable packet buffer: a 3-byte header (tag and
	// sequence number, voicePacketHeader) and the encoded frame after it.
	// Owned by processCapture goroutine only, as are txSeq and the
	// processing stages below.
	opusBuf [maxOpusPacketBytes + 3]byte
	txSeq   uint16

	// The microphone processing chain (voice_dsp.go): low-cut filter,
	// RNNoise, and automatic levelling. work is the frame as float32.
	lowCut   *lowCut
	denoiser *rnnoise.Denoiser
	leveler  *autoLevel
	work     []float32
	original []float32

	// Opus decoders — one per remote peer, created in touchIncoming() —
	// and each peer's packet sequence, for concealing losses.
	decoders   map[uuid.UUID]*opus.Decoder
	rxSeq      map[uuid.UUID]*rxSequence
	decodersMu sync.RWMutex

	// sampleRate is set once in Start() and read from pion goroutines; atomic so
	// reads in onAudioData/touchIncoming don't require a lock.
	sampleRate atomic.Uint32

	// VAD state (used in processCapture goroutine only — no lock needed)
	isSpeaking bool
	speakUntil time.Time

	// Echo cancellation. aecFarEnd is written by mixPCM (playback callback
	// goroutine) and read by sendFrame (processCapture goroutine) -- it has
	// its own internal mutex. aecFilt's taps are owned exclusively by
	// processCapture, same as enc. Both are always allocated in Start() (not
	// only when EchoCancellation starts enabled) so toggling it on mid-call
	// works immediately without lazy-init races -- see UpdateConfig.
	aecFarEnd *aecFarEndBuffer
	aecFilt   *aecFilter

	// localLevel holds the RMS amplitude of the most recent captured mic frame,
	// stored as float32 bits in a uint32 for lock-free atomic access.
	// Written by processCapture goroutine, read by pollLevels goroutine.
	localLevel uint32

	sigOut   chan<- VoiceSignalOut // outbound signaling → app → server
	eventOut chan<- interface{}    // events → bubbletea Update loop

	quit    chan struct{}
	stopped bool
	mu      sync.Mutex
}

// NewVoiceEngine constructs a VoiceEngine without starting audio devices.
func NewVoiceEngine(
	cfg AudioConfig,
	localUID uuid.UUID,
	sigOut chan<- VoiceSignalOut,
	eventOut chan<- interface{},
) *VoiceEngine {
	se := webrtc.SettingEngine{}
	me := &webrtc.MediaEngine{}
	api := webrtc.NewAPI(
		webrtc.WithSettingEngine(se),
		webrtc.WithMediaEngine(me),
	)
	return &VoiceEngine{
		cfg:           cfg,
		localUserID:   localUID,
		api:           api,
		peers:         make(map[uuid.UUID]*peerConn),
		incoming:      make(map[uuid.UUID]*incomingBuffer),
		decoders:      make(map[uuid.UUID]*opus.Decoder),
		rxSeq:         make(map[uuid.UUID]*rxSequence),
		captureC:      make(chan []byte, 32),
		sigOut:        sigOut,
		eventOut:      eventOut,
		quit:          make(chan struct{}),
	}
}

// ── Lifecycle ─────────────────────────────────────────────────────────────────

// Start initialises audio devices and begins capturing from the microphone.
func (e *VoiceEngine) Start(serverID, channelID uuid.UUID, ice []protocol.ICEServer) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.serverID = serverID
	e.channelID = channelID
	e.iceList = ice

	// ── malgo context ────────────────────────────────────────────────────────
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(msg string) {
		log.Printf("voice/malgo: %s", strings.TrimSpace(msg))
	})
	if err != nil {
		return fmt.Errorf("malgo context: %w", err)
	}
	if ctx == nil {
		return fmt.Errorf("malgo context: InitContext returned nil (audio backend unavailable on this system)")
	}
	// NOTE: do NOT assign e.malgoCtx yet — only store it once we know the
	// context's internal C pointer is valid. A broken context stored in
	// e.malgoCtx would cause Stop() to panic on Uninit() later.

	// freeCtx tears down ctx if Start fails before we commit it to e.malgoCtx.
	freeCtx := func() {
		_ = ctx.Uninit()
		ctx.Free()
	}

	// Always 48 kHz (RNNoise needs it); the preset only sets the bitrate.
	sampleRate := uint32(voiceSampleRate)
	bitrate := bitrateForPreset(e.cfg.CodecPreset)
	log.Printf("voice: codec preset=%q → %d Hz / %d bps (Opus)", e.cfg.CodecPreset, sampleRate, bitrate)

	// Pin each capture/playback callback to exactly one Opus frame's worth of
	// samples. Without an explicit period size, miniaudio/ALSA picks its own
	// default, which frequently does NOT land on one of Opus's fixed valid
	// frame durations (2.5/5/10/20/40/60ms) -- Encode() then fails with
	// "opus: invalid argument" on every single callback.
	periodFrames := sampleRate * uint32(voiceFrameMs) / 1000

	// ── Opus encoder ─────────────────────────────────────────────────────────
	enc, err := opus.NewEncoder(int(sampleRate), voiceChannels, opusAppVoIP)
	if err != nil {
		freeCtx()
		return fmt.Errorf("opus encoder: %w", err)
	}
	if err := enc.SetBitrate(bitrate); err != nil {
		log.Printf("voice: opus SetBitrate: %v", err)
	}
	// Best quality the encoder can do (a 20 ms mono voice frame is cheap at
	// any setting), full band, and a low-bitrate copy of each frame in the
	// next one, so a single lost packet can be rebuilt (onAudioData).
	for name, err := range map[string]error{
		"SetComplexity":     enc.SetComplexity(10),
		"SetMaxBandwidth":   enc.SetMaxBandwidth(opusFullband),
		"SetInBandFEC":      enc.SetInBandFEC(true),
		"SetPacketLossPerc": enc.SetPacketLossPerc(opusFECLossPercent),
	} {
		if err != nil {
			log.Printf("voice: opus %s: %v", name, err)
		}
	}
	e.enc = enc
	e.sampleRate.Store(sampleRate)

	// ── Microphone processing (voice_dsp.go) ─────────────────────────────────
	e.lowCut = newLowCut(80, voiceSampleRate)
	e.denoiser = rnnoise.New()
	e.leveler = newAutoLevel()
	e.work = make([]float32, 0, voiceSampleRate*voiceFrameMs/1000)
	e.original = make([]float32, 0, voiceSampleRate*voiceFrameMs/1000)

	// ── Echo cancellation ────────────────────────────────────────────────────
	// Always allocated (usage is still gated per-frame on cfg.EchoCancellation)
	// so toggling it on mid-call doesn't need any lazy re-init. Capacity is the
	// filter's tap window plus one frame of slack, matching what aecFilter.process
	// requires as minimum far-end history.
	numTaps := aecTapsForSampleRate(sampleRate)
	e.aecFilt = newAECFilter(numTaps)
	e.aecFarEnd = newAECFarEndBuffer(numTaps + int(periodFrames))

	// ── Capture device (microphone) ──────────────────────────────────────────
	capCfg := malgo.DefaultDeviceConfig(malgo.Capture)
	capCfg.Capture.Format = malgo.FormatS16
	capCfg.Capture.Channels = voiceChannels
	capCfg.SampleRate = sampleRate
	capCfg.PeriodSizeInFrames = periodFrames
	// Select specific input device if configured.
	if e.cfg.InputDevice != "" {
		if devs, err := ctx.Devices(malgo.Capture); err == nil {
			for _, d := range devs {
				if d.Name() == e.cfg.InputDevice {
					id := d.ID
					capCfg.Capture.DeviceID = id.Pointer()
					break
				}
			}
		}
	}

	capDev, err := malgo.InitDevice(ctx.Context, capCfg, malgo.DeviceCallbacks{
		Data: func(_, pInput []byte, _ uint32) {
			if len(pInput) == 0 {
				return
			}
			frame := make([]byte, len(pInput))
			copy(frame, pInput)
			select {
			case e.captureC <- frame:
			default: // drop on overrun
			}
		},
		Stop: e.handleDeviceStopped,
	})
	if err != nil {
		freeCtx()
		return fmt.Errorf("malgo capture init: %w", err)
	}

	// ── Playback device (speakers / headphones) ───────────────────────────────
	playCfg := malgo.DefaultDeviceConfig(malgo.Playback)
	playCfg.Playback.Format = malgo.FormatS16
	playCfg.Playback.Channels = voiceChannels
	playCfg.SampleRate = sampleRate
	playCfg.PeriodSizeInFrames = periodFrames
	// Select specific output device if configured.
	if e.cfg.OutputDevice != "" {
		if devs, err := ctx.Devices(malgo.Playback); err == nil {
			for _, d := range devs {
				if d.Name() == e.cfg.OutputDevice {
					id := d.ID
					playCfg.Playback.DeviceID = id.Pointer()
					break
				}
			}
		}
	}

	playDev, err := malgo.InitDevice(ctx.Context, playCfg, malgo.DeviceCallbacks{
		Data: func(pOutput, _ []byte, frameCount uint32) {
			mixed := e.mixPCM(int(frameCount))
			copy(pOutput, mixed)
		},
		Stop: e.handleDeviceStopped,
	})
	if err != nil {
		capDev.Uninit()
		freeCtx()
		return fmt.Errorf("malgo playback init: %w", err)
	}

	// ── Start both devices ───────────────────────────────────────────────────
	if err := capDev.Start(); err != nil {
		capDev.Uninit()
		playDev.Uninit()
		freeCtx()
		return fmt.Errorf("malgo start capture: %w", err)
	}
	if err := playDev.Start(); err != nil {
		_ = capDev.Stop()
		capDev.Uninit()
		playDev.Uninit()
		freeCtx()
		return fmt.Errorf("malgo start playback: %w", err)
	}

	// All devices started successfully — commit to engine state.
	e.malgoCtx = ctx
	e.capDevice = capDev
	e.playDevice = playDev

	e.procWg.Add(1)
	go e.processCapture()
	go e.pollStats()
	go e.pollLevels()

	select {
	case e.eventOut <- VoiceEngineReadyMsg{}:
	default:
	}
	log.Printf("voice: engine started (channel=%s)", channelID)
	return nil
}

// Stop tears down peers and audio devices.
func (e *VoiceEngine) Stop() {
	// Recover from any CGO/malgo panics during teardown (e.g. a context whose
	// internal C pointer was never fully initialised on some Windows drivers).
	defer func() {
		if r := recover(); r != nil {
			log.Printf("voice: panic in Stop (recovered): %v", r)
		}
	}()

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
	e.peers = make(map[uuid.UUID]*peerConn)
	e.peersMu.Unlock()

	if e.capDevice != nil {
		_ = e.capDevice.Stop()
		e.capDevice.Uninit()
	}
	if e.playDevice != nil {
		_ = e.playDevice.Stop()
		e.playDevice.Uninit()
	}
	if e.malgoCtx != nil {
		_ = e.malgoCtx.Uninit()
		e.malgoCtx.Free()
	}

	// Wait for processCapture to exit before nilling the encoder it owns.
	e.procWg.Wait()
	e.enc = nil
	e.denoiser.Close()
	e.denoiser = nil
	e.decodersMu.Lock()
	e.decoders = make(map[uuid.UUID]*opus.Decoder)
	e.rxSeq = make(map[uuid.UUID]*rxSequence)
	e.decodersMu.Unlock()

	log.Printf("voice: engine stopped")
}

// handleDeviceStopped is wired as every malgo device's Stop callback. It fires
// both when Stop() itself calls capDevice.Stop()/playDevice.Stop() during an
// intentional shutdown, and when the backend fails out from under us (e.g. an
// "[ALSA] poll() failed." on the underlying PCM device). There is no error
// return path for the latter case -- without this, capture/playback would go
// silently dead while the app kept reporting the engine as connected. e.stopped
// tells the two apart.
func (e *VoiceEngine) handleDeviceStopped() {
	e.mu.Lock()
	intentional := e.stopped
	e.mu.Unlock()
	if intentional {
		return
	}
	log.Printf("voice: device stopped unexpectedly")
	select {
	case e.eventOut <- VoiceEngineErrorMsg{Err: fmt.Errorf("audio device disconnected unexpectedly")}:
	default:
	}
}

// ── Peer management ───────────────────────────────────────────────────────────

// AddPeer opens a WebRTC connection to the given remote user. If our UUID is
// lexicographically smaller we send the Offer; otherwise we wait for theirs.
func (e *VoiceEngine) AddPeer(userID uuid.UUID) {
	e.peersMu.Lock()
	if _, ok := e.peers[userID]; ok {
		e.peersMu.Unlock()
		return
	}
	e.peersMu.Unlock()

	peer := e.newPeerConn(userID)
	if peer == nil {
		return
	}

	e.peersMu.Lock()
	e.peers[userID] = peer
	e.peersMu.Unlock()

	if e.localUserID.String() < userID.String() {
		go e.sendOffer(peer)
	}
}

// RemovePeer closes the connection to a peer and cleans up its audio buffer.
func (e *VoiceEngine) RemovePeer(userID uuid.UUID) {
	e.peersMu.Lock()
	p, ok := e.peers[userID]
	if ok {
		delete(e.peers, userID)
	}
	e.peersMu.Unlock()
	if ok {
		_ = p.pc.Close()
	}
	e.dropIncoming(userID)
}

// newPeerConn builds a peerConn with all event handlers wired up.
func (e *VoiceEngine) newPeerConn(userID uuid.UUID) *peerConn {
	pc, err := e.api.NewPeerConnection(webrtc.Configuration{
		ICEServers: e.iceServers(),
	})
	if err != nil {
		log.Printf("voice: NewPeerConnection(%s): %v", userID, err)
		return nil
	}
	peer := &peerConn{userID: userID, pc: pc}

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		b, err := json.Marshal(c.ToJSON())
		if err != nil {
			return
		}
		e.sigOut <- VoiceSignalOut{
			TargetUserID: userID,
			ChannelID:    e.channelID,
			Type:         "candidate",
			Candidate:    b,
		}
	})

	pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		log.Printf("voice: %s → %s", userID, s)
		switch s {
		case webrtc.PeerConnectionStateConnected:
			select {
			case e.eventOut <- VoicePeerConnectedMsg{UserID: userID}:
			default:
			}
		case webrtc.PeerConnectionStateFailed,
			webrtc.PeerConnectionStateDisconnected,
			webrtc.PeerConnectionStateClosed:
			e.dropIncoming(userID)
			select {
			case e.eventOut <- VoicePeerDisconnectedMsg{UserID: userID}:
			default:
			}
		}
	})

	// Handle incoming audio data channel (remote peer → us).
	pc.OnDataChannel(func(d *webrtc.DataChannel) {
		if d.Label() != "audio" {
			return
		}
		d.OnOpen(func() {
			e.touchIncoming(userID)
		})
		d.OnMessage(func(msg webrtc.DataChannelMessage) {
			if !msg.IsString && len(msg.Data) > 0 {
				e.onAudioData(userID, msg.Data)
			}
		})
	})

	return peer
}

// ── Signaling ─────────────────────────────────────────────────────────────────

// HandleSignal routes an incoming WebRTC signal to the appropriate handler.
func (e *VoiceEngine) HandleSignal(fromUserID uuid.UUID, sigType, sdp string, candidateJSON []byte) {
	switch sigType {
	case "offer":
		go e.handleOffer(fromUserID, sdp)
	case "answer":
		go e.handleAnswer(fromUserID, sdp)
	case "candidate":
		go e.handleCandidate(fromUserID, candidateJSON)
	}
}

func (e *VoiceEngine) sendOffer(peer *peerConn) {
	ordered := false
	maxRtx := uint16(0)
	dc, err := peer.pc.CreateDataChannel("audio", &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &maxRtx,
	})
	if err != nil {
		log.Printf("voice: CreateDataChannel offer: %v", err)
		return
	}
	dc.OnOpen(func() {
		peer.mu.Lock()
		peer.sendDC = dc
		peer.mu.Unlock()
	})

	offer, err := peer.pc.CreateOffer(nil)
	if err != nil {
		log.Printf("voice: CreateOffer: %v", err)
		return
	}
	if err := peer.pc.SetLocalDescription(offer); err != nil {
		log.Printf("voice: SetLocalDescription offer: %v", err)
		return
	}
	e.sigOut <- VoiceSignalOut{
		TargetUserID: peer.userID,
		ChannelID:    e.channelID,
		Type:         "offer",
		SDP:          offer.SDP,
	}
}

func (e *VoiceEngine) handleOffer(fromUserID uuid.UUID, sdp string) {
	// Ensure a peer entry exists (remote may have initiated before we called AddPeer).
	e.peersMu.Lock()
	peer, ok := e.peers[fromUserID]
	if !ok {
		peer = e.newPeerConn(fromUserID)
		if peer == nil {
			e.peersMu.Unlock()
			return
		}
		e.peers[fromUserID] = peer
	}
	e.peersMu.Unlock()

	if err := peer.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  sdp,
	}); err != nil {
		log.Printf("voice: SetRemoteDescription offer: %v", err)
		return
	}
	peer.flushCandidates()

	// Create our audio send channel for this direction.
	ordered := false
	maxRtx := uint16(0)
	dc, err := peer.pc.CreateDataChannel("audio", &webrtc.DataChannelInit{
		Ordered:        &ordered,
		MaxRetransmits: &maxRtx,
	})
	if err != nil {
		log.Printf("voice: CreateDataChannel answer: %v", err)
		return
	}
	dc.OnOpen(func() {
		peer.mu.Lock()
		peer.sendDC = dc
		peer.mu.Unlock()
	})

	answer, err := peer.pc.CreateAnswer(nil)
	if err != nil {
		log.Printf("voice: CreateAnswer: %v", err)
		return
	}
	if err := peer.pc.SetLocalDescription(answer); err != nil {
		log.Printf("voice: SetLocalDescription answer: %v", err)
		return
	}
	e.sigOut <- VoiceSignalOut{
		TargetUserID: fromUserID,
		ChannelID:    e.channelID,
		Type:         "answer",
		SDP:          answer.SDP,
	}
}

func (e *VoiceEngine) handleAnswer(fromUserID uuid.UUID, sdp string) {
	e.peersMu.RLock()
	peer, ok := e.peers[fromUserID]
	e.peersMu.RUnlock()
	if !ok {
		log.Printf("voice: answer from unknown peer %s", fromUserID)
		return
	}
	if err := peer.pc.SetRemoteDescription(webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  sdp,
	}); err != nil {
		log.Printf("voice: SetRemoteDescription answer: %v", err)
		return
	}
	peer.flushCandidates()
}

func (e *VoiceEngine) handleCandidate(fromUserID uuid.UUID, candidateJSON []byte) {
	e.peersMu.RLock()
	peer, ok := e.peers[fromUserID]
	e.peersMu.RUnlock()
	if !ok {
		return
	}
	var init webrtc.ICECandidateInit
	if err := json.Unmarshal(candidateJSON, &init); err != nil {
		log.Printf("voice: bad ICE candidate JSON: %v", err)
		return
	}
	peer.bufferCandidate(init)
}

// ── Audio pipeline ────────────────────────────────────────────────────────────

// processCapture runs in a goroutine, consuming raw PCM from the capture callback.
func (e *VoiceEngine) processCapture() {
	defer e.procWg.Done()
	for {
		select {
		case <-e.quit:
			return
		case frame := <-e.captureC:
			e.sendFrame(frame)
		}
	}
}

func (e *VoiceEngine) sendFrame(raw []byte) {
	samples := bytesToInt16(raw)

	// Echo cancellation: subtract our own predicted playback echo before
	// anything else (gain, noise gate, VAD, VU meter) touches the signal.
	if e.cfg.EchoCancellation && e.aecFilt != nil && e.aecFarEnd != nil {
		farHistory := e.aecFarEnd.snapshot(len(samples) + len(e.aecFilt.taps) - 1)
		samples = e.aecFilt.process(samples, farHistory, aecMuForStrength(e.cfg.EchoCancellationStrength))
	}

	// The rest of the chain works in float32 on the int16 scale (voice_dsp.go).
	work := e.work[:0]
	for _, s := range samples {
		work = append(work, float32(s))
	}
	gain := float32(1)
	if e.cfg.InputGain != 0 {
		gain = float32(e.cfg.InputGain)
	}
	for i := range work {
		work[i] *= gain
	}

	// Low-cut: rumble, desk bumps and mains hum.
	e.lowCut.process(work)

	// Noise suppression (RNNoise, in 10 ms pieces): removes background
	// noise while someone talks, and says how likely the frame is speech.
	// Strength blends some of the original back (mixDenoised).
	speechProb := float32(-1) // -1: not measured
	if e.cfg.NoiseSuppress && rnnoise.Available && e.denoiser != nil {
		original := append(e.original[:0], work...)
		speechProb = 0
		for off := 0; off+rnnoise.FrameSize <= len(work); off += rnnoise.FrameSize {
			if p := e.denoiser.Process(work[off : off+rnnoise.FrameSize]); p > speechProb {
				speechProb = p
			}
		}
		mixDenoised(work, original, clampF(e.cfg.NoiseSuppressStrength, 0, 1))
	}

	// Automatic levelling, measured on speech only.
	isSpeech := speechProb >= 0.5 || (speechProb < 0 && rmsAmplitude(samples) >= 0.02)
	if e.cfg.AutoLevelOff {
		for i, s := range work {
			work[i] = softLimit(float64(s))
		}
	} else {
		e.leveler.process(work, isSpeech)
	}
	for i, s := range work {
		samples[i] = int16(s)
	}

	// Track local mic level for the VU meter — always, regardless of the VAD gate.
	micRMS := float32(rmsAmplitude(samples))
	atomic.StoreUint32(&e.localLevel, *(*uint32)(unsafe.Pointer(&micRMS)))

	// VAD / gate decision. With RNNoise running, it decides what's speech
	// (keyboards and coughs don't open the mic); without it, loudness does.
	var shouldSend bool
	switch {
	case e.cfg.VADEnabled:
		now := time.Now()
		var open bool
		if speechProb >= 0 {
			open = speechProb >= speechThreshold(vadSensitivityFromThreshold(e.cfg.VADThreshold))
		} else {
			threshold := e.cfg.VADThreshold
			if threshold == 0 {
				threshold = 0.02 // sensible default if unset
			}
			open = rmsAmplitude(samples) >= threshold
		}
		if open {
			e.speakUntil = now.Add(vadHoldDuration)
		}
		shouldSend = now.Before(e.speakUntil)
	default:
		shouldSend = true // always-on
	}

	if shouldSend != e.isSpeaking {
		e.isSpeaking = shouldSend
		select {
		case e.eventOut <- voiceLocalSpeakingMsg{speaking: shouldSend}:
		default:
		}
	}

	if !shouldSend {
		return
	}

	if e.enc == nil {
		return
	}

	// Apply any pending bitrate change before encoding (lock-free handoff from UpdateConfig).
	if pb := atomic.SwapInt32(&e.pendingBitrate, 0); pb != 0 {
		if err := e.enc.SetBitrate(int(pb)); err != nil {
			log.Printf("voice: opus SetBitrate: %v", err)
		}
	}

	// Encode PCM → Opus after the packet header (tag and sequence number).
	// The pre-allocated array avoids a per-frame heap allocation.
	e.txSeq++
	h := voicePacketHeader(e.opusBuf[:], e.txSeq)
	n, err := e.enc.Encode(samples, e.opusBuf[h:])
	if err != nil {
		log.Printf("voice: opus encode: %v", err)
		return
	}
	opusPacket := e.opusBuf[:h+n]

	e.peersMu.RLock()
	defer e.peersMu.RUnlock()
	for _, peer := range e.peers {
		peer.mu.Lock()
		dc := peer.sendDC
		peer.mu.Unlock()
		if dc == nil {
			continue
		}
		if err := dc.Send(opusPacket); err != nil {
			log.Printf("voice: send to %s: %v", peer.userID, err)
		}
	}
}

// onAudioData is called from pion goroutines when an Opus packet arrives from a peer.
func (e *VoiceEngine) onAudioData(fromUserID uuid.UUID, data []byte) {
	e.incomingMu.RLock()
	buf, ok := e.incoming[fromUserID]
	e.incomingMu.RUnlock()
	if !ok {
		return
	}

	// Validate and split the packet written by sendFrame (or by a client from
	// before sequence numbers, which is played without loss concealment).
	payload, seq, hasSeq, okPacket := parseVoicePacket(data)
	if !okPacket {
		tag := byte(0)
		if len(data) > 0 {
			tag = data[0]
		}
		log.Printf("voice: dropping unrecognized frame from %s (len=%d, tag=%#x)", fromUserID, len(data), tag)
		return
	}

	// Decoding is serialized per sender under decodersMu: pion can deliver a
	// peer's packets on more than one goroutine, and an Opus decoder (and the
	// sequence it follows) isn't safe for concurrent use.
	e.decodersMu.Lock()
	defer e.decodersMu.Unlock()
	dec, hasDec := e.decoders[fromUserID]
	if !hasDec {
		return
	}
	frameSize := int(e.sampleRate.Load()) * voiceFrameMs / 1000

	if hasSeq {
		rx := e.rxSeq[fromUserID]
		if rx == nil {
			rx = &rxSequence{}
			e.rxSeq[fromUserID] = rx
		}
		lost, late := rx.next(seq)
		if late {
			return // a duplicate, or overtaken by newer audio: already concealed
		}
		if lost > 0 && lost <= maxConcealedFrames {
			// Fill all but the last lost frame with packet loss concealment,
			// then rebuild the last from the copy carried in this packet.
			for i := 0; i < lost-1; i++ {
				pcm := make([]int16, frameSize)
				if err := dec.DecodePLC(pcm); err == nil {
					buf.push(pcm)
				}
			}
			pcm := make([]int16, frameSize)
			if err := dec.DecodeFEC(payload, pcm); err == nil {
				buf.push(pcm)
			}
		}
	}

	// Decode Opus → PCM. frameSize = sampleRate × 20ms / 1000.
	pcm := make([]int16, frameSize)
	n, err := dec.Decode(payload, pcm)
	if err != nil {
		log.Printf("voice: opus decode from %s: %v", fromUserID, err)
		return
	}
	buf.push(pcm[:n])
}

// mixPCM mixes all incoming peer buffers into a single frame. Called from the
// malgo playback callback — must be fast and non-blocking.
func (e *VoiceEngine) mixPCM(frameCount int) []byte {
	mixed := make([]int16, frameCount)
	e.incomingMu.RLock()
	defer e.incomingMu.RUnlock()
	for _, buf := range e.incoming {
		for i, s := range buf.pop(frameCount) {
			v := int32(mixed[i]) + int32(s)
			if v > 32767 {
				v = 32767
			} else if v < -32768 {
				v = -32768
			}
			mixed[i] = int16(v)
		}
	}
	if e.aecFarEnd != nil {
		e.aecFarEnd.push(mixed)
	}
	return int16ToBytes(mixed)
}

// ── Volume / config ───────────────────────────────────────────────────────────

// SetUserVolume adjusts playback volume for a remote peer (0.0–2.0).
func (e *VoiceEngine) SetUserVolume(userID uuid.UUID, vol float64) {
	e.incomingMu.RLock()
	buf, ok := e.incoming[userID]
	e.incomingMu.RUnlock()
	if ok {
		buf.mu.Lock()
		buf.volume = vol
		buf.mu.Unlock()
	}
	// Persist to config map.
	e.mu.Lock()
	if e.cfg.PerUserVolumes == nil {
		e.cfg.PerUserVolumes = make(map[string]float64)
	}
	e.cfg.PerUserVolumes[userID.String()] = vol
	e.mu.Unlock()
}

// UpdateConfig hot-reloads audio config (gain, VAD, per-user volumes, Opus bitrate).
func (e *VoiceEngine) UpdateConfig(cfg AudioConfig) {
	e.mu.Lock()
	oldPreset := e.cfg.CodecPreset
	e.cfg = cfg
	e.mu.Unlock()

	// If the codec preset changed, signal processCapture to update the Opus bitrate
	// before its next Encode call. Sample-rate changes require a voice reconnect.
	if cfg.CodecPreset != oldPreset {
		atomic.StoreInt32(&e.pendingBitrate, int32(bitrateForPreset(cfg.CodecPreset)))
	}

	e.incomingMu.RLock()
	defer e.incomingMu.RUnlock()
	for uidStr, vol := range cfg.PerUserVolumes {
		uid, err := uuid.Parse(uidStr)
		if err != nil {
			continue
		}
		if buf, ok := e.incoming[uid]; ok {
			buf.mu.Lock()
			buf.volume = vol
			buf.mu.Unlock()
		}
	}
}

// ── Internal helpers ──────────────────────────────────────────────────────────

func (e *VoiceEngine) iceServers() []webrtc.ICEServer {
	var out []webrtc.ICEServer
	for _, s := range e.iceList {
		if len(s.URLs) == 0 {
			continue
		}
		out = append(out, webrtc.ICEServer{URLs: s.URLs, Username: s.Username, Credential: s.Credential})
	}
	if len(out) == 0 {
		out = []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	return out
}

func (e *VoiceEngine) touchIncoming(userID uuid.UUID) {
	e.incomingMu.Lock()
	if _, ok := e.incoming[userID]; !ok {
		vol := 1.0
		e.mu.Lock()
		if v, found := e.cfg.PerUserVolumes[userID.String()]; found {
			vol = v
		}
		e.mu.Unlock()
		e.incoming[userID] = newIncomingBuffer(vol)
	}
	e.incomingMu.Unlock()

	// Create an Opus decoder for this peer if one doesn't exist yet.
	e.decodersMu.Lock()
	if _, ok := e.decoders[userID]; !ok {
		dec, err := opus.NewDecoder(int(e.sampleRate.Load()), voiceChannels)
		if err != nil {
			log.Printf("voice: opus NewDecoder for %s: %v", userID, err)
		} else {
			e.decoders[userID] = dec
		}
	}
	e.decodersMu.Unlock()
}

func (e *VoiceEngine) dropIncoming(userID uuid.UUID) {
	e.incomingMu.Lock()
	delete(e.incoming, userID)
	e.incomingMu.Unlock()

	e.decodersMu.Lock()
	delete(e.decoders, userID)
	delete(e.rxSeq, userID)
	e.decodersMu.Unlock()
}

// rmsAmplitude returns RMS of an int16 PCM slice, normalised to [0,1].
func rmsAmplitude(samples []int16) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sum float64
	for _, s := range samples {
		f := float64(s) / 32768.0
		sum += f * f
	}
	return math.Sqrt(sum / float64(len(samples)))
}

func bytesToInt16(b []byte) []int16 {
	out := make([]int16, len(b)/2)
	for i := range out {
		out[i] = int16(binary.LittleEndian.Uint16(b[i*2:]))
	}
	return out
}

func int16ToBytes(s []int16) []byte {
	out := make([]byte, len(s)*2)
	for i, v := range s {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(v))
	}
	return out
}

// ── Connection quality ────────────────────────────────────────────────────────

// pollStats runs every 5 s and emits VoiceQualityMsg for each connected peer
// based on the ICE candidate-pair round-trip time reported by pion.
func (e *VoiceEngine) pollStats() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-e.quit:
			return
		case <-ticker.C:
			e.peersMu.RLock()
			for uid, peer := range e.peers {
				report := peer.pc.GetStats()
				latencyMs := extractRTTMs(report)
				select {
				case e.eventOut <- VoiceQualityMsg{UserID: uid, LatencyMs: latencyMs}:
				default:
				}
			}
			e.peersMu.RUnlock()
		}
	}
}

// pollLevels runs every 50 ms (~20 fps) and emits VoiceLevelMsg for the local
// user and each remote peer.  It applies an asymmetric exponential moving
// average so the bar rises quickly when audio arrives and falls smoothly when
// it stops — giving a polished, non-choppy animation.
//
// Attack alpha 0.6  → bar reaches ~95 % of peak in ≈150 ms
// Decay  alpha 0.88 → bar falls to ~10 % of peak in ≈1 s
func (e *VoiceEngine) pollLevels() {
	const (
		tickInterval = 50 * time.Millisecond
		attackAlpha  = float32(0.6)  // weight of new sample when rising
		decayAlpha   = float32(0.88) // retention factor when falling
		minLevel     = float32(0.001)
	)

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	// smoothed is local to this goroutine — no lock needed.
	smoothed := make(map[uuid.UUID]float32)

	smooth := func(uid uuid.UUID, raw float32) float32 {
		cur := smoothed[uid]
		if raw > cur {
			cur = cur*(1-attackAlpha) + raw*attackAlpha // fast attack
		} else {
			cur = cur * decayAlpha // slow decay
		}
		if cur < minLevel {
			cur = 0
			delete(smoothed, uid)
		} else {
			smoothed[uid] = cur
		}
		return cur
	}

	for {
		select {
		case <-e.quit:
			return
		case <-ticker.C:
			// ── Local mic level ───────────────────────────────────────────────
			// SwapUint32 reads and resets atomically so each tick sees only the
			// freshest frame; if no frames arrived the raw value is 0 (silence).
			rawBits := atomic.SwapUint32(&e.localLevel, 0)
			localRaw := *(*float32)(unsafe.Pointer(&rawBits))
			if lvl := smooth(e.localUserID, localRaw); lvl > 0 || smoothed[e.localUserID] > 0 || localRaw > 0 {
				select {
				case e.eventOut <- VoiceLevelMsg{UserID: e.localUserID, Level: lvl}:
				default:
				}
			}

			// ── Remote peer levels ────────────────────────────────────────────
			e.incomingMu.RLock()
			for uid, buf := range e.incoming {
				buf.mu.Lock()
				raw := buf.lastLevel
				buf.lastLevel = 0 // reset so silence shows up correctly next tick
				buf.mu.Unlock()

				lvl := smooth(uid, raw)
				select {
				case e.eventOut <- VoiceLevelMsg{UserID: uid, Level: lvl}:
				default:
				}
			}
			e.incomingMu.RUnlock()
		}
	}
}

// extractRTTMs scans a pion StatsReport for the nominated ICE candidate-pair
// and returns its current round-trip time in milliseconds. Returns -1 if the
// stats are not yet available (e.g. ICE is still gathering).
func extractRTTMs(report webrtc.StatsReport) int {
	for _, s := range report {
		pair, ok := s.(webrtc.ICECandidatePairStats)
		if !ok || !pair.Nominated {
			continue
		}
		if pair.CurrentRoundTripTime > 0 {
			return int(pair.CurrentRoundTripTime * 1000)
		}
	}
	return -1
}

// ── Device enumeration ────────────────────────────────────────────────────────

// ListDevices enumerates audio input and output devices.
// If the engine is already running it reuses its existing malgo context so that
// no second context is created alongside the active one — creating two concurrent
// WASAPI contexts on Windows causes the second Devices() call to silently return
// an empty list. If the engine has not yet started it falls back to GetAudioDevices.
func (e *VoiceEngine) ListDevices() (inputs, outputs []AudioDevice, err error) {
	e.mu.Lock()
	ctx := e.malgoCtx
	e.mu.Unlock()
	if ctx != nil {
		return enumerateDevicesFromCtx(ctx)
	}
	return GetAudioDevices()
}

// enumerateDevicesFromCtx lists capture and playback devices using an already-open context.
func enumerateDevicesFromCtx(ctx *malgo.AllocatedContext) (inputs, outputs []AudioDevice, err error) {
	capDevs, err := ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate inputs: %w", err)
	}
	for _, d := range capDevs {
		inputs = append(inputs, AudioDevice{ID: d.ID.String(), Name: d.Name()})
	}
	pbDevs, err := ctx.Devices(malgo.Playback)
	if err != nil {
		return nil, nil, fmt.Errorf("enumerate outputs: %w", err)
	}
	for _, d := range pbDevs {
		outputs = append(outputs, AudioDevice{ID: d.ID.String(), Name: d.Name()})
	}
	return inputs, outputs, nil
}

// GetAudioDevices creates a temporary malgo context to enumerate devices.
// Use VoiceEngine.ListDevices() instead when an engine is already running to
// avoid creating two concurrent WASAPI contexts.
func GetAudioDevices() (inputs, outputs []AudioDevice, err error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("malgo init: %w", err)
	}
	if ctx == nil {
		return nil, nil, fmt.Errorf("malgo init: audio backend unavailable on this system")
	}
	defer func() {
		_ = ctx.Uninit()
		ctx.Free()
	}()
	return enumerateDevicesFromCtx(ctx)
}

// isVoiceSupported returns true in the voice build.
func isVoiceSupported() bool { return true }
