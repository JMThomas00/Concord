package client

import (
	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// PluginPaneState holds the client's view of an active remote-pane plugin
// channel: the last rendered frame the owning plugin process pushed, and
// enough bookkeeping to ignore stale/out-of-order frames. Concord's client
// never interprets the frame's contents — it's an opaque, pre-rendered string
// from the plugin's own Bubble Tea View().
type PluginPaneState struct {
	ChannelID uuid.UUID
	Frame     string
	LastSeq   int64
}

// enterPluginPane switches the active pane to a plugin channel: it skips the
// normal chat history load (selectChannel's caller is responsible for that)
// and tells the owning plugin process a viewer showed up, so it can start
// pushing rendered frames sized to the current viewport.
func (a *App) enterPluginPane(ch *models.Channel) {
	a.pluginPane = &PluginPaneState{ChannelID: ch.ID}
	a.sendPluginPane(protocol.OpPluginPaneEnter, protocol.PluginPaneEnterPayload{
		ChannelID: ch.ID,
		Width:     a.chatViewport.Width,
		Height:    a.chatViewport.Height,
	})
}

// leavePluginPane tells the owning plugin a viewer navigated away and clears
// local pane state. Safe to call even if no pane is active.
func (a *App) leavePluginPane() {
	if a.pluginPane == nil {
		return
	}
	a.sendPluginPane(protocol.OpPluginPaneLeave, protocol.PluginPaneLeavePayload{
		ChannelID: a.pluginPane.ChannelID,
	})
	a.pluginPane = nil
}

// resizePluginPane reports the current viewport size to the owning plugin,
// e.g. after a terminal resize. No-op if no pane is active.
func (a *App) resizePluginPane() {
	if a.pluginPane == nil {
		return
	}
	a.sendPluginPane(protocol.OpPluginPaneResize, protocol.PluginPaneResizePayload{
		ChannelID: a.pluginPane.ChannelID,
		Width:     a.chatViewport.Width,
		Height:    a.chatViewport.Height,
	})
}

// forwardPluginPaneInput relays one keypress to the owning plugin. Called
// from Update() for any key not already claimed by a global keybind while a
// remote pane is active.
func (a *App) forwardPluginPaneInput(msg tea.KeyMsg) {
	if a.pluginPane == nil {
		return
	}
	a.sendPluginPane(protocol.OpPluginPaneInput, protocol.PluginPaneInputPayload{
		ChannelID: a.pluginPane.ChannelID,
		KeyType:   int(msg.Type),
		Runes:     msg.Runes,
		Alt:       msg.Alt,
		KeyString: msg.String(),
	})
}

// sendPluginPane marshals and sends one plugin-pane opcode over the active
// connection. Silently no-ops if there's no active connection — the same
// tolerance the rest of the client's fire-and-forget sends already have.
func (a *App) sendPluginPane(op protocol.OpCode, payload interface{}) {
	if a.activeConn == nil {
		return
	}
	msg, err := protocol.NewMessage(op, payload)
	if err != nil {
		return
	}
	_ = a.activeConn.Connection.Send(msg)
}

// applyPluginPaneFrame stores a newly-received rendered frame, dropping it if
// stale (out-of-order delivery) or meant for a channel that isn't the
// currently active pane.
func (a *App) applyPluginPaneFrame(payload protocol.PluginPaneFramePayload) {
	if a.pluginPane == nil || a.pluginPane.ChannelID != payload.ChannelID {
		return
	}
	if a.pluginPane.LastSeq != 0 && payload.Seq <= a.pluginPane.LastSeq {
		return
	}
	a.pluginPane.Frame = payload.Frame
	a.pluginPane.LastSeq = payload.Seq
}

// renderPluginPaneFrame fits the plugin's last-pushed frame into the chat
// panel's interior, matching the empty-state styling used elsewhere in
// renderChatPanel so a pane looks native rather than bolted on.
func (a *App) renderPluginPaneFrame(width, height int) string {
	if a.pluginPane == nil || a.pluginPane.Frame == "" {
		return lipgloss.NewStyle().
			Foreground(lipgloss.Color(a.theme.Colors.Comment)).
			Italic(true).
			Width(width).
			Height(height).
			Align(lipgloss.Center, lipgloss.Center).
			Render("Waiting for plugin…")
	}
	return lipgloss.NewStyle().
		Width(width).
		Height(height).
		Render(a.pluginPane.Frame)
}
