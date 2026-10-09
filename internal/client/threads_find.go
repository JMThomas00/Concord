package client

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// Finding threads again (Threads Plan, phase 3): new messages push a thread
// up and out of view, so the way back never scrolls away.
//   - The chat box's bottom edge, right-aligned (Jordan's sketch):
//     "3 active threads · 1 unread · Alt+T"; for a few seconds after a reply
//     in a thread you follow, who replied.
//   - Alt+T (or /threads): every thread in the channel, latest activity
//     first; Enter opens one, expanded, with the message box replying in it.
//   - Optional, off by default (Settings > Notifications > Thread Replies in
//     the Channel): a dim line in the channel saying who replied where,
//     only the latest per thread; a click opens the thread.

// threadActiveFor is how recent a thread's last reply must be to count as
// active in the bottom-edge indicator.
const threadActiveFor = 24 * time.Hour

// threadFlashFor is how long "↳ who replied" shows in the bottom edge.
const threadFlashFor = 6 * time.Second

// Thread Replies in the Channel (Settings > Notifications).
const (
	ThreadLinesOff      = ""
	ThreadLinesFollowed = "followed"
	ThreadLinesAll      = "all"
)

// channelThreads lists the current channel's threads with replies, the
// latest activity first.
func (a *App) channelThreads() []threadListItem {
	if a.activeConn == nil || a.currentChannel == nil {
		return nil
	}
	var items []threadListItem
	for _, m := range a.activeConn.GetMessages(a.currentChannel.ID) {
		ts := a.activeConn.existingThread(m.ID)
		if ts.replyCount() == 0 {
			continue
		}
		a.activeConn.mu.RLock()
		it := threadListItem{root: m, replies: ts.replyCount(), unread: ts.summary.Unread, last: m.CreatedAt}
		if ts.summary.LastReplyAt != nil {
			it.last = *ts.summary.LastReplyAt
		}
		if lr := ts.summary.LastReply; lr != nil && lr.Author != nil {
			it.lastWho = memberOrUserName(lr.Member, lr.Author.Username)
		}
		a.activeConn.mu.RUnlock()
		items = append(items, it)
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].last.After(items[j].last) })
	return items
}

type threadListItem struct {
	root    *MessageDisplay
	replies int
	unread  bool
	last    time.Time
	lastWho string
}

// threadIndicator is the chat box's bottom-edge text (empty for none), and
// a shorter form for when the typing indicator needs the room.
func (a *App) threadIndicator(now time.Time) (full, short string) {
	items := a.channelThreads()
	active, unread := 0, 0
	for _, it := range items {
		if now.Sub(it.last) < threadActiveFor {
			active++
		}
		if it.unread {
			unread++
		}
	}
	if active == 0 && unread == 0 && (a.threadFlash == "" || now.After(a.threadFlashUntil)) {
		return "", ""
	}
	c := a.theme.Colors
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	var parts []string
	if a.threadFlash != "" && now.Before(a.threadFlashUntil) {
		parts = append(parts, hot.Render("↳ "+a.threadFlash+" replied"))
	} else {
		parts = append(parts, dim.Render(fmt.Sprintf("%d active %s", active, pluralWord(active, "thread", "threads"))))
	}
	if unread > 0 {
		parts = append(parts, hot.Render(fmt.Sprintf("%d unread", unread)))
	}
	parts = append(parts, dim.Render("Alt+T"))
	sep := dim.Render(" · ")
	full = strings.Join(parts, sep)
	short = dim.Render("🧵 ") + hot.Render(fmt.Sprint(unread)) + sep + dim.Render("Alt+T")
	if unread == 0 {
		short = dim.Render(fmt.Sprintf("🧵 %d · Alt+T", active))
	}
	return full, short
}

// embedChatBottom writes the chat box's bottom edge: the typing indicator
// on the left (it wins when both don't fit) and the thread indicator on the
// right.
func (a *App) embedChatBottom(box, typing string, borderStyle lipgloss.Style) string {
	full, short := a.threadIndicator(time.Now())
	if full == "" {
		return embedBorderBottom(box, typing, borderStyle)
	}
	lines := strings.Split(box, "\n")
	last := len(lines) - 1
	width := lipgloss.Width(lines[last])
	left := 0
	if typing != "" {
		left = lipgloss.Width(typing) + 2 // " text "
	}
	right := full
	for _, cand := range []string{full, short, ""} {
		right = cand
		if lipgloss.Width(cand)+2+left+4 <= width {
			break
		}
	}
	if right == "" {
		return embedBorderBottom(box, typing, borderStyle)
	}
	var b strings.Builder
	b.WriteString(borderStyle.Render("╰─"))
	used := 2
	if typing != "" {
		b.WriteString(" " + typing + " ")
		used += left
	}
	fill := width - used - lipgloss.Width(right) - 2 - 2
	b.WriteString(borderStyle.Render(strings.Repeat("─", max(1, fill))))
	b.WriteString(" " + right + " ")
	b.WriteString(borderStyle.Render("─╯"))
	lines[last] = b.String()
	return strings.Join(lines, "\n")
}

// flashThreadReply shows who replied in a followed thread in the bottom
// edge for a moment; the returned command redraws once it's over.
func (a *App) flashThreadReply(who string) tea.Cmd {
	a.threadFlash, a.threadFlashUntil = who, time.Now().Add(threadFlashFor)
	return tea.Tick(threadFlashFor+50*time.Millisecond, func(time.Time) tea.Msg { return threadFlashDoneMsg{} })
}

type threadFlashDoneMsg struct{}

// ── The thread list (Alt+T) ─────────────────────────────────────────────────

type threadListState struct {
	items  []threadListItem
	cursor int
}

// openThreadList opens the list of the channel's threads.
func (a *App) openThreadList() {
	items := a.channelThreads()
	if len(items) == 0 {
		a.statusMessage = "No threads in this channel yet: Alt+M, then t on a message, starts one"
		a.statusError = false
		return
	}
	a.threadList = &threadListState{items: items}
}

// handleThreadListKey moves through the list, opens a thread, or closes.
func (a *App) handleThreadListKey(msg tea.KeyMsg) tea.Cmd {
	l := a.threadList
	switch msg.String() {
	case "up", "k":
		l.cursor = max(0, l.cursor-1)
	case "down", "j":
		l.cursor = min(len(l.items)-1, l.cursor+1)
	case "home":
		l.cursor = 0
	case "end":
		l.cursor = len(l.items) - 1
	case "enter":
		root := l.items[l.cursor].root
		a.threadList = nil
		return a.jumpToThread(root)
	case "esc", "alt+t", "q":
		a.threadList = nil
	}
	return nil
}

// jumpToThread shows a thread: expanded, scrolled to, and the message box
// replying in it.
func (a *App) jumpToThread(root *MessageDisplay) tea.Cmd {
	a.startThreadReply(root)
	if a.expandedThreads == nil {
		a.expandedThreads = make(map[uuid.UUID]bool)
	}
	if !a.expandedThreads[root.ID] {
		a.expandedThreads[root.ID] = true
		a.openThread(root.ID)
	}
	a.updateChatContent()
	for i, m := range a.visibleMessages() {
		if m.ID == root.ID {
			a.chatViewport.SetYOffset(max(0, a.calculateMessageLinePosition(i)-1))
			break
		}
	}
	a.messageNavMode = false
	a.focus = FocusInput
	return a.input.Focus()
}

// renderThreadList draws the thread list as a box over the chat.
func (a *App) renderThreadList() string {
	l := a.threadList
	c := a.theme.Colors
	w := min(84, max(40, a.width-10))
	inner := w - 4
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Cyan)).Bold(true)
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(c.Selection)).Foreground(lipgloss.Color(c.Foreground))

	var b strings.Builder
	name := ""
	if a.currentChannel != nil {
		name = a.currentChannel.Name
	}
	b.WriteString(title.Render("🧵 Threads in #"+name) + dim.Render(fmt.Sprintf("  %d", len(l.items))) + "\n\n")
	rows := min(len(l.items), max(3, a.height-14))
	start := min(max(0, l.cursor-rows/2), max(0, len(l.items)-rows))
	now := time.Now()
	for i := start; i < start+rows && i < len(l.items); i++ {
		it := l.items[i]
		mark := "  "
		if it.unread {
			mark = hot.Render("● ")
		}
		meta := fmt.Sprintf("%d %s · %s", it.replies, pluralWord(it.replies, "reply", "replies"), agoText(now.Sub(it.last)))
		if it.lastWho != "" {
			meta += " · " + it.lastWho
		}
		who := it.root.AuthorName
		text := it.root.Content
		if it.root.IsDeleted {
			text = "(deleted)"
		}
		head := who + ": " + strings.Join(strings.Fields(text), " ")
		room := max(10, inner-2-ansi.StringWidth(meta)-2)
		head = ansi.Truncate(head, room, "…")
		pad := max(1, inner-2-ansi.StringWidth(head)-ansi.StringWidth(meta))
		line := head + strings.Repeat(" ", pad) + meta
		if i == l.cursor {
			b.WriteString(mark + sel.Render(line) + "\n")
		} else {
			b.WriteString(mark + line[:len(head)] + dim.Render(line[len(head):]) + "\n")
		}
	}
	b.WriteString("\n" + dim.Render("↑↓ choose · Enter open (and reply) · Esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Purple)).
		Padding(0, 1).
		Width(w - 2).
		Background(lipgloss.Color(c.Background)).
		Render(b.String())
}

// ── The in-channel line (optional) ──────────────────────────────────────────

// threadNotice is the latest reply in one thread, for the optional line in
// the channel.
type threadNotice struct {
	threadID uuid.UUID
	at       time.Time
	who      string
	quote    string // the thread's first words
	followed bool   // the reader follows the thread (or was mentioned)
}

// noteThreadReply remembers a thread's latest reply for the in-channel line;
// only the latest per thread is kept.
func (a *App) noteThreadReply(sc *ServerConnection, d *MessageDisplay, followed bool) {
	quote := ""
	for _, m := range sc.GetMessages(d.ChannelID) {
		if m.ID == *d.ThreadID {
			quote = extractFirstLineWithEllipsis(m.Content, 40)
		}
	}
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if sc.ThreadNotices == nil {
		sc.ThreadNotices = make(map[uuid.UUID]map[uuid.UUID]threadNotice)
	}
	if sc.ThreadNotices[d.ChannelID] == nil {
		sc.ThreadNotices[d.ChannelID] = make(map[uuid.UUID]threadNotice)
	}
	sc.ThreadNotices[d.ChannelID][*d.ThreadID] = threadNotice{
		threadID: *d.ThreadID, at: d.CreatedAt, who: d.AuthorName, quote: quote, followed: followed,
	}
}

// channelNotices is the current channel's in-channel lines to show, oldest
// first, as the setting allows.
func (a *App) channelNotices() []threadNotice {
	mode := a.notifConfig.ThreadLines
	if mode == ThreadLinesOff || a.activeConn == nil || a.currentChannel == nil {
		return nil
	}
	a.activeConn.mu.RLock()
	var out []threadNotice
	for _, n := range a.activeConn.ThreadNotices[a.currentChannel.ID] {
		if mode == ThreadLinesAll || n.followed {
			out = append(out, n)
		}
	}
	a.activeConn.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].at.Before(out[j].at) })
	return out
}

// noticeLine draws one in-channel line.
func (a *App) noticeLine(n threadNotice) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment)).Italic(true)
	return dim.Render(fmt.Sprintf("  ↳ %s replied in a thread: \"%s\" · click, or Alt+T", n.who, n.quote))
}
