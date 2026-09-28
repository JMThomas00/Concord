// Package pty puts an existing terminal program -- one that knows nothing
// about Concord -- into a Concord channel: it runs the program in a
// pseudo-terminal, keeps a virtual screen of its output, and shows that
// screen to everyone viewing the channel, with one of them typing.
//
//	plugin.Run(ctx, cfg, pty.Handler(pty.Options{Command: "./btop"}))
//
// It's a separate module (github.com/JMThomas00/Concord/sdk/pty) because
// the terminal emulator it uses needs newer libraries than the rest of the
// SDK; only plugins that wrap a program pay for them.
//
// Safety: everyone who can see the channel sees the program, and whoever
// holds the keyboard controls it -- with the plugin's permissions on the
// server. Never wrap a shell or anything that can start one. Ship the
// program inside the plugin's release archive; a Docker server can't
// install packages.
//
// Linux and macOS only for now (Windows needs ConPTY support).
package pty

import (
	"strings"
	"time"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// Options says what to run and who may type.
type Options struct {
	// Command is the program to run (a path, usually inside the plugin's
	// own folder), and Args its arguments.
	Command string
	Args    []string
	// Dir is its working directory; default the plugin's data folder.
	Dir string
	// Env is extra "KEY=value" environment. The program otherwise gets only
	// TERM, COLORTERM, LANG, PATH and HOME (= the data folder).
	Env []string
	// Shared lets every viewer type. By default only the "driver" does:
	// the first viewer, then whoever's next when they leave.
	Shared bool
	// FPS caps frames per second (default 30).
	FPS int
}

// session is one channel's running program.
type session struct {
	channelID uuid.UUID
	proc      process // platform-specific (pty_unix.go)
	screen    *screen
	viewers   []*viewer // in arrival order; viewers[0] drives
	exited    bool
	exitMsg   string
	dirty     bool
	scheduled bool
}

type viewer struct {
	id            uuid.UUID
	name          string
	width, height int
	lastFrame     string
}

// Handler returns plugin callbacks that run opts.Command in each of the
// plugin's channels.
func Handler(opts Options) plugin.Handler {
	if opts.FPS <= 0 {
		opts.FPS = 30
	}
	h := &host{opts: opts, sessions: map[uuid.UUID]*session{}}
	return plugin.Handler{
		OnEnter:         h.enter,
		OnInput:         h.input,
		OnResize:        h.resize,
		OnLeave:         h.leave,
		OnChannelDelete: func(c *plugin.Conn, e wire.ChannelDeletePayload) { h.stop(e.ChannelID) },
	}
}

type host struct {
	opts     Options
	conn     *plugin.Conn
	sessions map[uuid.UUID]*session
}

func (h *host) enter(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
	h.conn = c
	s := h.sessions[e.ChannelID]
	if s == nil {
		s = &session{channelID: e.ChannelID}
		h.sessions[e.ChannelID] = s
	}
	name := e.ViewerDisplayName
	if name == "" {
		name = e.ViewerName
	}
	for _, v := range s.viewers {
		if v.id == e.ViewerID { // replayed after a reconnect
			v.width, v.height, v.lastFrame = e.Width, e.Height, ""
			h.redraw(s)
			return
		}
	}
	s.viewers = append(s.viewers, &viewer{id: e.ViewerID, name: name, width: e.Width, height: e.Height})
	if s.proc == nil && !s.exited {
		h.start(s)
	} else {
		h.fitToDriver(s)
	}
	h.title(s)
	h.redraw(s)
}

func (h *host) input(c *plugin.Conn, e wire.PluginPaneInputPayload) {
	h.conn = c
	s := h.sessions[e.ChannelID]
	if s == nil || len(s.viewers) == 0 {
		return
	}
	if !h.opts.Shared && s.viewers[0].id != e.ViewerID {
		return // only the driver types
	}
	if s.exited {
		if e.KeyString == "enter" {
			s.exited = false
			h.start(s)
		}
		return
	}
	if seq := Encode(e.KeyString, e.Runes); seq != "" && s.proc != nil {
		_, _ = s.proc.Write([]byte(seq))
	}
}

func (h *host) resize(c *plugin.Conn, e wire.PluginPaneResizePayload) {
	h.conn = c
	s := h.sessions[e.ChannelID]
	if s == nil {
		return
	}
	for _, v := range s.viewers {
		if v.id == e.ViewerID {
			v.width, v.height, v.lastFrame = e.Width, e.Height, ""
		}
	}
	h.fitToDriver(s)
	h.redraw(s)
}

func (h *host) leave(c *plugin.Conn, e wire.PluginPaneLeavePayload) {
	h.conn = c
	s := h.sessions[e.ChannelID]
	if s == nil {
		return
	}
	for i, v := range s.viewers {
		if v.id == e.ViewerID {
			s.viewers = append(s.viewers[:i], s.viewers[i+1:]...)
			break
		}
	}
	if len(s.viewers) == 0 {
		h.stop(e.ChannelID) // nobody's watching: don't keep it running
		return
	}
	h.fitToDriver(s)
	h.title(s)
}

// start launches the program at the driver's pane size.
func (h *host) start(s *session) {
	w, ht := h.driverSize(s)
	s.screen = newScreen(w, ht)
	proc, err := startProcess(h.opts, h.dataDir(), w, ht)
	if err != nil {
		s.exited, s.exitMsg = true, "Couldn't start "+h.opts.Command+": "+err.Error()
		return
	}
	s.proc = proc
	channelID := s.channelID
	conn := h.conn
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := proc.Read(buf)
			if n > 0 {
				data := append([]byte(nil), buf[:n]...)
				conn.Post(func() { h.output(channelID, proc, data) })
			}
			if err != nil {
				conn.Post(func() { h.exit(channelID, proc) })
				return
			}
		}
	}()
	// The emulator answers the program's terminal queries (cursor
	// position, device attributes); pass those answers back to it.
	go s.screen.pipeReplies(proc)
}

func (h *host) output(channelID uuid.UUID, proc process, data []byte) {
	s := h.sessions[channelID]
	if s == nil || s.proc != proc {
		return
	}
	s.screen.write(data)
	s.dirty = true
	if !s.scheduled {
		s.scheduled = true
		conn := h.conn
		time.AfterFunc(time.Second/time.Duration(h.opts.FPS), func() {
			conn.Post(func() {
				if s := h.sessions[channelID]; s != nil {
					s.scheduled = false
					if s.dirty {
						s.dirty = false
						h.redraw(s)
					}
				}
			})
		})
	}
}

func (h *host) exit(channelID uuid.UUID, proc process) {
	s := h.sessions[channelID]
	if s == nil || s.proc != proc {
		return
	}
	s.proc.Close()
	s.screen.close() // ends its reply-relay goroutine
	s.proc, s.exited = nil, true
	s.exitMsg = h.opts.Command + " exited. The driver can press Enter to start it again."
	h.redraw(s)
}

func (h *host) stop(channelID uuid.UUID) {
	if s := h.sessions[channelID]; s != nil {
		if s.proc != nil {
			s.proc.Close()
		}
		if s.screen != nil {
			s.screen.close()
		}
	}
	delete(h.sessions, channelID)
}

func (h *host) driverSize(s *session) (int, int) {
	if len(s.viewers) == 0 {
		return 80, 24
	}
	d := s.viewers[0]
	w, ht := d.width, d.height
	if w < 10 {
		w = 10
	}
	if ht < 3 {
		ht = 3
	}
	return w, ht
}

// fitToDriver resizes the terminal to whoever drives now.
func (h *host) fitToDriver(s *session) {
	if s.proc == nil || s.screen == nil {
		return
	}
	w, ht := h.driverSize(s)
	if w == s.screen.width && ht == s.screen.height {
		return
	}
	s.screen.resize(w, ht)
	_ = s.proc.Resize(w, ht)
}

// title tells viewers who has the keyboard.
func (h *host) title(s *session) {
	if h.conn == nil || len(s.viewers) == 0 {
		return
	}
	name := h.opts.Command
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	title := name + " — " + s.viewers[0].name + " has the keyboard"
	if h.opts.Shared {
		title = name + " — everyone can type"
	}
	_ = h.conn.SetTitle(s.channelID, uuid.Nil, title)
}

// redraw sends each viewer the screen, fitted to their pane.
func (h *host) redraw(s *session) {
	if h.conn == nil {
		return
	}
	frame := s.exitMsg
	if !s.exited && s.screen != nil {
		frame = s.screen.render()
	}
	for _, v := range s.viewers {
		f := pane.Fit(frame, v.width, v.height)
		if f == v.lastFrame {
			continue
		}
		if err := h.conn.Frame(s.channelID, v.id, f); err == nil {
			v.lastFrame = f
		}
	}
}

func (h *host) dataDir() string {
	if h.conn != nil && h.conn.DataDir() != "" {
		return h.conn.DataDir()
	}
	return "."
}
