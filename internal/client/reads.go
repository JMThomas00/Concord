package client

// Read positions that last (servers v0.1.9+): the server keeps how far you've
// read each channel, sends the unread counts with each server at sign-in,
// and tells your other devices when you read one (OpChannelRead,
// CHANNEL_READ). A channel opened with unread messages gets a "new" divider
// above the first of them until you leave it; Alt+U jumps there.

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

// readsVersion is the first server version that keeps read positions.
const readsVersion = "0.1.9"

// keepsReads reports whether sc's server keeps read positions.
func keepsReads(sc *ServerConnection) bool {
	if sc == nil {
		return false
	}
	sc.mu.RLock()
	v := strings.TrimPrefix(strings.TrimSpace(sc.ServerVersion), "v")
	sc.mu.RUnlock()
	return v == "dev" || versionAtLeast(v, readsVersion)
}

// newDivider is where the "new" line goes in the open channel.
type newDivider struct {
	channelID uuid.UUID
	afterSeq  int64     // messages after this one were unread when it opened
	messageID uuid.UUID // the first of them, once found (uuid.Nil until then)
	line      int       // its line in the chat, from the last render (-1: not drawn)
}

// channelReadFlushMsg sends a read that was held back (markRead).
type channelReadFlushMsg struct {
	serverID, channelID uuid.UUID
}

// seedUnreads takes a server's unread state from SERVER_CREATE: the
// server's counts replace what the client had, except for the open channel,
// which is read again now.
func (a *App) seedUnreads(serverID uuid.UUID, sc *ServerConnection, unreads []protocol.ChannelUnread) {
	if len(unreads) == 0 {
		return
	}
	sc.mu.Lock()
	if sc.ReadSeq == nil {
		sc.ReadSeq = make(map[uuid.UUID]int64)
	}
	for _, u := range unreads {
		sc.ReadSeq[u.ChannelID] = u.ReadSeq
	}
	sc.mu.Unlock()
	if a.unreadCounts[serverID] == nil {
		a.unreadCounts[serverID] = make(map[uuid.UUID]int)
	}
	if a.mentionCounts[serverID] == nil {
		a.mentionCounts[serverID] = make(map[uuid.UUID]int)
	}
	for _, u := range unreads {
		ch := u.ChannelID
		open := a.currentChannel != nil && a.currentChannel.ID == ch
		if open || a.mutedChannels[ch] || a.mutedServers[serverID] || u.Unread == 0 {
			delete(a.unreadCounts[serverID], ch)
			delete(a.mentionCounts[serverID], ch)
			continue
		}
		a.unreadCounts[serverID][ch] = u.Unread
		if u.Mentions > 0 {
			a.mentionCounts[serverID][ch] = u.Mentions
		} else {
			delete(a.mentionCounts[serverID], ch)
		}
	}
}

// openedChannel is called as a channel opens, before its unread count is
// cleared: it places the divider (or keeps it, when the same channel is
// opened again, as after a reconnect) and tells the server it's read.
func (a *App) openedChannel(serverID uuid.UUID, sc *ServerConnection, chID uuid.UUID) {
	if a.divider != nil && a.divider.channelID == chID {
		a.sendChannelRead(sc, chID)
		return
	}
	a.divider = nil
	if !keepsReads(sc) {
		return
	}
	if a.unreadCounts[serverID][chID] > 0 {
		sc.mu.RLock()
		after, ok := sc.ReadSeq[chID]
		sc.mu.RUnlock()
		if ok {
			a.divider = &newDivider{channelID: chID, afterSeq: after, line: -1}
		}
	}
	a.sendChannelRead(sc, chID)
}

// sendChannelRead tells the server the channel is read, now.
func (a *App) sendChannelRead(sc *ServerConnection, chID uuid.UUID) {
	if !keepsReads(sc) || sc.Connection == nil {
		return
	}
	if msg, err := protocol.NewMessage(protocol.OpChannelRead, protocol.ChannelReadRequest{ChannelID: chID}); err == nil {
		_ = sc.Connection.Send(msg)
		if a.readSentAt == nil {
			a.readSentAt = make(map[uuid.UUID]time.Time)
		}
		a.readSentAt[chID] = time.Now()
	}
}

// readEvery is how often a busy open channel is marked read.
const readEvery = 2 * time.Second

// markRead marks the open channel read as messages arrive in it: at once,
// or held back to one read per readEvery while they keep coming.
func (a *App) markRead(serverID uuid.UUID, sc *ServerConnection, chID uuid.UUID) tea.Cmd {
	if !keepsReads(sc) {
		return nil
	}
	if since := time.Since(a.readSentAt[chID]); since >= readEvery {
		a.sendChannelRead(sc, chID)
		return nil
	}
	if a.readPending == nil {
		a.readPending = make(map[uuid.UUID]bool)
	}
	if a.readPending[chID] {
		return nil
	}
	a.readPending[chID] = true
	return tea.Tick(readEvery, func(time.Time) tea.Msg { return channelReadFlushMsg{serverID, chID} })
}

func (a *App) flushChannelRead(m channelReadFlushMsg) {
	delete(a.readPending, m.channelID)
	if sc := a.connMgr.GetConnection(m.serverID); sc != nil {
		a.sendChannelRead(sc, m.channelID)
	}
}

// channelReadElsewhere takes CHANNEL_READ: the channel was read (here or on
// another device), so its unread marks go.
func (a *App) channelReadElsewhere(serverID uuid.UUID, sc *ServerConnection, p protocol.ChannelReadPayload) {
	sc.mu.Lock()
	if sc.ReadSeq == nil {
		sc.ReadSeq = make(map[uuid.UUID]int64)
	}
	if p.ReadSeq > sc.ReadSeq[p.ChannelID] {
		sc.ReadSeq[p.ChannelID] = p.ReadSeq
	}
	sc.mu.Unlock()
	delete(a.unreadCounts[serverID], p.ChannelID)
	delete(a.mentionCounts[serverID], p.ChannelID)
}

// dividerAbove reports whether the divider goes above messages[i], finding
// the first unread message the first time it's drawn: the first top-level
// message from someone else after the read position.
func (a *App) dividerAbove(messages []*MessageDisplay, i int) bool {
	d := a.divider
	if d == nil || a.currentChannel == nil || d.channelID != a.currentChannel.ID {
		return false
	}
	if d.messageID == uuid.Nil {
		for _, m := range messages {
			if m.Message != nil && m.ThreadID == nil && !m.IsOwn && m.Seq > d.afterSeq {
				d.messageID = m.ID
				break
			}
		}
	}
	m := messages[i]
	return d.messageID != uuid.Nil && m.Message != nil && m.ID == d.messageID
}

// dividerLine draws "── new ──" across the chat.
func (a *App) dividerLine(width int) string {
	label := " new "
	side := (width - len(label)) / 2
	if side < 3 {
		side = 3
	}
	bar := strings.Repeat("─", side)
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red)).Bold(true)
	return lipgloss.PlaceHorizontal(width, lipgloss.Center, style.Render(bar+label+bar))
}

// jumpToUnread scrolls the chat to the divider (Alt+U).
func (a *App) jumpToUnread() {
	if a.divider == nil || a.divider.line < 0 || a.currentChannel == nil || a.divider.channelID != a.currentChannel.ID {
		a.statusMessage = "Nothing new here"
		return
	}
	a.chatViewport.SetYOffset(max(0, a.divider.line-1))
}
