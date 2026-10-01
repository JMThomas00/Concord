// Package plugin connects a program to a Concord server as a plugin.
//
// A plugin is a separate, complete program. Concord launches it with three
// environment variables (see ConfigFromEnv), and it connects back over a
// WebSocket to receive events -- someone opened its pane, pressed a key, a
// setting changed -- and to push rendered frames, notifications and chat
// messages. Run handles the connection (identify, reconnect with backoff,
// ordered delivery); a plugin only implements the Handler callbacks it
// cares about.
//
//	func main() {
//		cfg, ok := plugin.ConfigFromEnv()
//		if !ok {
//			runStandalone() // not launched by Concord: be a normal program
//			return
//		}
//		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
//		defer stop()
//		log.Fatal(plugin.Run(ctx, cfg, plugin.Handler{OnInput: ..., OnEnter: ...}))
//	}
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Config is how to reach the Concord server that launched this plugin.
type Config struct {
	URL      string // CONCORD_WS_URL
	PluginID string // CONCORD_PLUGIN_ID -- this install's [plugin].id
	Token    string // CONCORD_PLUGIN_TOKEN -- valid for this process's lifetime
	DataDir  string // CONCORD_PLUGIN_DATA_DIR -- private folder that survives updates
}

// ConfigFromEnv reads the variables Concord sets when it launches a plugin.
// ok is false when they're absent -- the program was started some other
// way (a terminal, a test) and should behave as a standalone app.
func ConfigFromEnv() (cfg Config, ok bool) {
	cfg = Config{
		URL:      os.Getenv("CONCORD_WS_URL"),
		PluginID: os.Getenv("CONCORD_PLUGIN_ID"),
		Token:    os.Getenv("CONCORD_PLUGIN_TOKEN"),
		DataDir:  os.Getenv("CONCORD_PLUGIN_DATA_DIR"),
	}
	return cfg, cfg.URL != "" && cfg.Token != ""
}

// Handler receives a plugin's events. Every callback is optional, and they
// run one at a time, in arrival order, on a single goroutine -- so handler
// code needs no locking against other callbacks, and a slow one delays
// later events rather than the connection.
type Handler struct {
	// OnReady runs after each successful (re)connect. self is the plugin's
	// service account. Channel, config and Enter events for current
	// viewers follow immediately.
	OnReady func(c *Conn, self *wire.User)

	// Remote panes: see wire.PluginPaneEnterPayload and friends. After a
	// reconnect, Enter is replayed for everyone still viewing.
	OnEnter  func(c *Conn, e wire.PluginPaneEnterPayload)
	OnInput  func(c *Conn, e wire.PluginPaneInputPayload)
	OnResize func(c *Conn, e wire.PluginPaneResizePayload)
	OnLeave  func(c *Conn, e wire.PluginPaneLeavePayload)

	// OnConfig delivers this plugin's server settings (secrets included) on
	// connect and whenever an admin saves them.
	OnConfig func(c *Conn, info wire.PluginInfo)

	// OnChannel reports one of the plugin's channels: each existing one on
	// connect, then any created or edited. PluginConfig holds its
	// create_field values.
	OnChannel       func(c *Conn, ch wire.Channel)
	OnChannelDelete func(c *Conn, e wire.ChannelDeletePayload)

	// OnMessage receives chat messages relayed to the plugin (in channels
	// it owns, or @mentions).
	OnMessage func(c *Conn, m wire.MessageCreatePayload)

	// OnEvent receives any other PLUGIN_EVENT (Members replies are also
	// delivered to the RequestMembers caller that asked).
	OnEvent func(c *Conn, e wire.PluginEventPayload)

	// OnDisconnect runs when the connection drops (Run then reconnects).
	OnDisconnect func(err error)
}

// Conn sends to the Concord server. One Conn lives for the whole Run, across
// reconnects; sends while disconnected return ErrNotConnected.
type Conn struct {
	cfg Config

	mu   sync.Mutex // guards ws; also serializes writes (gorilla requires it)
	ws   *websocket.Conn
	self *wire.User

	seq atomic.Int64 // frame sequence: increases on every frame, so it's monotonic per viewer too

	pendingMu sync.Mutex
	pending   map[string]chan wire.PluginMembersResponse
	posts     map[string]chan uuid.UUID // PostMessage calls waiting for their echo, by nonce
	reqID     atomic.Int64

	postMu sync.Mutex
	post   func(func())
}

// Post runs fn on the handler goroutine, in order with events -- the way
// to hand results of background work (a computer opponent's move, a
// members lookup) back to code that owns plugin state, without locks.
// Safe to call from any goroutine; fn is dropped if Run has stopped.
func (c *Conn) Post(fn func()) {
	c.postMu.Lock()
	post := c.post
	c.postMu.Unlock()
	if post != nil {
		post(fn)
	}
}

// ErrNotConnected is returned by sends while the connection is down.
var ErrNotConnected = errors.New("not connected to Concord")

// PluginID is this install's id.
func (c *Conn) PluginID() string { return c.cfg.PluginID }

// DataDir is this plugin's private data folder (may be "" in tests).
func (c *Conn) DataDir() string { return c.cfg.DataDir }

// Self is the plugin's own service account (nil before the first Ready).
func (c *Conn) Self() *wire.User {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.self
}

// Send writes one message. Most plugins use the helpers below instead.
func (c *Conn) Send(op wire.OpCode, data interface{}) error {
	msg, err := wire.NewMessage(op, data)
	if err != nil {
		return err
	}
	raw, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.ws == nil {
		return ErrNotConnected
	}
	_ = c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
	return c.ws.WriteMessage(websocket.TextMessage, raw)
}

// Frame sends a rendered screen to one viewer. Keep each line within the
// viewer's reported width and the line count within their height; only
// SGR color codes and OSC 8 links survive to the screen.
func (c *Conn) Frame(channelID, viewerID uuid.UUID, frame string) error {
	return c.Send(wire.OpPluginPaneFrame, wire.PluginPaneFramePayload{
		ChannelID: channelID, ViewerID: viewerID, Frame: frame, Seq: c.seq.Add(1),
	})
}

// Broadcast sends the same screen to everyone viewing channelID (a shared
// view, e.g. a terminal passthrough). Games usually render per viewer
// instead, since sizes and perspectives differ.
func (c *Conn) Broadcast(channelID uuid.UUID, frame string) error {
	return c.Frame(channelID, uuid.Nil, frame)
}

// FrameWithImages sends a rendered screen with images placed over it: files
// from the plugin's client/ folder, each in a box of cells. Concord draws
// them the best way each viewer's terminal allows (down to colored
// half-blocks). Keep sensible text under each box: viewers who turned
// images off see it instead.
func (c *Conn) FrameWithImages(channelID, viewerID uuid.UUID, frame string, images []wire.PaneImage) error {
	return c.Send(wire.OpPluginPaneFrame, wire.PluginPaneFramePayload{
		ChannelID: channelID, ViewerID: viewerID, Frame: frame, Seq: c.seq.Add(1), Images: images,
	})
}

// PlaySound plays a sound file (WAV or Ogg/Opus) from the plugin's client/
// folder for one viewer, or for everyone viewing channelID when viewerID
// is uuid.Nil. Volume is 0 to 1 (0 means full); members scale it again
// with their own plugin-sound volume.
func (c *Conn) PlaySound(channelID, viewerID uuid.UUID, asset string, volume float64) error {
	return c.Event(wire.PluginEventPlaySound, viewerID, wire.PluginPlaySoundPayload{ChannelID: channelID, Asset: asset, Volume: volume})
}

// Event sends a generic plugin event.
func (c *Conn) Event(kind string, viewerID uuid.UUID, payload interface{}) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.Send(wire.OpPluginEvent, wire.PluginEventPayload{PluginID: c.cfg.PluginID, Kind: kind, Payload: raw, ViewerID: viewerID})
}

// Notify posts a line in the plugin's activity channel, if an admin set one.
func (c *Conn) Notify(text string) error {
	return c.Event(wire.PluginEventNotify, uuid.Nil, wire.PluginNotifyEventPayload{Content: text})
}

// NotifyUser shows one member a toast and badges channelID for them --
// e.g. "Alex challenged you". Dropped if they can't see the channel.
func (c *Conn) NotifyUser(userID, channelID uuid.UUID, text string) error {
	return c.Event(wire.PluginEventNotifyUser, uuid.Nil, wire.PluginNotifyUserPayload{UserID: userID, ChannelID: channelID, Content: text})
}

// SetTitle sets the pane's border title for one viewer, or everyone with
// viewerID uuid.Nil. "" restores the channel name.
func (c *Conn) SetTitle(channelID, viewerID uuid.UUID, title string) error {
	return c.Event(wire.PluginEventPaneTitle, viewerID, wire.PluginPaneTitlePayload{ChannelID: channelID, Title: title})
}

// LeavePane hands a viewer's keyboard back to Concord (the pane stays open
// and keeps updating). Viewers can always do this themselves with Ctrl+].
func (c *Conn) LeavePane(channelID, viewerID uuid.UUID) error {
	return c.Event(wire.PluginEventLeavePane, viewerID, wire.PluginPaneClosePayload{ChannelID: channelID})
}

// SendMessage posts a chat message as the plugin.
func (c *Conn) SendMessage(channelID uuid.UUID, content string, replyTo *uuid.UUID) error {
	return c.Send(wire.OpSendMessage, wire.SendMessagePayload{ChannelID: channelID, Content: content, ReplyToID: replyTo})
}

// Typing shows or clears "typing…" for the plugin in channelID.
func (c *Conn) Typing(channelID uuid.UUID, on bool) error {
	op := wire.OpTypingStop
	if on {
		op = wire.OpTypingStart
	}
	return c.Send(op, wire.TypingPayload{ChannelID: channelID})
}

// RequestMembers asks who can see channelID -- online or not, viewing or
// not -- and waits for the answer. Call it from a goroutine rather than
// directly inside a Handler callback: the reply arrives through the same
// ordered event stream the callback is blocking.
func (c *Conn) RequestMembers(ctx context.Context, channelID uuid.UUID) ([]wire.PluginMember, error) {
	id := fmt.Sprintf("m%d", c.reqID.Add(1))
	ch := make(chan wire.PluginMembersResponse, 1)
	c.pendingMu.Lock()
	if c.pending == nil {
		c.pending = map[string]chan wire.PluginMembersResponse{}
	}
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()
	if err := c.Event(wire.PluginEventMembers, uuid.Nil, wire.PluginMembersRequest{ChannelID: channelID, RequestID: id}); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		return resp.Members, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// answerMembers hands a members reply to its waiting RequestMembers call;
// it runs on the read loop, not the handler goroutine.
func (c *Conn) answerMembers(e wire.PluginEventPayload) {
	var resp wire.PluginMembersResponse
	if json.Unmarshal(e.Payload, &resp) != nil {
		return
	}
	c.pendingMu.Lock()
	ch := c.pending[resp.RequestID]
	c.pendingMu.Unlock()
	if ch != nil {
		select {
		case ch <- resp:
		default:
		}
	}
}

// Run connects to Concord and delivers events to h until ctx is done,
// reconnecting (with backoff up to 30s) whenever the connection drops. It
// returns ctx.Err() on shutdown, or an error if the server rejects the
// plugin's token -- which a reconnect can't fix.
func Run(ctx context.Context, cfg Config, h Handler) error {
	c := &Conn{cfg: cfg}
	return c.run(ctx, h)
}

// ErrRejected means Concord refused the plugin's token (it was rotated by
// a restart, or the plugin was uninstalled). Exit; Concord relaunches
// plugins it still wants running, with a fresh token.
var ErrRejected = errors.New("Concord rejected this plugin's token")

func (c *Conn) run(ctx context.Context, h Handler) error {
	// Run returns only after the callback in progress (if any) finishes, so
	// a plugin exiting after Run never cuts one short mid-save. The handler
	// goroutine stops on this cancel, whichever way Run ends.
	ctx, cancel := context.WithCancel(ctx)
	events := make(chan func(), 1024)
	handlersDone := make(chan struct{})
	defer func() { cancel(); <-handlersDone }()
	go func() {
		defer close(handlersDone)
		for {
			select {
			case fn := <-events:
				fn()
			case <-ctx.Done():
				return
			}
		}
	}()
	dispatch := func(fn func()) {
		select {
		case events <- fn:
		case <-ctx.Done():
		}
	}
	c.postMu.Lock()
	c.post = dispatch
	c.postMu.Unlock()

	backoff := time.Second
	for {
		connected := time.Now()
		err := c.session(ctx, h, dispatch)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, ErrRejected) {
			return err
		}
		if h.OnDisconnect != nil {
			dispatch(func() { h.OnDisconnect(err) })
		}
		if time.Since(connected) > time.Minute {
			backoff = time.Second
		}
		log.Printf("concord: connection lost (%v); reconnecting in %v", err, backoff)
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			return ctx.Err()
		}
		if backoff *= 2; backoff > 30*time.Second {
			backoff = 30 * time.Second
		}
	}
}

// session is one connection: dial, identify, read until it fails.
func (c *Conn) session(ctx context.Context, h Handler, dispatch func(func())) error {
	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	ws, _, err := dialer.DialContext(ctx, c.cfg.URL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", c.cfg.URL, err)
	}
	stop := context.AfterFunc(ctx, func() { ws.Close() })
	defer stop()
	defer func() {
		c.mu.Lock()
		if c.ws == ws {
			c.ws = nil
		}
		c.mu.Unlock()
		ws.Close()
	}()

	read := func() (*wire.Message, error) {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return nil, err
		}
		var m wire.Message
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("bad message from server: %w", err)
		}
		return &m, nil
	}

	c.mu.Lock()
	c.ws = ws
	c.mu.Unlock()
	if err := c.Send(wire.OpIdentify, wire.IdentifyPayload{Token: c.cfg.Token, ClientType: wire.ClientTypePlugin}); err != nil {
		return err
	}
	// Hello arrives first, whenever the socket opens; wait past it for Ready.
	for i := 0; ; i++ {
		if i == 10 {
			return errors.New("no Ready from the server")
		}
		m, err := read()
		if err != nil {
			return err
		}
		if m.Op == wire.OpInvalid {
			return ErrRejected
		}
		if m.Op == wire.OpReady {
			var ready wire.ReadyPayload
			_ = json.Unmarshal(m.Data, &ready)
			c.mu.Lock()
			c.self = ready.User
			c.mu.Unlock()
			if h.OnReady != nil {
				dispatch(func() { h.OnReady(c, ready.User) })
			}
			break
		}
	}

	for {
		m, err := read()
		if err != nil {
			return err
		}
		if m.Op != wire.OpDispatch {
			continue
		}
		c.route(m, h, dispatch)
	}
}

// route decodes one dispatch and queues its handler.
func (c *Conn) route(m *wire.Message, h Handler, dispatch func(func())) {
	decode := func(v interface{}) bool {
		if err := json.Unmarshal(m.Data, v); err != nil {
			log.Printf("concord: can't decode %s: %v", m.Type, err)
			return false
		}
		return true
	}
	switch m.Type {
	case wire.EventPluginPaneEnter:
		var e wire.PluginPaneEnterPayload
		if h.OnEnter != nil && decode(&e) {
			dispatch(func() { h.OnEnter(c, e) })
		}
	case wire.EventPluginPaneInput:
		var e wire.PluginPaneInputPayload
		if h.OnInput != nil && decode(&e) {
			dispatch(func() { h.OnInput(c, e) })
		}
	case wire.EventPluginPaneResize:
		var e wire.PluginPaneResizePayload
		if h.OnResize != nil && decode(&e) {
			dispatch(func() { h.OnResize(c, e) })
		}
	case wire.EventPluginPaneLeave:
		var e wire.PluginPaneLeavePayload
		if h.OnLeave != nil && decode(&e) {
			dispatch(func() { h.OnLeave(c, e) })
		}
	case wire.EventPluginConfigUpdate:
		var p wire.PluginConfigListPayload
		if h.OnConfig != nil && decode(&p) {
			for _, info := range p.Plugins {
				if info.ID == c.cfg.PluginID || c.cfg.PluginID == "" {
					info := info
					dispatch(func() { h.OnConfig(c, info) })
				}
			}
		}
	case wire.EventChannelCreate:
		var p wire.ChannelCreatePayload
		if h.OnChannel != nil && decode(&p) && p.Channel != nil {
			ch := *p.Channel
			if len(p.PluginConfig) > 0 {
				ch.PluginConfig = p.PluginConfig
			}
			dispatch(func() { h.OnChannel(c, ch) })
		}
	case wire.EventChannelUpdate:
		var p wire.ChannelUpdatePayload
		if h.OnChannel != nil && decode(&p) && p.Channel != nil {
			ch := *p.Channel
			dispatch(func() { h.OnChannel(c, ch) })
		}
	case wire.EventChannelDelete:
		var e wire.ChannelDeletePayload
		if h.OnChannelDelete != nil && decode(&e) {
			dispatch(func() { h.OnChannelDelete(c, e) })
		}
	case wire.EventMessageCreate:
		var p wire.MessageCreatePayload
		if !decode(&p) || p.ChatMessage == nil || c.answerPost(p) {
			return // the echo of our own PostMessage: its caller has it
		}
		if h.OnMessage != nil {
			dispatch(func() { h.OnMessage(c, p) })
		}
	case wire.EventPluginEvent:
		var e wire.PluginEventPayload
		if !decode(&e) {
			return
		}
		if e.Kind == wire.PluginEventMembers {
			c.answerMembers(e)
		}
		if h.OnEvent != nil {
			dispatch(func() { h.OnEvent(c, e) })
		}
	case "":
		var e wire.ErrorPayload
		if json.Unmarshal(m.Data, &e) == nil && e.Message != "" {
			log.Printf("concord: server error %d: %s", e.Code, e.Message)
		}
	}
}
