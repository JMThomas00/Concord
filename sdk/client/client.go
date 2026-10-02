// Package client is the SDK for a plugin's client code: WebAssembly that
// runs inside each viewer's Concord client, next to the plugin's pane.
//
// Client code is optional. A plugin's server half (sdk/plugin) does all the
// work for most plugins. Client code is for what a server can't do well:
// instant feedback on keys, animation, drawing ahead of the server's next
// frame, sounds timed to the viewer's own actions, and remembering things on
// the viewer's computer.
//
// It runs sandboxed: no files, no network, no other programs. It can only
// do what the plugin's manifest asks for and the viewer agrees to:
//
//	[client]
//	wasm = "client/plugin.wasm"
//	capabilities = ["pane", "images", "sound", "storage", "server"]
//	publisher_key = "ed25519:..."   # concord-plugin keygen
//
// Build it with GOOS=wasip1 GOARCH=wasm, then sign it with
// `concord-plugin sign client/plugin.wasm`; Concord won't run unsigned code.
//
//	func main() {
//		client.Run(client.Handler{
//			OnStart: func(e client.Event) { draw(e.Width, e.Height) },
//			OnKey:   func(e client.Event) { ... client.Frame(view()) },
//		})
//	}
//
// Events arrive one at a time, in order, on the goroutine that called Run.
// Each must be handled within Concord's time budget (2 seconds); code that
// takes longer is stopped. Use After for anything periodic.
package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/JMThomas00/Concord/sdk/wire"
)

// Event is something Concord tells the client code. Which fields are set
// depends on Type.
type Event struct {
	// Type is "start" (once, first), "key", "resize", "server_frame",
	// "server" or "timer".
	Type string `json:"type"`

	// start
	PluginID     string   `json:"plugin_id,omitempty"`
	ChannelID    string   `json:"channel_id,omitempty"`
	ViewerID     string   `json:"viewer_id,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`

	// start, resize: the pane's size in cells and the viewer's theme.
	Width  int             `json:"width,omitempty"`
	Height int             `json:"height,omitempty"`
	Theme  *wire.PaneTheme `json:"theme,omitempty"`

	// key: the key as Bubble Tea names it ("a", "enter", "ctrl+c"), and
	// the characters typed, if any.
	Key   string `json:"key,omitempty"`
	Runes string `json:"runes,omitempty"`

	// server_frame: the frame the plugin's server half sent this viewer.
	Text   string           `json:"text,omitempty"`
	Images []wire.PaneImage `json:"images,omitempty"`

	// server: what the plugin's server half sent with Conn.SendToClient.
	Data json.RawMessage `json:"data,omitempty"`

	// timer: the id given to After.
	ID string `json:"id,omitempty"`
}

// Handler holds the client code's callbacks. All are optional.
type Handler struct {
	OnStart  func(e Event)
	OnKey    func(e Event)
	OnResize func(e Event)
	// OnServerFrame sees each frame the server half sends this viewer. While
	// the client code has drawn its own frame (Frame), the viewer sees that
	// instead; ClearFrame shows the server's again.
	OnServerFrame func(e Event)
	OnServer      func(data json.RawMessage)
	OnTimer       func(id string)
}

// Run delivers events to h until Concord stops the client code (the viewer
// left the pane), then returns. Call it once, from main.
func Run(h Handler) {
	for {
		raw, ok := transport.NextEvent()
		if !ok {
			return
		}
		var e Event
		if json.Unmarshal(raw, &e) != nil {
			continue
		}
		switch e.Type {
		case "start":
			call(h.OnStart, e)
		case "key":
			call(h.OnKey, e)
		case "resize":
			call(h.OnResize, e)
		case "server_frame":
			call(h.OnServerFrame, e)
		case "server":
			if h.OnServer != nil {
				h.OnServer(e.Data)
			}
		case "timer":
			if h.OnTimer != nil {
				h.OnTimer(e.ID)
			}
		}
	}
}

func call(f func(Event), e Event) {
	if f != nil {
		f(e)
	}
}

// Errors a call can return.
var (
	ErrBadRequest  = errors.New("concord: bad request")
	ErrNotAllowed  = errors.New("concord: capability not granted")
	ErrUnknown     = errors.New("concord: unknown function")
	ErrLimit       = errors.New("concord: over a limit")
	ErrNotFound    = errors.New("concord: not found")
	ErrUnavailable = errors.New("concord: not running inside Concord")
)

// errorFor maps a negative call result to its error.
func errorFor(code int32) error {
	switch code {
	case -1:
		return ErrBadRequest
	case -2:
		return ErrNotAllowed
	case -3:
		return ErrUnknown
	case -4:
		return ErrLimit
	case -5:
		return ErrNotFound
	case -6:
		return ErrUnavailable
	}
	return fmt.Errorf("concord: call failed (%d)", code)
}

// invoke makes one host call: req is marshalled to JSON with "fn" set, and
// the result (if any) is unmarshalled into out.
func invoke(fn string, req map[string]any, out any) error {
	if req == nil {
		req = map[string]any{}
	}
	req["fn"] = fn
	raw, err := json.Marshal(req)
	if err != nil {
		return err
	}
	resp, code := transport.Call(raw)
	if code < 0 {
		return errorFor(code)
	}
	if out != nil && len(resp) > 0 {
		return json.Unmarshal(resp, out)
	}
	return nil
}

// Frame draws the pane on this viewer's screen, replacing the server
// half's frames until ClearFrame. Text follows the same rules as a server
// frame (SGR colors only, at most the pane's size); images need the
// "images" capability. Needs "pane".
func Frame(text string, images ...wire.PaneImage) error {
	return invoke("frame", map[string]any{"text": text, "images": images}, nil)
}

// ClearFrame goes back to showing the server half's frames. Needs "pane".
func ClearFrame() error { return invoke("clear_frame", nil, nil) }

// ForwardKeys says whether this viewer's keys also go to the server half
// (the default) or only to the client code. Needs "pane".
func ForwardKeys(on bool) error {
	return invoke("forward_keys", map[string]any{"forward": on}, nil)
}

// ClaimKeys takes Esc, Tab or Shift+Tab (wire.PaneKeyEsc...) for the client
// code while its frame is on screen; with no keys, it gives them back.
// Unclaimed, Concord uses them to move focus out of the pane, as in every
// other channel (Esc and Shift+Tab to the channels, Tab to the members).
// Claim them only while they mean something here, such as Esc while there's
// a selection to cancel. Ctrl+] always leaves. Needs "pane".
func ClaimKeys(keys ...string) error {
	if keys == nil {
		keys = []string{}
	}
	return invoke("claim_keys", map[string]any{"keys": keys}, nil)
}

// After asks for a "timer" event with this id after d (at least 16ms; at
// most 16 pending at once). Needs "pane".
func After(id string, d time.Duration) error {
	return invoke("timer", map[string]any{"id": id, "ms": d.Milliseconds()}, nil)
}

// PlaySound plays a sound from the plugin's client/ folder for this viewer.
// Volume is 0 to 1 (0 means full). Needs "sound".
func PlaySound(asset string, volume float64) error {
	return invoke("play_sound", map[string]any{"asset": asset, "volume": volume}, nil)
}

// Get reads a value the client code stored on this viewer's computer.
// Needs "storage".
func Get(key string) (value string, ok bool, err error) {
	var out struct {
		Value *string `json:"value"`
	}
	if err := invoke("storage_get", map[string]any{"key": key}, &out); err != nil {
		return "", false, err
	}
	if out.Value == nil {
		return "", false, nil
	}
	return *out.Value, true, nil
}

// Set stores a value on this viewer's computer: keys up to 256 bytes, and
// 1 MB per plugin in total. Needs "storage".
func Set(key, value string) error {
	return invoke("storage_set", map[string]any{"key": key, "value": value}, nil)
}

// Delete removes a stored value. Needs "storage".
func Delete(key string) error {
	return invoke("storage_set", map[string]any{"key": key, "value": nil}, nil)
}

// Send sends data (anything that marshals to JSON, at most
// wire.MaxClientMessageBytes) to the plugin's server half, which gets it in
// Handler.OnClientMessage. Needs "server".
func Send(data any) error {
	return invoke("send_server", map[string]any{"data": data}, nil)
}

// Log writes a line to the viewer's Concord log, for debugging.
func Log(format string, args ...any) {
	_ = invoke("log", map[string]any{"text": fmt.Sprintf(format, args...)}, nil)
}

// Transport carries events and calls between the client code and Concord.
// Inside Concord it's the WebAssembly host interface; clienttest replaces
// it to run client code in ordinary Go tests.
type Transport interface {
	// NextEvent blocks for the next event's JSON; false means stop.
	NextEvent() ([]byte, bool)
	// Call makes a host call. code < 0 is an error (see the Err values).
	Call(req []byte) (resp []byte, code int32)
}

var transport Transport = hostTransport()

// SetTransport replaces how the client code reaches Concord (for tests).
func SetTransport(t Transport) { transport = t }
