// Package pane runs Bubble Tea models as Concord remote panes: the same
// model a plugin uses as a standalone terminal app is driven by viewers'
// keys and resized to their panes, and its View() becomes their frames.
//
//	host := pane.NewHost(func(v *pane.Viewer) tea.Model {
//		return newBoardModel(v.DisplayName) // your normal Bubble Tea model
//	})
//	plugin.Run(ctx, cfg, host.Handler())
//
// Each viewer gets their own model (their own cursor, menus, scroll). Shared
// state -- a game, a board -- lives outside the models; after changing it,
// call Host.Broadcast with a message the models react to, and every
// viewer of that channel is re-rendered. A model returning tea.Quit hands
// that viewer's keyboard back to Concord (the pane stays on screen).
//
// Colors: render with lipgloss as usual. Frames are produced in true color
// and converted down to each viewer's terminal (Viewer.Profile), so a
// model never needs to know who's looking.
package pane

import (
	"bytes"
	"reflect"
	"runtime"
	"strings"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"github.com/muesli/termenv"
)

// Viewer is one person with a pane open.
type Viewer struct {
	ID          uuid.UUID
	ChannelID   uuid.UUID
	Name        string // username
	DisplayName string // how they appear in the server (nickname, display name or username)
	Width       int
	Height      int
	Theme       *wire.PaneTheme // their Concord theme; may be nil
	Profile     colorprofile.Profile

	model    tea.Model
	lastSent string
}

// Color returns the viewer's Concord theme color by name ("red", "cyan",
// "foreground", ...) as a lipgloss color, or fallback when unknown -- so a
// pane can match whatever theme each viewer uses.
func (v *Viewer) Color(name string, fallback lipgloss.TerminalColor) lipgloss.TerminalColor {
	if v.Theme != nil {
		if c := v.Theme.Palette[name]; c != "" {
			return lipgloss.Color(c)
		}
	}
	return fallback
}

// NewModel builds a model for a viewer who just opened a pane. It's sent a
// tea.WindowSizeMsg with the pane's size before its first render, and
// again whenever the pane is resized.
type NewModel func(v *Viewer) tea.Model

// Host drives one model per viewer across all of a plugin's pane channels.
// Its methods must be called from Handler callbacks (which run one at a
// time), not from other goroutines -- send those work via Conn instead,
// or wrap the call in the plugin's own event.
type Host struct {
	newModel NewModel
	viewers  map[uuid.UUID]*Viewer
	conn     *plugin.Conn

	// While a model is updating, Send/Broadcast calls (e.g. from inside
	// that model's own Update) are queued and delivered right after, so
	// no model is ever re-entered mid-update.
	busy   bool
	queued []queuedMsg
}

type queuedMsg struct {
	viewer uuid.UUID
	msg    tea.Msg
}

func init() {
	// Render everything at full color; frames are downsampled per viewer.
	// Without this, lipgloss would detect the plugin's stdout (a pipe, not a
	// terminal) and render no color at all.
	lipgloss.SetColorProfile(termenv.TrueColor)
}

// NewHost makes a Host that builds each viewer's model with newModel.
func NewHost(newModel NewModel) *Host {
	return &Host{newModel: newModel, viewers: map[uuid.UUID]*Viewer{}}
}

// Handler returns plugin callbacks for pane events. To handle other events
// too, set more fields on the returned Handler (or wrap these).
func (h *Host) Handler() plugin.Handler {
	return plugin.Handler{
		OnEnter:  h.Enter,
		OnInput:  h.Input,
		OnResize: h.Resize,
		OnLeave:  h.Leave,
	}
}

// Viewers returns everyone currently viewing channelID.
func (h *Host) Viewers(channelID uuid.UUID) []*Viewer {
	var out []*Viewer
	for _, v := range h.viewers {
		if v.ChannelID == channelID {
			out = append(out, v)
		}
	}
	return out
}

// Viewer returns the viewer with id, or nil.
func (h *Host) Viewer(id uuid.UUID) *Viewer { return h.viewers[id] }

// Enter starts a model for a viewer (or rebuilds it: Concord replays Enter
// for current viewers after a reconnect).
func (h *Host) Enter(c *plugin.Conn, e wire.PluginPaneEnterPayload) {
	h.conn = c
	v := &Viewer{
		ID: e.ViewerID, ChannelID: e.ChannelID, Name: e.ViewerName, DisplayName: e.ViewerDisplayName,
		Width: e.Width, Height: e.Height, Theme: e.Theme, Profile: profileOf(e.Theme),
	}
	if v.DisplayName == "" {
		v.DisplayName = v.Name
	}
	if old := h.viewers[e.ViewerID]; old != nil && old.ChannelID == e.ChannelID && old.model != nil {
		v.model = old.model // same pane, reconnected: keep their place
	} else {
		v.model = h.newModel(v)
		v.model = h.run(v, v.model.Init())
	}
	h.viewers[e.ViewerID] = v
	h.update(v, tea.WindowSizeMsg{Width: v.Width, Height: v.Height})
}

// Input feeds one keypress to the viewer's model.
func (h *Host) Input(c *plugin.Conn, e wire.PluginPaneInputPayload) {
	h.conn = c
	if v := h.viewers[e.ViewerID]; v != nil {
		h.update(v, KeyMsg(e))
	}
}

// Resize tells the viewer's model its new size (and theme).
func (h *Host) Resize(c *plugin.Conn, e wire.PluginPaneResizePayload) {
	h.conn = c
	v := h.viewers[e.ViewerID]
	if v == nil {
		return
	}
	v.Width, v.Height = e.Width, e.Height
	if e.Theme != nil {
		v.Theme, v.Profile = e.Theme, profileOf(e.Theme)
		v.lastSent = "" // colors may render differently now
	}
	h.update(v, tea.WindowSizeMsg{Width: v.Width, Height: v.Height})
}

// Leave drops the viewer's model.
func (h *Host) Leave(_ *plugin.Conn, e wire.PluginPaneLeavePayload) {
	delete(h.viewers, e.ViewerID)
}

// Send delivers msg to one viewer's model and re-renders them.
func (h *Host) Send(viewerID uuid.UUID, msg tea.Msg) {
	if v := h.viewers[viewerID]; v != nil {
		h.update(v, msg)
	}
}

// Broadcast delivers msg to the model of everyone viewing channelID -- e.g.
// "the board changed" after a move -- and re-renders each of them. Like
// Send, it's safe to call from inside a model's own Update: delivery then
// waits until that update is done.
func (h *Host) Broadcast(channelID uuid.UUID, msg tea.Msg) {
	for _, v := range h.Viewers(channelID) {
		h.update(v, msg)
	}
}

// update feeds msg through the model (running any commands it returns)
// and pushes the new frame if it changed.
func (h *Host) update(v *Viewer, msg tea.Msg) {
	if h.busy {
		h.queued = append(h.queued, queuedMsg{v.ID, msg})
		return
	}
	h.busy = true
	m, cmd := v.model.Update(msg)
	v.model = m
	v.model = h.run(v, cmd)
	h.render(v)
	h.busy = false

	for len(h.queued) > 0 {
		q := h.queued[0]
		h.queued = h.queued[1:]
		if qv := h.viewers[q.viewer]; qv != nil {
			h.update(qv, q.msg)
		}
	}
}

// run executes cmd -- and whatever it triggers -- against the viewer's
// model, standing in for tea.Program's event loop. Commands run
// synchronously, in order. Two kinds are skipped, because there's no real
// terminal and no clock-driven redraw here:
//
//   - Cursor blinking (bubbles/cursor): BlinkCmd blocks for the whole blink
//     interval before returning, which made every keystroke in a text
//     field take ~530ms (found in Tukan). It's recognised by name and never
//     called.
//   - tea.Quit ends that viewer's key capture: the keyboard goes back to
//     Concord, the pane stays open.
//
// Anything a command returns that isn't a batch is fed back into Update.
func (h *Host) run(v *Viewer, cmd tea.Cmd) tea.Model {
	m := v.model
	for depth := 0; cmd != nil && depth < 100; depth++ {
		if isBlinkCmd(cmd) {
			return m
		}
		msg := cmd()
		switch msg := msg.(type) {
		case nil:
			return m
		case tea.QuitMsg:
			if h.conn != nil {
				_ = h.conn.LeavePane(v.ChannelID, v.ID)
			}
			return m
		case tea.BatchMsg:
			for _, c := range msg {
				v.model = m
				m = h.run(v, c)
			}
			return m
		}
		if isBlinkMsg(msg) {
			return m
		}
		m, cmd = m.Update(msg)
	}
	return m
}

// render pushes the viewer's current View() if it changed: clamped to their
// pane (anything wider or taller would be wrapped by Concord and garble
// every line) and converted to their terminal's colors.
func (h *Host) render(v *Viewer) {
	frame := Fit(v.model.View(), v.Width, v.Height)
	if v.Profile != colorprofile.TrueColor {
		var buf bytes.Buffer
		w := &colorprofile.Writer{Forward: &buf, Profile: v.Profile}
		_, _ = w.WriteString(frame)
		frame = buf.String()
	}
	if frame == v.lastSent || h.conn == nil {
		return
	}
	if err := h.conn.Frame(v.ChannelID, v.ID, frame); err == nil {
		v.lastSent = frame
	}
}

// Fit trims a rendered view to at most width columns and height lines
// (never pads), keeping escape codes intact.
func Fit(view string, width, height int) string {
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, l := range lines {
		if width > 0 && ansi.StringWidth(l) > width {
			lines[i] = ansi.Truncate(l, width, "")
		}
	}
	return strings.Join(lines, "\n")
}

// KeyMsg rebuilds the tea.KeyMsg a viewer's keypress would have produced in
// a real terminal, from its KeyString ("a", "enter", "ctrl+c", "alt+left").
func KeyMsg(e wire.PluginPaneInputPayload) tea.KeyMsg {
	s := e.KeyString
	alt := false
	if strings.HasPrefix(s, "alt+") && s != "alt+" {
		alt, s = true, strings.TrimPrefix(s, "alt+")
	}
	if kt, ok := keyNames[s]; ok {
		return tea.KeyMsg{Type: kt, Alt: alt}
	}
	if s == " " || s == "space" {
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}, Alt: alt}
	}
	if len(e.Runes) > 0 {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: e.Runes, Alt: alt || e.Alt}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s), Alt: alt}
}

// keyNames maps Bubble Tea's key names back to key types.
var keyNames = func() map[string]tea.KeyType {
	m := map[string]tea.KeyType{}
	for t := tea.KeyType(-1000); t <= 1000; t++ {
		if name := (tea.KeyMsg{Type: t}).String(); name != "" && t != tea.KeyRunes && t != tea.KeySpace {
			if _, dup := m[name]; !dup {
				m[name] = t
			}
		}
	}
	return m
}()

// profileOf maps a viewer's reported color support to a colorprofile.
func profileOf(t *wire.PaneTheme) colorprofile.Profile {
	if t == nil {
		return colorprofile.TrueColor
	}
	switch t.ColorProfile {
	case "ansi256":
		return colorprofile.ANSI256
	case "ansi":
		return colorprofile.ANSI
	case "ascii":
		return colorprofile.Ascii
	default:
		return colorprofile.TrueColor
	}
}

// blinkPkgPath is where bubbles' cursor-blink message chain lives, checked
// by reflection because its first message type is unexported.
const blinkPkgPath = "github.com/charmbracelet/bubbles/cursor"

func isBlinkMsg(msg tea.Msg) bool {
	t := reflect.TypeOf(msg)
	return t != nil && t.PkgPath() == blinkPkgPath
}

// isBlinkCmd recognises bubbles/cursor's BlinkCmd (or a closure it made) by
// its function name, without calling it -- calling it is what blocks.
func isBlinkCmd(cmd tea.Cmd) bool {
	name := runtime.FuncForPC(reflect.ValueOf(cmd).Pointer()).Name()
	return strings.Contains(name, "/bubbles/cursor.")
}
