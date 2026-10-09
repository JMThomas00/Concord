package client

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// Threads (2026-10-09; vault: "Concord - Threads Plan"). A channel's message
// list holds only messages outside threads; each thread's replies live in
// ServerConnection.Threads, loaded when the thread is first opened. The chat
// draws visibleMessages(): the channel's messages, with an expanded
// thread's replies right after its first message, and message navigation
// moves through that same list. A thread is drawn in a box: minimised, its
// first message and its latest reply; expanded, every reply.
//
// Keys, in message navigation (Alt+M): t replies in a message's thread
// (starting one if it has none), Enter on a thread's first message expands
// or minimises it. Esc leaves a thread you're replying in.

// threadState is one thread as this client knows it.
type threadState struct {
	summary protocol.ThreadSummary // its own Following/Unread are this user's
	replies []*MessageDisplay      // oldest first, once loaded
	loaded  bool
	loading bool
}

// thread returns a thread's state, creating it (thread-safe).
func (sc *ServerConnection) thread(id uuid.UUID) *threadState {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.Threads == nil {
		sc.Threads = make(map[uuid.UUID]*threadState)
	}
	ts := sc.Threads[id]
	if ts == nil {
		ts = &threadState{summary: protocol.ThreadSummary{ThreadID: id}}
		sc.Threads[id] = ts
	}
	return ts
}

// existingThread returns a thread's state, or nil.
func (sc *ServerConnection) existingThread(id uuid.UUID) *threadState {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.Threads[id]
}

// replyCount is how many replies a thread has, as far as this client knows.
func (ts *threadState) replyCount() int {
	if ts == nil {
		return 0
	}
	return max(ts.summary.ReplyCount, len(ts.replies))
}

// threadDisplay turns a message from the server into the chat's form.
func threadDisplay(pd *protocol.MessageDisplay, self uuid.UUID, theme string) *MessageDisplay {
	d := &MessageDisplay{
		Message:     pd.Message,
		AuthorColor: theme,
		ShowHeader:  pd.Type != models.MessageTypeSystem,
		IsSystem:    pd.Type == models.MessageTypeSystem,
		IsWhisper:   pd.IsWhisper,
		IsDeleted:   pd.Deleted,
	}
	if pd.Author != nil {
		d.AuthorName = memberOrUserName(pd.Member, pd.Author.Username)
		d.IsOwn = pd.Author.ID == self
		d.IsBotAuthor = pd.Author.IsServiceAccount
	}
	if pd.Recipient != nil {
		d.RecipientName = pd.Recipient.Username
	}
	return d
}

func selfID(sc *ServerConnection) uuid.UUID {
	if sc == nil || sc.User == nil {
		return uuid.Nil
	}
	return sc.User.ID
}

// threadOnScreen reports whether a thread is open in front of the user right
// now: its channel on screen, on this server, expanded.
func (a *App) threadOnScreen(sc *ServerConnection, channelID, threadID uuid.UUID) bool {
	return a.view == ViewMain && a.activeConn == sc && a.currentChannel != nil &&
		a.currentChannel.ID == channelID && a.expandedThreads[threadID]
}

// applyThreadSummary records a thread's summary. A summary sent to the
// whole channel lists its followers instead of this user's own state: it's
// ours to follow when we're listed, and unread when someone else wrote the
// latest reply. A thread open on screen is read as it arrives.
func (a *App) applyThreadSummary(sc *ServerConnection, sum protocol.ThreadSummary) {
	self := selfID(sc)
	if len(sum.Followers) > 0 {
		sum.Following = false
		for _, id := range sum.Followers {
			if id == self {
				sum.Following = true
			}
		}
		sum.Unread = sum.Following && sum.LastReply != nil && sum.LastReply.AuthorID != self
		sum.Followers = nil
	}
	if sum.Unread && a.threadOnScreen(sc, sum.ChannelID, sum.ThreadID) {
		a.markThreadRead(sc, sum.ChannelID, sum.ThreadID)
		sum.Unread = false
	}
	ts := sc.thread(sum.ThreadID)
	sc.mu.Lock()
	ts.summary = sum
	sc.mu.Unlock()
}

// addThreadReply puts a new reply into its thread (if its replies are
// loaded; otherwise the summary that follows says enough). It reports
// whether this user follows the thread.
func (a *App) addThreadReply(sc *ServerConnection, d *MessageDisplay) bool {
	ts := sc.thread(*d.ThreadID)
	self := selfID(sc)
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if ts.loaded {
		dup := false
		for _, r := range ts.replies {
			if r.ID == d.ID {
				dup = true
			}
		}
		if !dup {
			ts.replies = append(ts.replies, d)
		}
	}
	following := ts.summary.Following || d.IsOwn
	if !following { // a thread we started follows us before its first summary arrives
		for _, m := range sc.Messages[d.ChannelID] {
			if m.ID == *d.ThreadID && m.AuthorID == self {
				following = true
			}
		}
	}
	return following
}

// visibleMessages is what the chat shows and message navigation moves
// through: the channel's messages, with each expanded thread's replies right
// after its first message.
func (a *App) visibleMessages() []*MessageDisplay {
	if a.activeConn == nil || a.currentChannel == nil {
		return nil
	}
	roots := a.activeConn.GetMessages(a.currentChannel.ID)
	if len(a.expandedThreads) == 0 {
		return roots
	}
	out := make([]*MessageDisplay, 0, len(roots))
	for _, m := range roots {
		out = append(out, m)
		if !a.expandedThreads[m.ID] {
			continue
		}
		if ts := a.activeConn.existingThread(m.ID); ts != nil {
			a.activeConn.mu.RLock()
			out = append(out, ts.replies...)
			a.activeConn.mu.RUnlock()
		}
	}
	return out
}

// selectedMessage is the message navigation's selection (nil for none).
func (a *App) selectedMessage() *MessageDisplay {
	msgs := a.visibleMessages()
	if a.messageNavIndex < 0 || a.messageNavIndex >= len(msgs) {
		return nil
	}
	return msgs[a.messageNavIndex]
}

// threadRootOf is the first message of the thread a message is in, or the
// message itself.
func (a *App) threadRootOf(m *MessageDisplay) *MessageDisplay {
	if m.ThreadID == nil || a.activeConn == nil {
		return m
	}
	for _, r := range a.activeConn.GetMessages(m.ChannelID) {
		if r.ID == *m.ThreadID {
			return r
		}
	}
	return m
}

// toggleThread expands a thread (loading its replies the first time, and
// reading it) or minimises it.
func (a *App) toggleThread(rootID uuid.UUID) {
	if a.expandedThreads == nil {
		a.expandedThreads = make(map[uuid.UUID]bool)
	}
	if a.expandedThreads[rootID] {
		delete(a.expandedThreads, rootID)
		a.updateChatContent()
		return
	}
	a.expandedThreads[rootID] = true
	a.openThread(rootID)
	a.updateChatContent()
}

// openThread loads a thread's replies if they aren't yet, and clears its
// unread mark.
func (a *App) openThread(rootID uuid.UUID) {
	sc, ch := a.activeConn, a.currentChannel
	if sc == nil || ch == nil {
		return
	}
	ts := sc.thread(rootID)
	sc.mu.Lock()
	load := !ts.loaded && !ts.loading
	if load {
		ts.loading = true
	}
	unread := ts.summary.Unread
	ts.summary.Unread = false
	sc.mu.Unlock()
	if load {
		a.sendThreadOp(sc, protocol.OpRequestThread, protocol.ThreadRequest{ChannelID: ch.ID, ThreadID: rootID})
	}
	if unread {
		a.markThreadRead(sc, ch.ID, rootID)
	}
}

// markThreadRead tells the server this thread is read, so its unread mark
// clears on every device.
func (a *App) markThreadRead(sc *ServerConnection, channelID, threadID uuid.UUID) {
	a.sendThreadOp(sc, protocol.OpThreadRead, protocol.ThreadReadPayload{ChannelID: channelID, ThreadID: threadID})
}

func (a *App) sendThreadOp(sc *ServerConnection, op protocol.OpCode, payload any) {
	if a.connMgr == nil || sc == nil {
		return
	}
	if msg, err := protocol.NewMessage(op, payload); err == nil {
		_ = a.connMgr.SendRaw(sc.ServerID, msg)
	}
}

// startThreadReply makes the message box post into the thread m is in (or
// starts one on m), and opens the thread.
func (a *App) startThreadReply(m *MessageDisplay) {
	root := a.threadRootOf(m)
	a.threadTarget = root
	a.threadQuote = extractFirstLineWithEllipsis(root.Content, 50)
	if root.IsDeleted {
		a.threadQuote = "the deleted message"
	}
	a.replyTarget = nil
	if a.expandedThreads == nil {
		a.expandedThreads = make(map[uuid.UUID]bool)
	}
	if a.activeConn != nil && a.activeConn.existingThread(root.ID).replyCount() > 0 {
		a.expandedThreads[root.ID] = true
		a.openThread(root.ID)
	}
	a.updateChatContent()
}

// handleThreadMessages takes a thread's replies (THREAD_MESSAGES).
func (a *App) handleThreadMessages(sc *ServerConnection, p protocol.ThreadMessagesPayload) {
	self := selfID(sc)
	replies := make([]*MessageDisplay, 0, len(p.Messages))
	for _, m := range p.Messages {
		replies = append(replies, threadDisplay(m, self, a.theme.Colors.Purple))
	}
	ts := sc.thread(p.ThreadID)
	sc.mu.Lock()
	ts.replies, ts.loaded, ts.loading = replies, true, false
	sc.mu.Unlock()
	if p.Summary != nil {
		a.applyThreadSummary(sc, *p.Summary)
	}
	if a.activeConn == sc && a.currentChannel != nil && a.currentChannel.ID == p.ChannelID {
		a.updateChatContent()
	}
}

// ── Drawing ─────────────────────────────────────────────────────────────────

// threadPart says how a message sits in a thread box: the first message of
// a thread with replies, or one of an expanded thread's replies (and
// whether it's the last).
type threadPart struct {
	boxed     bool
	root      bool
	expanded  bool
	lastReply bool
	state     *threadState
}

func (a *App) threadPartOf(msgs []*MessageDisplay, i int) threadPart {
	m := msgs[i]
	sc := a.activeConn
	if sc == nil {
		return threadPart{}
	}
	if m.ThreadID != nil { // a reply, shown because its thread is expanded
		last := i == len(msgs)-1 || msgs[i+1].ThreadID == nil || *msgs[i+1].ThreadID != *m.ThreadID
		return threadPart{boxed: true, expanded: true, lastReply: last, state: sc.existingThread(*m.ThreadID)}
	}
	ts := sc.existingThread(m.ID)
	expanded := a.expandedThreads[m.ID]
	if ts.replyCount() == 0 && !(expanded && ts != nil && ts.loading) {
		return threadPart{}
	}
	return threadPart{boxed: true, root: true, expanded: expanded, state: ts}
}

// threadBorder draws a thread box's top edge: + or − to open or close it,
// then how many replies, in the theme's cyan when some are unread.
func (a *App) threadBorder(p threadPart, width int) string {
	c := a.theme.Colors
	edge := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	sign := "+"
	if p.expanded {
		sign = "−"
	}
	n := p.state.replyCount()
	label := fmt.Sprintf(" 🧵 %d %s ", n, pluralWord(n, "reply", "replies"))
	labelStyle := edge
	if p.state != nil && p.state.summary.Unread {
		labelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
		label = fmt.Sprintf(" 🧵 %d %s · new ", n, pluralWord(n, "reply", "replies"))
	}
	signPart := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true).Render(" " + sign + " ")
	fill := max(1, width-2-1-3-ansi.StringWidth(label)-1)
	return edge.Render("╭─") + signPart + edge.Render(strings.Repeat("─", fill)) + labelStyle.Render(label) + edge.Render("─╮")
}

func (a *App) threadBottom(width int) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Render("╰" + strings.Repeat("─", max(0, width-2)) + "╯")
}

// threadSide wraps one rendered line in the box's sides, padded to width.
func (a *App) threadSide(line string, width int) string {
	edge := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	inner := width - 4
	if w := ansi.StringWidth(line); w > inner {
		line = ansi.Truncate(line, inner, "")
	} else {
		line += strings.Repeat(" ", inner-w)
	}
	return edge.Render("│ ") + line + edge.Render(" │")
}

// threadLatestLine is a minimised thread's second line: the latest reply,
// who wrote it and when.
func (a *App) threadLatestLine(ts *threadState, inner int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	if ts == nil {
		return ""
	}
	var who, text string
	var at time.Time
	switch {
	case ts.summary.LastReply != nil:
		lr := ts.summary.LastReply
		who = lr.Message.AuthorID.String()[:8]
		if lr.Author != nil {
			who = memberOrUserName(lr.Member, lr.Author.Username)
		}
		text = lr.Content
		at = lr.CreatedAt
	case len(ts.replies) > 0:
		lr := ts.replies[len(ts.replies)-1]
		who, text, at = lr.AuthorName, lr.Content, lr.CreatedAt
	default:
		return ""
	}
	when := ""
	if !at.IsZero() {
		when = " · " + agoText(time.Since(at))
	}
	room := max(10, inner-4-ansi.StringWidth(who)-ansi.StringWidth(when))
	return dim.Render("  ↳ ") + dim.Bold(true).Render(who) + dim.Render(": ") + a.previewLine(text, room) + dim.Render(when)
}

// boxThreadSegment draws one message's rendered lines inside its thread's
// box: the top edge before a thread's first message, the latest reply and
// bottom edge after a minimised one, and the bottom edge after an expanded
// thread's last reply. It returns the lines, and whether the box closed.
func (a *App) boxThreadSegment(segment string, p threadPart, width int, gap int) (string, bool) {
	segment = strings.TrimRight(segment, "\n")
	var b strings.Builder
	if p.root {
		b.WriteString(a.threadBorder(p, width) + "\n")
	}
	for _, l := range strings.Split(segment, "\n") {
		b.WriteString(a.threadSide(l, width) + "\n")
	}
	closed := false
	switch {
	case p.root && !p.expanded:
		if line := a.threadLatestLine(p.state, width-4); line != "" {
			b.WriteString(a.threadSide("", width) + "\n")
			b.WriteString(a.threadSide(line, width) + "\n")
		}
		closed = true
	case p.root && p.expanded && (p.state == nil || (!p.state.loaded && len(p.state.replies) == 0)):
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Italic(true)
		b.WriteString(a.threadSide("", width) + "\n")
		b.WriteString(a.threadSide(dim.Render("  Loading replies…"), width) + "\n")
		closed = true
	case p.lastReply:
		closed = true
	}
	if closed {
		b.WriteString(a.threadBottom(width) + "\n")
	} else {
		for range gap {
			b.WriteString(a.threadSide("", width) + "\n")
		}
	}
	return b.String(), closed
}

func pluralWord(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// agoText is a short "how long ago": now, 5m ago, 3h ago, 2d ago.
func agoText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// markLastToastThread turns the newest toast (a message's) into a thread's:
// "🧵 Thread in #channel", and a click opens the thread expanded.
func (a *App) markLastToastThread(threadID uuid.UUID, channelName string) {
	if len(a.toasts) == 0 {
		return
	}
	t := a.toasts[len(a.toasts)-1]
	t.threadID = threadID
	if !t.mention {
		t.label = strings.Replace(t.label, "💬 #"+channelName, "🧵 Thread in #"+channelName, 1)
		t.short1 = "🧵 #" + channelName
	}
}

// openThreadAt shows a thread from elsewhere (a toast): its channel, the
// thread expanded, and the message box replying in it.
func (a *App) openThreadAt(serverID, channelID, threadID uuid.UUID) {
	a.goToChannel(serverID, channelID)
	if a.activeConn == nil {
		return
	}
	for _, m := range a.activeConn.GetMessages(channelID) {
		if m.ID == threadID {
			a.startThreadReply(m)
			return
		}
	}
}

// sameThreadGroup reports whether message i may share an author line with
// the one before it: both outside thread boxes, or both in the same thread.
// Grouping across a box's edge would leave the message after a thread
// without its author.
func (a *App) sameThreadGroup(msgs []*MessageDisplay, i int) bool {
	prev, cur := a.threadPartOf(msgs, i-1), a.threadPartOf(msgs, i)
	if !prev.boxed && !cur.boxed {
		return true
	}
	if !prev.boxed || !cur.boxed {
		return false
	}
	key := func(m *MessageDisplay) uuid.UUID {
		if m.ThreadID != nil {
			return *m.ThreadID
		}
		return m.ID
	}
	return key(msgs[i-1]) == key(msgs[i]) && cur.expanded
}

// markThreadReplyBox says, in the message box's borders, that it's posting
// into a thread (top right: which, its opening words kept short) and how to
// leave (bottom right).
func (a *App) markThreadReplyBox(box string, edge lipgloss.Style) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Italic(true)
	width := lipgloss.Width(strings.SplitN(box, "\n", 2)[0])
	quote := ansi.Truncate(a.threadQuote, max(10, min(32, width/2-24)), "…")
	box = embedBorderRight(box, style.Render(fmt.Sprintf("Replying in thread: \"%s\"", quote)), edge, true)
	return embedBorderRight(box, style.Render("Esc to leave thread"), edge, false)
}

// embedBorderRight writes text into a box's top or bottom border, near its
// right end: "─ text ─╮" or "─ text ─╯". Text too wide for the border is
// left out.
func embedBorderRight(box, text string, edge lipgloss.Style, top bool) string {
	lines := strings.Split(box, "\n")
	i := len(lines) - 1
	left, right := "╰", "╯"
	if top {
		i, left, right = 0, "╭", "╮"
	}
	width := lipgloss.Width(lines[i])
	tw := lipgloss.Width(text)
	if tw+8 > width {
		return box
	}
	lines[i] = edge.Render(left+strings.Repeat("─", width-tw-5)) + " " + text + " " + edge.Render("─"+right)
	return strings.Join(lines, "\n")
}

// previewLine is a message's first line of text (past any reply quote) as
// the chat shows it, markdown and all (*this* in italics, not asterisks),
// cut to width.
func (a *App) previewLine(text string, width int) string {
	line := ""
	for _, l := range strings.Split(text, "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "↩ ") {
			line = t
			break
		}
	}
	if line == "" {
		return ""
	}
	rendered := a.renderMessageContent("", line, max(40, width+20), false)
	for _, l := range strings.Split(rendered, "\n") {
		if strings.TrimSpace(ansi.Strip(l)) != "" {
			return ansi.Truncate(strings.TrimSpace(l), width, "…")
		}
	}
	return ""
}
