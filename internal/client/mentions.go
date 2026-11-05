package client

// The mentions inbox (Alt+I, /mentions; servers v0.1.9+): every message
// that called for you on every server you're connected to, newest first.
// Each server answers for its own (OpRequestMentions → MENTIONS); read or
// unread comes from the read positions, so it agrees with the @ badges.
// Enter jumps to the message: its server and channel, history loaded
// around it, its thread opened, and the message selected.

import (
	"fmt"
	"log"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

// Inbox filters, in Tab order.
var mentionFilters = []string{"All", "@me", "Replies", "@everyone"}

type mentionsInbox struct {
	filter  int
	cursor  int
	servers map[uuid.UUID]*inboxServer
	order   []uuid.UUID // servers in the order they were asked
}

type inboxServer struct {
	name    string
	items   []inboxItem
	loading bool
	more    bool
	before  int64 // the oldest Seq loaded, for the next page
	old     bool  // a server before v0.1.9: no inbox
}

type inboxItem struct {
	serverID, channelID uuid.UUID
	msg                 *MessageDisplay
	kind                string
	unread              bool
}

// mentionJump is a jump to a message in progress: it lands once the
// channel's history (and, for a thread reply, the thread) has loaded.
type mentionJump struct {
	serverID, channelID, messageID uuid.UUID
	threadID                       *uuid.UUID
	waitingThread                  bool
}

// root is the message the channel's history shows: the message itself, or
// its thread's first message.
func (j *mentionJump) root() uuid.UUID {
	if j.threadID != nil {
		return *j.threadID
	}
	return j.messageID
}

// openMentions opens the inbox and asks every connected server for its
// newest mentions.
func (a *App) openMentions() {
	in := &mentionsInbox{servers: map[uuid.UUID]*inboxServer{}}
	if a.connMgr != nil {
		for _, sc := range a.connMgr.GetConnectedServers() {
			s := &inboxServer{name: serverName(sc), old: !keepsReads(sc)}
			in.servers[sc.ServerID] = s
			in.order = append(in.order, sc.ServerID)
		}
	}
	if len(in.order) == 0 {
		a.statusMessage = "Not connected to any server"
		a.statusError = false
		return
	}
	a.mentions = in
	for _, id := range in.order {
		if !in.servers[id].old {
			a.requestMentions(id, 0)
		}
	}
}

func serverName(sc *ServerConnection) string {
	if sc.ServerInfo != nil && sc.ServerInfo.Name != "" {
		return sc.ServerInfo.Name
	}
	return "server"
}

// requestMentions asks one server for a page of mentions before Seq before
// (0: the newest).
func (a *App) requestMentions(serverID uuid.UUID, before int64) {
	in := a.mentions
	if in == nil || in.servers[serverID] == nil || a.connMgr == nil {
		return
	}
	msg, err := protocol.NewMessage(protocol.OpRequestMentions, protocol.MentionsRequest{ServerID: serverID, Before: before})
	if err != nil {
		return
	}
	if err := a.connMgr.SendRaw(serverID, msg); err != nil {
		log.Printf("mentions: %v", err)
		return
	}
	in.servers[serverID].loading = true
}

// handleMentions takes a page of one server's mentions (MENTIONS).
func (a *App) handleMentions(serverID uuid.UUID, sc *ServerConnection, p protocol.MentionsPayload) {
	in := a.mentions
	if in == nil || in.servers[serverID] == nil {
		return
	}
	s := in.servers[serverID]
	self := selfID(sc)
	page := make([]inboxItem, 0, len(p.Items))
	for _, it := range p.Items {
		if it.Message == nil || it.Message.Message == nil {
			continue
		}
		page = append(page, inboxItem{
			serverID:  serverID,
			channelID: it.Message.ChannelID,
			msg:       threadDisplay(it.Message, self, a.theme.Colors.Purple),
			kind:      it.Kind,
			unread:    it.Unread,
		})
	}
	if p.Before == 0 {
		s.items = page
	} else {
		s.items = append(s.items, page...)
	}
	s.loading, s.more = false, p.HasMore
	if n := len(s.items); n > 0 {
		s.before = s.items[n-1].msg.Seq
	}
	in.cursor = min(in.cursor, max(0, len(a.inboxList())-1))
}

// inboxList is what the inbox shows: every server's mentions that pass the
// filter, newest first.
func (a *App) inboxList() []inboxItem {
	in := a.mentions
	if in == nil {
		return nil
	}
	var out []inboxItem
	for _, id := range in.order {
		for _, it := range in.servers[id].items {
			if mentionPasses(it.kind, in.filter) {
				out = append(out, it)
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].msg.CreatedAt.After(out[j].msg.CreatedAt) })
	return out
}

func mentionPasses(kind string, filter int) bool {
	switch filter {
	case 1:
		return kind == protocol.MentionDirect || kind == protocol.MentionWhisper
	case 2:
		return kind == protocol.MentionReply
	case 3:
		return kind == protocol.MentionEveryone
	}
	return true
}

// mentionArrived refreshes an open inbox's first page when a new mention
// comes in on that server.
func (a *App) mentionArrived(serverID uuid.UUID) {
	if in := a.mentions; in != nil && in.servers[serverID] != nil && !in.servers[serverID].old {
		a.requestMentions(serverID, 0)
	}
}

// inboxChannelRead marks an open inbox's top-level mentions in a channel
// read up to a read position (CHANNEL_READ).
func (a *App) inboxChannelRead(serverID, channelID uuid.UUID, readSeq int64) {
	in := a.mentions
	if in == nil || in.servers[serverID] == nil {
		return
	}
	items := in.servers[serverID].items
	for i := range items {
		if items[i].channelID == channelID && items[i].msg.ThreadID == nil && items[i].msg.Seq <= readSeq {
			items[i].unread = false
		}
	}
}

// handleMentionsKey moves through the inbox, filters, jumps or closes.
func (a *App) handleMentionsKey(msg tea.KeyMsg) tea.Cmd {
	in := a.mentions
	list := a.inboxList()
	switch msg.String() {
	case "up", "k":
		in.cursor = max(0, in.cursor-1)
	case "down", "j":
		in.cursor = min(max(0, len(list)-1), in.cursor+1)
	case "pgup":
		in.cursor = max(0, in.cursor-10)
	case "pgdown":
		in.cursor = min(max(0, len(list)-1), in.cursor+10)
	case "home":
		in.cursor = 0
	case "end":
		in.cursor = max(0, len(list)-1)
	case "tab":
		in.filter, in.cursor = (in.filter+1)%len(mentionFilters), 0
	case "shift+tab":
		in.filter, in.cursor = (in.filter+len(mentionFilters)-1)%len(mentionFilters), 0
	case "r", "R":
		a.markAllMentionsRead()
	case "enter":
		if in.cursor < len(list) {
			a.mentions = nil
			return a.jumpToMention(list[in.cursor])
		}
	case "esc", "alt+i", "q":
		a.mentions = nil
		return nil
	}
	// Near the end of what's loaded: the next page from each server with more.
	if a.mentions != nil && in.cursor >= len(a.inboxList())-5 {
		for _, id := range in.order {
			if s := in.servers[id]; s.more && !s.loading {
				a.requestMentions(id, s.before)
			}
		}
	}
	return nil
}

// markAllMentionsRead reads every channel and thread with an unread
// mention (R).
func (a *App) markAllMentionsRead() {
	in := a.mentions
	n := 0
	for _, id := range in.order {
		sc := a.connMgr.GetConnection(id)
		done := map[uuid.UUID]bool{}
		items := in.servers[id].items
		for i := range items {
			it := &items[i]
			if !it.unread || sc == nil {
				continue
			}
			it.unread = false
			n++
			key := it.channelID
			if it.msg.ThreadID != nil {
				key = *it.msg.ThreadID
			}
			if done[key] {
				continue
			}
			done[key] = true
			if it.msg.ThreadID != nil {
				a.markThreadRead(sc, it.channelID, *it.msg.ThreadID)
			} else {
				a.sendChannelRead(sc, it.channelID)
			}
		}
	}
	if n == 0 {
		a.statusMessage = "Nothing unread"
	} else {
		a.statusMessage = fmt.Sprintf("Marked %d %s read", n, pluralWord(n, "mention", "mentions"))
	}
	a.statusError = false
}

// jumpToMention goes to a mention: straight there when it's already on
// screen, otherwise by opening its channel with history around it.
func (a *App) jumpToMention(it inboxItem) tea.Cmd {
	j := &mentionJump{serverID: it.serverID, channelID: it.channelID, messageID: it.msg.ID, threadID: it.msg.ThreadID}
	a.pendingJump = j
	if a.activeConn != nil && a.activeConn.ServerID == j.serverID && a.currentChannel != nil && a.currentChannel.ID == j.channelID {
		for _, m := range a.activeConn.GetMessages(j.channelID) {
			if m.ID == j.root() {
				return a.landJump()
			}
		}
	}
	a.goToChannel(j.serverID, j.channelID) // selectChannel asks for history around it
	if a.activeConn == nil || a.activeConn.ServerID != j.serverID || a.currentChannel == nil || a.currentChannel.ID != j.channelID {
		a.pendingJump = nil
		a.statusMessage = "That channel isn't available any more"
		a.statusError = true
	}
	return nil
}

// jumpAround is the message a channel's history should be loaded around,
// for a jump in progress to it (nil: the newest, as usual).
func (a *App) jumpAround(serverID, channelID uuid.UUID) *uuid.UUID {
	j := a.pendingJump
	if j == nil || j.serverID != serverID || j.channelID != channelID {
		return nil
	}
	root := j.root()
	return &root
}

// jumpHistoryLoaded lands a jump once its channel's history is in.
func (a *App) jumpHistoryLoaded(serverID uuid.UUID, p protocol.MessageHistoryPayload) tea.Cmd {
	j := a.pendingJump
	if j == nil || j.serverID != serverID || j.channelID != p.ChannelID || p.Around == nil || *p.Around != j.root() {
		return nil
	}
	cmd := a.landJump()
	if p.NewerMissing {
		a.statusMessage = "Showing the messages around it: open the channel again for the newest"
		a.statusError = false
	}
	return cmd
}

// jumpThreadLoaded lands a jump to a thread reply once the thread's
// replies are in.
func (a *App) jumpThreadLoaded(serverID uuid.UUID, threadID uuid.UUID) tea.Cmd {
	j := a.pendingJump
	if j == nil || !j.waitingThread || j.serverID != serverID || j.threadID == nil || *j.threadID != threadID {
		return nil
	}
	return a.landJump()
}

// landJump selects the jump's message (opening its thread first), as
// Alt+M would, and scrolls to it.
func (a *App) landJump() tea.Cmd {
	j := a.pendingJump
	sc := a.activeConn
	if j == nil || sc == nil {
		return nil
	}
	if j.threadID != nil && !j.waitingThread {
		if a.expandedThreads == nil {
			a.expandedThreads = make(map[uuid.UUID]bool)
		}
		a.expandedThreads[*j.threadID] = true
		a.openThread(*j.threadID)
		a.markThreadRead(sc, j.channelID, *j.threadID)
		ts := sc.thread(*j.threadID)
		sc.mu.RLock()
		loaded := ts.loaded
		sc.mu.RUnlock()
		if !loaded {
			j.waitingThread = true // THREAD_MESSAGES lands it
			a.updateChatContent()
			return nil
		}
	}
	a.pendingJump = nil
	a.updateChatContent()
	msgs := a.visibleMessages()
	at := -1
	for i, m := range msgs {
		if m.ID == j.messageID {
			at = i
			break
		}
		if m.ID == j.root() {
			at = i // the thread's first message, if the reply isn't there
		}
	}
	if at < 0 {
		a.statusMessage = "That message isn't in the history any more"
		a.statusError = true
		return nil
	}
	return a.selectMessageAt(at)
}

// selectMessageAt enters message navigation (Alt+M) with message i of
// visibleMessages selected, scrolled a third of the way down the chat.
func (a *App) selectMessageAt(i int) tea.Cmd {
	a.messageNavMode = true
	a.inMessageEditMode = false
	a.focus = FocusMessageNav
	a.messageNavIndex = i
	a.messageSelectionStart, a.messageSelectionEnd = nil, nil
	a.input.Blur()
	a.updateChatContent()
	a.chatViewport.SetYOffset(max(0, a.calculateMessageLinePosition(i)-a.chatViewport.Height/3))
	return tea.HideCursor
}

// mentionTotal is the unread @mentions across every server (the status bar).
func (a *App) mentionTotal() int {
	n := 0
	for _, chans := range a.mentionCounts {
		for _, c := range chans {
			n += c
		}
	}
	return n
}

// ── Drawing ─────────────────────────────────────────────────────────────────

// mentionKindLabel is the short tag in each row.
func mentionKindLabel(kind string) string {
	switch kind {
	case protocol.MentionReply:
		return "↩ reply"
	case protocol.MentionWhisper:
		return "whisper"
	case protocol.MentionEveryone:
		return "@all"
	}
	return "@you"
}

// renderMentions draws the inbox as a box over the chat.
func (a *App) renderMentions() string {
	in := a.mentions
	c := a.theme.Colors
	w := min(110, max(50, a.width-8))
	inner := w - 4
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(c.Selection)).Foreground(lipgloss.Color(c.Foreground))
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Background(lipgloss.Color(c.Purple)).Bold(true)

	list := a.inboxList()
	var tabs []string
	for i, f := range mentionFilters {
		if i == in.filter {
			tabs = append(tabs, on.Render(" "+f+" "))
		} else {
			tabs = append(tabs, dim.Render(" "+f+" "))
		}
	}
	head := title.Render("@ Mentions")
	tabLine := strings.Join(tabs, "")
	gap := max(2, inner-ansi.StringWidth(head)-ansi.StringWidth(tabLine))
	var b strings.Builder
	b.WriteString(head + strings.Repeat(" ", gap) + tabLine + "\n\n")

	loading := false
	for _, id := range in.order {
		loading = loading || in.servers[id].loading
	}
	rows := max(3, a.height-16)
	if len(list) == 0 {
		switch {
		case loading:
			b.WriteString(dim.Render("  Loading…") + "\n")
		case in.filter == 0:
			b.WriteString(dim.Render("  Nothing has mentioned you yet.") + "\n")
		default:
			b.WriteString(dim.Render("  None of these.") + "\n")
		}
	}
	start := min(max(0, in.cursor-rows/2), max(0, len(list)-rows))
	multi := len(in.order) > 1
	for i := start; i < start+rows && i < len(list); i++ {
		it := list[i]
		mark := "  "
		if it.unread {
			mark = hot.Render("● ")
		}
		where := "#" + a.channelNameOn(it.serverID, it.channelID)
		if multi {
			where = in.servers[it.serverID].name + " › " + where
		}
		if it.msg.ThreadID != nil {
			where += " 🧵"
		}
		where = ansi.Truncate(where, 30, "…")
		who := ansi.Truncate(it.msg.AuthorName, 14, "…")
		when := a.formatTimestamp(it.msg.CreatedAt)
		meta := fmt.Sprintf("%-7s  %s", mentionKindLabel(it.kind), when)
		left := where + strings.Repeat(" ", max(1, 32-ansi.StringWidth(where))) + who + strings.Repeat(" ", max(1, 16-ansi.StringWidth(who)))
		room := max(8, inner-2-ansi.StringWidth(left)-ansi.StringWidth(meta)-2)
		text := ansi.Truncate(strings.Join(strings.Fields(it.msg.Content), " "), room, "…")
		pad := max(1, inner-2-ansi.StringWidth(left)-ansi.StringWidth(text)-ansi.StringWidth(meta))
		line := left + text + strings.Repeat(" ", pad) + meta
		if i == in.cursor {
			b.WriteString(mark + sel.Render(line) + "\n")
		} else {
			b.WriteString(mark + left + text + dim.Render(strings.Repeat(" ", pad)+meta) + "\n")
		}
	}

	var notes []string
	if loading && len(list) > 0 {
		notes = append(notes, "loading more…")
	}
	for _, id := range in.order {
		if s := in.servers[id]; s.old {
			notes = append(notes, s.name+" is older than v0.1.9, so it has no inbox")
		}
	}
	if len(notes) > 0 {
		b.WriteString("\n" + dim.Render(strings.Join(notes, " · ")) + "\n")
	}
	b.WriteString("\n" + dim.Render("↑↓ choose · Enter go to it · Tab filter · R mark all read · Esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Purple)).
		Padding(0, 1).
		Width(w - 2).
		Background(lipgloss.Color(c.Background)).
		Render(b.String())
}

// channelNameOn is a channel's name on a server ("?" when unknown).
func (a *App) channelNameOn(serverID, channelID uuid.UUID) string {
	if a.connMgr == nil {
		return "?"
	}
	sc := a.connMgr.GetConnection(serverID)
	if sc == nil {
		return "?"
	}
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	for _, chans := range sc.Channels {
		for _, ch := range chans {
			if ch.ID == channelID {
				return ch.Name
			}
		}
	}
	return "?"
}
