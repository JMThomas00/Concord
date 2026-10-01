package client

import (
	"encoding/json"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
	"github.com/muesli/termenv"
)

// PluginPaneState holds the client's view of an active remote-pane plugin
// channel: the last rendered frame the owning plugin process pushed, and
// enough bookkeeping to ignore stale/out-of-order frames. Concord's client
// never interprets the frame's contents — it's an opaque, pre-rendered string
// from the plugin's own View().
//
// The pane's wire lifecycle (Enter, Resize, re-Enter after a reconnect) is
// driven by syncPluginPane on a short timer rather than from the places
// that change channel, layout or connection -- so every one of those paths
// is covered without each having to remember to tell the plugin.
type PluginPaneState struct {
	ChannelID uuid.UUID
	Frame     string // already passed through sanitizePaneFrame
	LastSeq   int64
	Epoch     int64  // the plugin connection LastSeq belongs to
	Title     string // plugin-set border title (pane_title); "" = channel name
	PluginID  string // the plugin (or instance) that owns the channel
	Images    []protocol.PaneImage // the frame's images (plugin_images.go)

	conn       *ServerConnection // the server this pane belongs to
	selectedAt time.Time
	entered    bool // the server has our Enter (reset when the connection drops)
	sentW      int
	sentH      int
	sentTheme  string
	renderW    int // the size renderPluginPaneFrame last drew at
	renderH    int

	checkPending bool
	retryAt      time.Time

	// The plugin's client code (plugin_code_pane.go).
	codeChecked bool         // whether to run it has been decided
	consent     *codeConsent // the question on screen instead of the pane
	code        *codeRunner  // the running code
	local       *codeFrame   // what it drew, shown instead of Frame
	keysLocal   bool         // it asked for keys not to go to the server
	codeW       int
	codeH       int
	codeTheme   string
}

// paneCheckMsg fires syncPluginPane's deferred check.
type paneCheckMsg struct{}

const (
	// paneEnterDelay is how long a plugin channel must stay selected before
	// its plugin is told someone's looking, so arrowing past plugin channels
	// in the list doesn't spray Enter/Leave pairs at every plugin passed.
	// Focusing the pane (Tab) enters immediately.
	paneEnterDelay = 150 * time.Millisecond
	// paneCheckDelay batches size checks: one runs this long after any
	// activity, after the View that activity triggered has rendered.
	paneCheckDelay = 100 * time.Millisecond
	// paneRetryDelay is the wait before retrying a send that failed because
	// the connection is down.
	paneRetryDelay = 2 * time.Second

	// paneLeaveKey hands focus back to Concord from a pane that captures
	// every other key. Reserved: never forwarded to plugins.
	paneLeaveKey = "ctrl+]"
)

// paneFocused reports whether the active plugin pane is capturing keys.
func (a *App) paneFocused() bool {
	return a.view == ViewMain && a.focus == FocusChat && a.pluginPane != nil &&
		a.currentChannel != nil && a.pluginPane.ChannelID == a.currentChannel.ID
}

// enterPluginPane switches the active pane to a plugin channel. The plugin
// is told once the selection settles (see paneEnterDelay).
func (a *App) enterPluginPane(ch *models.Channel) {
	a.pluginPane.stopPaneCode()
	a.pluginPane = &PluginPaneState{ChannelID: ch.ID, PluginID: ch.PluginID, conn: a.activeConn, selectedAt: time.Now()}
}

// leavePluginPane tells the owning plugin this viewer is gone (if it was
// ever told they arrived) and clears local pane state. Safe to call even if
// no pane is active.
func (a *App) leavePluginPane() {
	p := a.pluginPane
	if p == nil {
		return
	}
	if p.entered {
		a.sendPluginPane(p, protocol.OpPluginPaneLeave, protocol.PluginPaneLeavePayload{ChannelID: p.ChannelID})
	}
	p.stopPaneCode()
	a.pluginPane = nil
}

// releasePaneFocus stops the pane capturing keys and returns focus to the
// channel list. The pane stays open and keeps receiving frames -- a player
// can watch the board while browsing, or chat elsewhere and come back.
func (a *App) releasePaneFocus() {
	a.focus = FocusChannelList
}

// paneDisconnected marks the pane as no longer registered with the server
// (the connection dropped, so the server has already dropped us as a
// viewer). The pane stays on screen and syncPluginPane re-enters it once
// the connection is back.
func (a *App) paneDisconnected() {
	if a.pluginPane != nil {
		a.pluginPane.entered = false
		a.pluginPane.retryAt = time.Time{}
	}
}

// forwardPluginPaneInput relays one keypress to the owning plugin, and to
// its client code if that's running. Called from Update() for any key not
// already claimed while the pane is focused. A pane asking whether to run
// the plugin's code takes the keys itself.
func (a *App) forwardPluginPaneInput(msg tea.KeyMsg) tea.Cmd {
	p := a.pluginPane
	if p == nil || !p.entered || msg.String() == paneLeaveKey {
		return nil
	}
	if p.consent != nil {
		return a.handleConsentKey(p, msg)
	}
	if r := a.paneCode("pane"); r != nil {
		r.push(codeKeyEvent(msg))
		if p.keysLocal {
			return nil
		}
	}
	a.sendPluginPane(p, protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{
		ChannelID: p.ChannelID,
		KeyType:   int(msg.Type),
		Runes:     msg.Runes,
		Alt:       msg.Alt,
		KeyString: msg.String(),
	})
	return nil
}

// sendPluginPane sends one plugin-pane opcode over the pane's own server
// connection, reporting whether it went out.
func (a *App) sendPluginPane(p *PluginPaneState, op protocol.OpCode, payload interface{}) bool {
	if p.conn == nil || p.conn.Connection == nil {
		return false
	}
	msg, err := protocol.NewMessage(op, payload)
	if err != nil {
		return false
	}
	return p.conn.Connection.Send(msg) == nil
}

// paneSize is the size the pane is actually drawn at: what
// renderPluginPaneFrame last rendered, else the chat viewport's size.
func (a *App) paneSize() (int, int) {
	p := a.pluginPane
	if p != nil && p.renderW > 0 && p.renderH > 0 {
		return p.renderW, p.renderH
	}
	return a.chatViewport.Width, a.chatViewport.Height
}

// paneTheme describes the current theme and the terminal's color support,
// so plugins can render to match.
func (a *App) paneTheme() *protocol.PaneTheme {
	c := a.theme.Colors
	profile := "truecolor"
	switch lipgloss.ColorProfile() {
	case termenv.ANSI256:
		profile = "ansi256"
	case termenv.ANSI:
		profile = "ansi"
	case termenv.Ascii:
		profile = "ascii"
	}
	return &protocol.PaneTheme{
		Name: a.theme.Meta.Name,
		Palette: map[string]string{
			"background": c.Background, "current_line": c.CurrentLine, "selection": c.Selection,
			"foreground": c.Foreground, "comment": c.Comment, "red": c.Red, "orange": c.Orange,
			"yellow": c.Yellow, "green": c.Green, "cyan": c.Cyan, "purple": c.Purple, "pink": c.Pink,
		},
		ColorProfile: profile,
	}
}

// schedulePaneCheck queues one syncPluginPane pass shortly after any
// activity while a pane is open. Called from the Update wrapper for every
// message except paneCheckMsg itself, so an idle client schedules nothing.
func (a *App) schedulePaneCheck() tea.Cmd {
	p := a.pluginPane
	if p == nil || p.checkPending {
		return nil
	}
	return a.paneCheckAfter(paneCheckDelay)
}

func (a *App) paneCheckAfter(d time.Duration) tea.Cmd {
	a.pluginPane.checkPending = true
	return tea.Tick(d, func(time.Time) tea.Msg { return paneCheckMsg{} })
}

// syncPluginPane brings the server's view of this pane up to date: Enter
// once the selection has settled (or the pane is focused, or after a
// reconnect), then Resize whenever the drawn size or theme has changed.
func (a *App) syncPluginPane() tea.Cmd {
	p := a.pluginPane
	if p == nil {
		return nil
	}
	p.checkPending = false
	now := time.Now()
	if now.Before(p.retryAt) {
		return a.paneCheckAfter(p.retryAt.Sub(now))
	}
	w, h := a.paneSize()
	theme := a.paneTheme()

	if !p.entered {
		if since := now.Sub(p.selectedAt); !a.paneFocused() && since < paneEnterDelay {
			return a.paneCheckAfter(paneEnterDelay - since)
		}
		// A fresh Enter restarts the plugin's frame stream for us.
		p.LastSeq, p.Epoch = 0, 0
		if !a.sendPluginPane(p, protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{
			ChannelID: p.ChannelID, Width: w, Height: h, Theme: theme,
		}) {
			p.retryAt = now.Add(paneRetryDelay)
			return a.paneCheckAfter(paneRetryDelay)
		}
		p.entered, p.sentW, p.sentH, p.sentTheme = true, w, h, theme.Name
		return a.startPaneCodeIfAny(p)
	}

	a.syncPaneCode(w, h, theme)
	if w != p.sentW || h != p.sentH || theme.Name != p.sentTheme {
		if a.sendPluginPane(p, protocol.OpPluginPaneResize, protocol.PluginPaneResizePayload{
			ChannelID: p.ChannelID, Width: w, Height: h, Theme: theme,
		}) {
			p.sentW, p.sentH, p.sentTheme = w, h, theme.Name
		}
	}
	return nil
}

// applyPluginPaneFrame stores a newly-received rendered frame, dropping it if
// stale (out-of-order delivery) or meant for a channel that isn't the
// currently active pane.
func (a *App) applyPluginPaneFrame(payload protocol.PluginPaneFramePayload) tea.Cmd {
	if a.pluginPane == nil || a.pluginPane.ChannelID != payload.ChannelID {
		return nil
	}
	// A different epoch means the plugin reconnected (crash + restart) and
	// its Seq counter started over, so the new stream wins regardless of Seq.
	sameStream := payload.Epoch == a.pluginPane.Epoch
	if sameStream && a.pluginPane.LastSeq != 0 && payload.Seq <= a.pluginPane.LastSeq {
		return nil
	}
	a.pluginPane.Frame = sanitizePaneFrame(payload.Frame)
	a.pluginPane.LastSeq = payload.Seq
	a.pluginPane.Epoch = payload.Epoch
	a.pluginPane.Images = payload.Images
	if r := a.paneCode("pane"); r != nil {
		r.push(map[string]any{"type": "server_frame", "text": a.pluginPane.Frame, "images": payload.Images})
	}
	// Start fetching any image this frame shows for the first time.
	return a.fetchPaneAssets(a.pluginPane.conn, a.pluginPane.PluginID, payload.Images)
}

// renderPluginPaneFrame fits the plugin's last-pushed frame into the chat
// panel's interior, matching the empty-state styling used elsewhere in
// renderChatPanel so a pane looks native rather than bolted on. It also
// records the size it drew at, which is what syncPluginPane reports.
func (a *App) renderPluginPaneFrame(width, height int) string {
	if a.pluginPane != nil {
		a.pluginPane.renderW, a.pluginPane.renderH = width, height
	}
	if p := a.pluginPane; p != nil && p.consent != nil {
		return a.renderConsent(p.consent, width, height)
	}
	frame, images := "", []protocol.PaneImage(nil)
	if p := a.pluginPane; p != nil {
		frame, images = p.Frame, p.Images
		if p.local != nil && p.local.text != nil {
			// The plugin's client code drew this one.
			frame, images = *p.local.text, p.local.images
		}
	}
	if frame == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Waiting for plugin…")
	}
	// Centered, not left-aligned: a plugin's frame doesn't always exactly
	// fill the box it's given (e.g. Tukan's own lane-width math rounds
	// down rather than ever risking rendering wider than reported, since
	// an oversized frame gets word-wrapped here and corrupts every line —
	// see the laneWidth fix in Tukan's own history). A slightly narrower
	// frame should sit centered in the pane, not jammed into the top-left
	// corner with all the slack on the right.
	if len(images) > 0 {
		lines, rasters := a.drawPaneImages(strings.Split(frame, "\n"), width, a.pluginPane.conn, a.pluginPane.PluginID, images)
		frame = strings.Join(lines, "\n")
		a.paneRasters = rasters
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Align(lipgloss.Center, lipgloss.Center).
		Render(frame)
}

// handlePluginEvent acts on an EventPluginEvent a plugin sent this viewer.
// Unknown kinds are ignored: plugins can add viewer-directed signals ahead
// of client support for them.
func (a *App) handlePluginEvent(serverID uuid.UUID, sc *ServerConnection, ev protocol.PluginEventPayload) {
	switch ev.Kind {
	case protocol.PluginEventLeavePane:
		var p protocol.PluginPaneClosePayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		// Only if this is still the pane on screen -- the viewer may have
		// navigated elsewhere before the plugin's signal arrived.
		if a.paneFocused() && a.pluginPane.ChannelID == p.ChannelID {
			a.releasePaneFocus()
		}

	case protocol.PluginEventPaneTitle:
		var p protocol.PluginPaneTitlePayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if a.pluginPane != nil && a.pluginPane.ChannelID == p.ChannelID {
			a.pluginPane.Title = sanitizePaneTitle(p.Title)
		}

	case protocol.PluginEventPlaySound:
		var p protocol.PluginPlaySoundPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		// Only for the pane on screen: a sound from a game you've switched
		// away from would come from nowhere.
		if a.pluginPane != nil && a.pluginPane.ChannelID == p.ChannelID && a.pluginPane.conn == sc {
			a.playPluginSound(sc, a.pluginPane.PluginID, p)
		}

	case protocol.PluginEventNotifyUser:
		var p protocol.PluginNotifyUserPayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		a.notifyFromPlugin(serverID, sc, p)

	case protocol.PluginEventClientMessage:
		// For the plugin's client code in the pane on screen, if it runs.
		var p protocol.PluginClientMessagePayload
		if json.Unmarshal(ev.Payload, &p) != nil {
			return
		}
		if r := a.paneCode("server"); r != nil && a.pluginPane.ChannelID == p.ChannelID && a.pluginPane.conn == sc {
			r.push(map[string]any{"type": "server", "data": p.Data})
		}
	}
}

// notifyFromPlugin shows a plugin's targeted notification: a status-bar
// message, a mention badge on the plugin's channel (unless it's already on
// screen), and the usual desktop popup/sound rules for a mention.
func (a *App) notifyFromPlugin(serverID uuid.UUID, sc *ServerConnection, p protocol.PluginNotifyUserPayload) {
	content := sanitizePaneTitle(p.Content)
	channelName := ""
	if sc != nil {
		for _, channels := range sc.Channels {
			for _, ch := range channels {
				if ch.ID == p.ChannelID {
					channelName = ch.Name
				}
			}
		}
	}
	isCurrentServer := a.activeConn != nil && a.activeConn.ServerID == serverID
	isCurrentChannel := isCurrentServer && a.currentChannel != nil && a.currentChannel.ID == p.ChannelID
	if !isCurrentChannel {
		if a.mentionCounts == nil {
			a.mentionCounts = make(map[uuid.UUID]map[uuid.UUID]int)
		}
		if a.unreadCounts == nil {
			a.unreadCounts = make(map[uuid.UUID]map[uuid.UUID]int)
		}
		if a.mentionCounts[serverID] == nil {
			a.mentionCounts[serverID] = make(map[uuid.UUID]int)
		}
		a.mentionCounts[serverID][p.ChannelID]++
		if a.unreadCounts[serverID] == nil {
			a.unreadCounts[serverID] = make(map[uuid.UUID]int)
		}
		a.unreadCounts[serverID][p.ChannelID]++
	}
	a.statusMessage = "#" + channelName + ": " + content
	a.statusError = false
	srvName := ""
	if sc != nil && sc.ServerInfo != nil {
		srvName = sc.ServerInfo.Name
	}
	a.triggerMessageNotification(channelName, srvName, channelName, content, true, isCurrentChannel, isCurrentServer)
}

// sanitizePaneTitle reduces plugin-supplied one-line text (titles, toast
// text) to printable characters on a single line, capped in length.
func sanitizePaneTitle(s string) string {
	s = sanitizePaneFrame(s)
	s = ansi.Strip(s)
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		s = string(r[:120]) + "…"
	}
	return s
}
