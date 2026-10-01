// Package plugintest is a fake Concord server for testing plugins without
// running Concord: point plugin.Run at Server.Config(), then act as viewers
// (Enter, Key, Resize, Leave), hand the plugin channels and settings, and
// read back the frames and events it sends.
//
//	srv := plugintest.NewServer(t)
//	go plugin.Run(ctx, srv.Config(), myHandler)
//	srv.WaitReady()
//	alice := srv.Enter(channelID, "alice", 80, 24)
//	srv.Key(alice, "enter")
//	frame := srv.NextFrame(alice) // what alice now sees
package plugintest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// Token is the token Server accepts.
const Token = "plugintest-token"

// Timeout bounds every wait (NextFrame, NextEvent, WaitReady...).
var Timeout = 5 * time.Second

// Server is a fake Concord server that one plugin connection talks to.
type Server struct {
	t    testing.TB
	http *httptest.Server
	Self wire.User // the plugin's service account, sent in Ready

	mu      sync.Mutex
	conn    *websocket.Conn
	ready   chan struct{}
	frames  map[uuid.UUID]chan wire.PluginPaneFramePayload // by viewer; uuid.Nil = broadcasts
	events  chan wire.PluginEventPayload
	chat    chan wire.SendMessagePayload
	edits   chan wire.EditMessagePayload
	posted  map[uuid.UUID]*Posted // the plugin's messages, by ID
	order   []uuid.UUID
	users   map[string]uuid.UUID // ChatMessage authors, by name
	viewers map[uuid.UUID]*Viewer
}

// Viewer is someone with one of the plugin's panes open.
type Viewer struct {
	ID          uuid.UUID
	Name        string
	ChannelID   uuid.UUID
	Width       int
	Height      int
	LastFrame   string // the most recent frame this viewer received
	LastImages  []wire.PaneImage // the images placed on that frame
	LastSeq     int64
	frameEpochs int64
}

// NewServer starts a fake server; it's shut down when the test ends.
func NewServer(t testing.TB) *Server {
	t.Helper()
	s := &Server{
		t:       t,
		Self:    wire.User{ID: uuid.New(), Username: "plugin", Discriminator: "0000"},
		ready:   make(chan struct{}),
		frames:  map[uuid.UUID]chan wire.PluginPaneFramePayload{},
		events:  make(chan wire.PluginEventPayload, 256),
		chat:    make(chan wire.SendMessagePayload, 256),
		edits:   make(chan wire.EditMessagePayload, 1024),
		posted:  map[uuid.UUID]*Posted{},
		users:   map[string]uuid.UUID{},
		viewers: map[uuid.UUID]*Viewer{},
	}
	s.http = httptest.NewServer(http.HandlerFunc(s.serveWS))
	t.Cleanup(s.Close)
	return s
}

// Config is what to pass to plugin.Run.
func (s *Server) Config() plugin.Config {
	return plugin.Config{
		URL:      "ws" + strings.TrimPrefix(s.http.URL, "http") + "/ws",
		PluginID: "plugintest",
		Token:    Token,
		DataDir:  s.t.TempDir(),
	}
}

// Close shuts the server down.
func (s *Server) Close() {
	s.mu.Lock()
	if s.conn != nil {
		s.conn.Close()
	}
	s.mu.Unlock()
	s.http.Close()
}

// DropConnection closes the plugin's connection (it should reconnect).
func (s *Server) DropConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.conn != nil {
		s.conn.Close()
		s.conn = nil
	}
	s.ready = make(chan struct{})
}

func (s *Server) serveWS(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.writeTo(ws, wire.OpHello, "", wire.HelloPayload{HeartbeatInterval: 45000})
	for {
		_, data, err := ws.ReadMessage()
		if err != nil {
			return
		}
		var m wire.Message
		if json.Unmarshal(data, &m) != nil {
			continue
		}
		s.handle(ws, &m)
	}
}

func (s *Server) handle(ws *websocket.Conn, m *wire.Message) {
	switch m.Op {
	case wire.OpIdentify:
		var id wire.IdentifyPayload
		_ = json.Unmarshal(m.Data, &id)
		if id.Token != Token || id.ClientType != wire.ClientTypePlugin {
			s.writeTo(ws, wire.OpInvalid, "", nil)
			return
		}
		s.mu.Lock()
		s.conn = ws
		ready := s.ready
		viewers := make([]*Viewer, 0, len(s.viewers))
		for _, v := range s.viewers {
			viewers = append(viewers, v)
		}
		s.mu.Unlock()
		s.writeTo(ws, wire.OpReady, "", wire.ReadyPayload{SessionID: uuid.NewString(), User: &s.Self})
		for _, v := range viewers { // like Concord: replay Enter for current viewers
			s.dispatch(wire.EventPluginPaneEnter, s.enterPayload(v))
		}
		select {
		case <-ready:
		default:
			close(ready)
		}
	case wire.OpPluginPaneFrame:
		var f wire.PluginPaneFramePayload
		if json.Unmarshal(m.Data, &f) == nil {
			s.deliverFrame(f)
		}
	case wire.OpPluginEvent:
		var e wire.PluginEventPayload
		if json.Unmarshal(m.Data, &e) == nil {
			s.events <- e
		}
	case wire.OpSendMessage:
		var p wire.SendMessagePayload
		if json.Unmarshal(m.Data, &p) == nil {
			s.post(ws, p)
		}
	case wire.OpEditMessage:
		var e wire.EditMessagePayload
		if json.Unmarshal(m.Data, &e) == nil {
			s.edit(e)
		}
	}
}

func (s *Server) deliverFrame(f wire.PluginPaneFramePayload) {
	s.mu.Lock()
	var targets []uuid.UUID
	if f.ViewerID == uuid.Nil {
		for id, v := range s.viewers {
			if v.ChannelID == f.ChannelID {
				targets = append(targets, id)
			}
		}
	} else if v, ok := s.viewers[f.ViewerID]; ok && v.ChannelID == f.ChannelID {
		targets = []uuid.UUID{f.ViewerID}
	} else {
		s.mu.Unlock()
		s.t.Errorf("plugintest: plugin sent a frame to %s, who isn't viewing channel %s", f.ViewerID, f.ChannelID)
		return
	}
	for _, id := range targets {
		v := s.viewers[id]
		if f.Seq <= v.LastSeq {
			s.t.Errorf("plugintest: frame Seq %d for %s isn't above the previous %d; Concord's client would drop it", f.Seq, v.Name, v.LastSeq)
		}
		v.LastSeq, v.LastFrame = f.Seq, f.Frame
		v.LastImages = f.Images
		for _, im := range f.Images {
			if im.Col < 0 || im.Row < 0 || im.Cols <= 0 || im.Rows <= 0 || im.Col+im.Cols > v.Width || im.Row+im.Rows > v.Height {
				s.t.Errorf("plugintest: image %q at %d,%d (%dx%d cells) doesn't fit %s's %dx%d pane", im.Asset, im.Col, im.Row, im.Cols, im.Rows, v.Name, v.Width, v.Height)
			}
		}
		lines := strings.Split(f.Frame, "\n")
		if len(lines) > v.Height {
			s.t.Errorf("plugintest: frame for %s is %d lines; their pane is %d tall", v.Name, len(lines), v.Height)
		}
		out := f
		out.ViewerID = id
		s.frameChan(id) <- out
	}
	s.mu.Unlock()
}

// frameChan must be called with s.mu held.
func (s *Server) frameChan(viewer uuid.UUID) chan wire.PluginPaneFramePayload {
	ch, ok := s.frames[viewer]
	if !ok {
		ch = make(chan wire.PluginPaneFramePayload, 1024)
		s.frames[viewer] = ch
	}
	return ch
}

func (s *Server) writeTo(ws *websocket.Conn, op wire.OpCode, event wire.EventType, data interface{}) {
	msg, err := wire.NewMessage(op, data)
	if err != nil {
		s.t.Fatalf("plugintest: %v", err)
	}
	msg.Type = event
	raw, _ := json.Marshal(msg)
	s.mu.Lock()
	defer s.mu.Unlock()
	_ = ws.WriteMessage(websocket.TextMessage, raw)
}

// dispatch sends an event to the plugin, if it's connected.
func (s *Server) dispatch(event wire.EventType, data interface{}) {
	s.mu.Lock()
	ws := s.conn
	s.mu.Unlock()
	if ws == nil {
		s.t.Fatalf("plugintest: sending %s, but the plugin isn't connected (call WaitReady first)", event)
	}
	s.writeTo(ws, wire.OpDispatch, event, data)
}

// WaitReady blocks until the plugin has connected and identified.
func (s *Server) WaitReady() {
	s.t.Helper()
	s.mu.Lock()
	ready := s.ready
	s.mu.Unlock()
	select {
	case <-ready:
	case <-time.After(Timeout):
		s.t.Fatal("plugintest: the plugin never connected and identified")
	}
}

func (s *Server) enterPayload(v *Viewer) wire.PluginPaneEnterPayload {
	return wire.PluginPaneEnterPayload{
		ChannelID: v.ChannelID, ViewerID: v.ID, Width: v.Width, Height: v.Height,
		ViewerName: v.Name, ViewerDisplayName: v.Name,
		Theme: &wire.PaneTheme{Name: "plugintest", ColorProfile: "ansi256"},
	}
}

// Enter opens channelID's pane as a new viewer called name.
func (s *Server) Enter(channelID uuid.UUID, name string, width, height int) *Viewer {
	v := &Viewer{ID: uuid.New(), Name: name, ChannelID: channelID, Width: width, Height: height}
	s.mu.Lock()
	s.viewers[v.ID] = v
	s.frameChan(v.ID)
	s.mu.Unlock()
	s.dispatch(wire.EventPluginPaneEnter, s.enterPayload(v))
	return v
}

// Key sends one keypress from v, in Bubble Tea's KeyMsg.String() form:
// "a", "enter", "up", "ctrl+c", "alt+x", " " (space)...
func (s *Server) Key(v *Viewer, key string) {
	in := wire.PluginPaneInputPayload{ChannelID: v.ChannelID, ViewerID: v.ID, KeyString: key, ViewerName: v.Name, ViewerDisplayName: v.Name}
	if r := []rune(key); len(r) == 1 {
		in.Runes = r
	}
	s.dispatch(wire.EventPluginPaneInput, in)
}

// Type sends each character of text from v as its own keypress.
func (s *Server) Type(v *Viewer, text string) {
	for _, r := range text {
		s.Key(v, string(r))
	}
}

// Resize changes v's pane size.
func (s *Server) Resize(v *Viewer, width, height int) {
	s.mu.Lock()
	v.Width, v.Height = width, height
	s.mu.Unlock()
	s.dispatch(wire.EventPluginPaneResize, wire.PluginPaneResizePayload{
		ChannelID: v.ChannelID, ViewerID: v.ID, Width: width, Height: height, ViewerName: v.Name, ViewerDisplayName: v.Name,
	})
}

// Leave closes v's pane.
func (s *Server) Leave(v *Viewer) {
	s.mu.Lock()
	delete(s.viewers, v.ID)
	s.mu.Unlock()
	s.dispatch(wire.EventPluginPaneLeave, wire.PluginPaneLeavePayload{ChannelID: v.ChannelID, ViewerID: v.ID})
}

// NextFrame waits for the next frame sent to v and returns it.
func (s *Server) NextFrame(v *Viewer) string {
	s.t.Helper()
	s.mu.Lock()
	ch := s.frameChan(v.ID)
	s.mu.Unlock()
	select {
	case f := <-ch:
		return f.Frame
	case <-time.After(Timeout):
		s.t.Fatalf("plugintest: no frame for %s within %v", v.Name, Timeout)
		return ""
	}
}

// FrameContaining waits until v receives a frame containing want (skipping
// earlier ones) and returns it.
func (s *Server) FrameContaining(v *Viewer, want string) string {
	s.t.Helper()
	deadline := time.After(Timeout)
	s.mu.Lock()
	ch := s.frameChan(v.ID)
	s.mu.Unlock()
	for {
		select {
		case f := <-ch:
			if strings.Contains(f.Frame, want) {
				return f.Frame
			}
		case <-deadline:
			s.mu.Lock()
			last := v.LastFrame
			s.mu.Unlock()
			s.t.Fatalf("plugintest: %s never saw %q; last frame:\n%s", v.Name, want, last)
			return ""
		}
	}
}

// NextEvent waits for the next plugin event (notify, notify_user,
// pane_title, leave_pane, members request, or a custom kind).
func (s *Server) NextEvent() wire.PluginEventPayload {
	s.t.Helper()
	select {
	case e := <-s.events:
		return e
	case <-time.After(Timeout):
		s.t.Fatalf("plugintest: no plugin event within %v", Timeout)
		return wire.PluginEventPayload{}
	}
}

// NextChat waits for the next chat message the plugin posts.
func (s *Server) NextChat() wire.SendMessagePayload {
	s.t.Helper()
	select {
	case m := <-s.chat:
		return m
	case <-time.After(Timeout):
		s.t.Fatalf("plugintest: no chat message within %v", Timeout)
		return wire.SendMessagePayload{}
	}
}

// Channel tells the plugin about one of its channels (as Concord does for
// each on connect, and on create/edit).
func (s *Server) Channel(ch wire.Channel) {
	if ch.Type == 0 {
		ch.Type = wire.ChannelTypePlugin
	}
	s.dispatch(wire.EventChannelUpdate, wire.ChannelUpdatePayload{Channel: &ch})
}

// DeleteChannel tells the plugin one of its channels was deleted.
func (s *Server) DeleteChannel(id uuid.UUID) {
	s.dispatch(wire.EventChannelDelete, wire.ChannelDeletePayload{ChannelID: id, Type: wire.ChannelTypePlugin})
}

// Settings sends the plugin its server settings.
func (s *Server) Settings(values map[string]string) {
	s.dispatch(wire.EventPluginConfigUpdate, wire.PluginConfigListPayload{Plugins: []wire.PluginInfo{{
		ID: "plugintest", Name: "plugintest", Enabled: true, Status: "running", ConfigValues: values,
	}}})
}

// ChatMessage relays a chat message to the plugin.
func (s *Server) ChatMessage(channelID uuid.UUID, author, content string) {
	s.dispatch(wire.EventMessageCreate, wire.MessageCreatePayload{
		ChatMessage: &wire.ChatMessage{ID: uuid.New(), ChannelID: channelID, AuthorID: s.UserID(author), Content: content},
		Author:      &wire.User{ID: s.UserID(author), Username: author},
	})
}

// UserID is the user ID ChatMessage uses for author: the same for every
// message by that name, so per-user logic (rate limits, turn order) can be
// tested.
func (s *Server) UserID(author string) uuid.UUID {
	s.mu.Lock()
	defer s.mu.Unlock()
	id, ok := s.users[author]
	if !ok {
		id = uuid.New()
		s.users[author] = id
	}
	return id
}

// AnswerMembers replies to a members request event with the given list.
func (s *Server) AnswerMembers(req wire.PluginEventPayload, members []wire.PluginMember) {
	var r wire.PluginMembersRequest
	_ = json.Unmarshal(req.Payload, &r)
	raw, _ := json.Marshal(wire.PluginMembersResponse{ChannelID: r.ChannelID, RequestID: r.RequestID, Members: members})
	s.dispatch(wire.EventPluginEvent, wire.PluginEventPayload{PluginID: "plugintest", Kind: wire.PluginEventMembers, Payload: raw})
}

// Event delivers a PLUGIN_EVENT of the given kind to the plugin (Handler
// OnEvent) -- e.g. to trigger plugin-side work from a test.
func (s *Server) Event(kind string, payload interface{}) {
	raw, _ := json.Marshal(payload)
	s.dispatch(wire.EventPluginEvent, wire.PluginEventPayload{PluginID: "plugintest", Kind: kind, Payload: raw})
}

// EnterAs is Enter for a specific user ID -- e.g. the member a plugin was
// told about in a members reply.
func (s *Server) EnterAs(channelID, userID uuid.UUID, name string, width, height int) *Viewer {
	v := &Viewer{ID: userID, Name: name, ChannelID: channelID, Width: width, Height: height}
	s.mu.Lock()
	s.viewers[v.ID] = v
	s.frameChan(v.ID)
	s.mu.Unlock()
	s.dispatch(wire.EventPluginPaneEnter, s.enterPayload(v))
	return v
}

// Images returns the images placed on the last frame v received.
func (s *Server) Images(v *Viewer) []wire.PaneImage {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]wire.PaneImage(nil), v.LastImages...)
}

// NextSound waits for the plugin to play a sound (skipping other events)
// and returns it, with ViewerID uuid.Nil when it's for everyone viewing.
func (s *Server) NextSound() (wire.PluginPlaySoundPayload, uuid.UUID) {
	s.t.Helper()
	deadline := time.After(Timeout)
	for {
		select {
		case e := <-s.events:
			if e.Kind != wire.PluginEventPlaySound {
				continue
			}
			var p wire.PluginPlaySoundPayload
			_ = json.Unmarshal(e.Payload, &p)
			return p, e.ViewerID
		case <-deadline:
			s.t.Fatalf("plugintest: no sound played within %v", Timeout)
			return wire.PluginPlaySoundPayload{}, uuid.Nil
		}
	}
}
