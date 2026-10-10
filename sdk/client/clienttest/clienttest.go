// Package clienttest runs a plugin's client code (sdk/client) in ordinary Go
// tests, against a fake Concord that records what it draws, plays, stores
// and sends.
//
//	h := clienttest.New(t, myHandler(), "pane", "storage")
//	h.Start(40, 12)
//	h.Key("enter")
//	if !strings.Contains(h.LastFrame(), "1 point") { ... }
//
// Every event method returns once the client code has handled it. Only one
// Host can run at a time (client code has one Concord), so don't use
// t.Parallel with it.
package clienttest

import (
	"encoding/json"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/client"
	"github.com/JMThomas00/Concord/sdk/wire"
)

// Timeout bounds how long an event may take to be handled.
var Timeout = 5 * time.Second

// Frame is one Frame call.
type Frame struct {
	Text   string
	Images []wire.PaneImage
}

// Sound is one PlaySound call.
type Sound struct {
	Asset  string
	Volume float64
}

// Host is the fake Concord.
type Host struct {
	t    testing.TB
	caps map[string]bool

	events  chan []byte
	ack     chan struct{}
	stop    chan struct{}
	done    chan struct{}
	started bool

	mu       sync.Mutex
	frames   []Frame
	cleared  bool
	forward  bool
	claimed  []string
	sounds   []Sound
	sent     []json.RawMessage
	timers   map[string]time.Duration
	logs     []string
	storage  map[string]string
	width    int
	height   int
	pluginID string
}

// New starts h's client code with the given capabilities granted.
func New(t testing.TB, h client.Handler, capabilities ...string) *Host {
	t.Helper()
	host := &Host{
		t: t, caps: map[string]bool{}, events: make(chan []byte), ack: make(chan struct{}),
		stop: make(chan struct{}), done: make(chan struct{}), forward: true,
		timers: map[string]time.Duration{}, storage: map[string]string{}, pluginID: "test-plugin",
	}
	for _, c := range capabilities {
		host.caps[c] = true
	}
	client.SetTransport(host)
	go func() {
		client.Run(h)
		close(host.done)
	}()
	t.Cleanup(host.Stop)
	return host
}

// Stop ends the client code, as leaving the pane does.
func (h *Host) Stop() {
	select {
	case <-h.stop:
	default:
		close(h.stop)
	}
	select {
	case <-h.done:
	case <-time.After(Timeout):
		h.t.Errorf("clienttest: client code didn't return from Run after stop")
	}
}

func (h *Host) send(e client.Event) {
	h.t.Helper()
	raw, _ := json.Marshal(e)
	select {
	case h.events <- raw:
	case <-h.done:
		h.t.Fatalf("clienttest: client code isn't running (Run returned)")
	case <-time.After(Timeout):
		h.t.Fatalf("clienttest: client code didn't ask for the %s event", e.Type)
	}
	select {
	case <-h.ack:
	case <-h.done:
	case <-time.After(Timeout):
		h.t.Fatalf("clienttest: client code took over %v handling %s", Timeout, e.Type)
	}
}

// Start sends the start event with the pane's size.
func (h *Host) Start(width, height int) {
	h.width, h.height = width, height
	var caps []string
	for c := range h.caps {
		caps = append(caps, c)
	}
	h.send(client.Event{Type: "start", PluginID: h.pluginID, ChannelID: "channel", ViewerID: "viewer",
		Width: width, Height: height, Capabilities: caps})
}

// Key sends a keypress, named as Bubble Tea names it.
func (h *Host) Key(key string) {
	e := client.Event{Type: "key", Key: key}
	if len([]rune(key)) == 1 {
		e.Runes = key
	}
	h.send(e)
}

// Resize sends a new pane size.
func (h *Host) Resize(width, height int) {
	h.width, h.height = width, height
	h.send(client.Event{Type: "resize", Width: width, Height: height})
}

// ServerFrame sends a frame as if from the plugin's server half.
func (h *Host) ServerFrame(text string, images ...wire.PaneImage) {
	h.send(client.Event{Type: "server_frame", Text: text, Images: images})
}

// Server sends data as if from the server half's Conn.SendToClient.
func (h *Host) Server(data any) {
	raw, err := json.Marshal(data)
	if err != nil {
		h.t.Fatal(err)
	}
	h.send(client.Event{Type: "server", Data: raw})
}

// Timer fires a timer the client code set with After (whether or not its
// time has come), and reports whether it had been set.
func (h *Host) Timer(id string) bool {
	h.mu.Lock()
	_, ok := h.timers[id]
	delete(h.timers, id)
	h.mu.Unlock()
	if ok {
		h.send(client.Event{Type: "timer", ID: id})
	}
	return ok
}

// LastFrame is the text of the last Frame drawn ("" if none, or cleared since).
func (h *Host) LastFrame() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cleared || len(h.frames) == 0 {
		return ""
	}
	return h.frames[len(h.frames)-1].Text
}

// Frames returns every Frame call so far.
func (h *Host) Frames() []Frame {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Frame(nil), h.frames...)
}

// Sounds returns every PlaySound call so far.
func (h *Host) Sounds() []Sound {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]Sound(nil), h.sounds...)
}

// Sent returns everything the client code sent its server half.
func (h *Host) Sent() []json.RawMessage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]json.RawMessage(nil), h.sent...)
}

// Timers returns the pending timers and their delays.
func (h *Host) Timers() map[string]time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]time.Duration{}
	for k, v := range h.timers {
		out[k] = v
	}
	return out
}

// Storage returns what the client code has stored.
func (h *Host) Storage() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := map[string]string{}
	for k, v := range h.storage {
		out[k] = v
	}
	return out
}

// Claimed returns the navigation keys the client code claims (ClaimKeys).
func (h *Host) Claimed() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.claimed...)
}

// ForwardingKeys reports the last ForwardKeys setting (true by default).
func (h *Host) ForwardingKeys() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.forward
}

// Logs returns everything logged.
func (h *Host) Logs() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.logs...)
}

// NextEvent implements client.Transport.
func (h *Host) NextEvent() ([]byte, bool) {
	if h.started {
		select {
		case h.ack <- struct{}{}:
		case <-h.stop:
			return nil, false
		}
	}
	h.started = true
	select {
	case e := <-h.events:
		return e, true
	case <-h.stop:
		return nil, false
	}
}

// Call implements client.Transport, with the same rules Concord applies.
func (h *Host) Call(raw []byte) ([]byte, int32) {
	var req struct {
		Fn      string           `json:"fn"`
		Text    string           `json:"text"`
		Images  []wire.PaneImage `json:"images"`
		Forward bool             `json:"forward"`
		ID      string           `json:"id"`
		MS      int64            `json:"ms"`
		Asset   string           `json:"asset"`
		Volume  float64          `json:"volume"`
		Key     string           `json:"key"`
		Value   *string          `json:"value"`
		Data    json.RawMessage  `json:"data"`
		Keys    []string         `json:"keys"`
	}
	if json.Unmarshal(raw, &req) != nil {
		return nil, -1
	}
	need := map[string]string{
		"frame": "pane", "clear_frame": "pane", "forward_keys": "pane", "timer": "pane", "claim_keys": "pane",
		"play_sound": "sound", "storage_get": "storage", "storage_set": "storage", "send_server": "server",
	}
	if c, ok := need[req.Fn]; ok && !h.caps[c] {
		return nil, -2
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	switch req.Fn {
	case "log":
		h.logs = append(h.logs, req.Text)
	case "frame":
		if len(req.Images) > 0 && !h.caps["images"] {
			return nil, -2
		}
		h.frames, h.cleared = append(h.frames, Frame{Text: req.Text, Images: req.Images}), false
	case "clear_frame":
		h.cleared = true
	case "forward_keys":
		h.forward = req.Forward
	case "claim_keys":
		for _, k := range req.Keys {
			if !slices.Contains(wire.PaneNavigationKeys, k) {
				return nil, -1
			}
		}
		h.claimed = append([]string(nil), req.Keys...)
	case "timer":
		if len(h.timers) >= 16 {
			return nil, -4
		}
		h.timers[req.ID] = time.Duration(max(req.MS, 16)) * time.Millisecond
	case "play_sound":
		h.sounds = append(h.sounds, Sound{Asset: req.Asset, Volume: req.Volume})
	case "storage_get":
		v, ok := h.storage[req.Key]
		if !ok {
			return []byte(`{"value":null}`), 0
		}
		out, _ := json.Marshal(map[string]string{"value": v})
		return out, 0
	case "storage_set":
		if len(req.Key) == 0 || len(req.Key) > 256 {
			return nil, -1
		}
		if req.Value == nil {
			delete(h.storage, req.Key)
		} else {
			h.storage[req.Key] = *req.Value
		}
	case "send_server":
		if len(req.Data) > wire.MaxClientMessageBytes {
			return nil, -4
		}
		h.sent = append(h.sent, req.Data)
	default:
		return nil, -3
	}
	return nil, 0
}
