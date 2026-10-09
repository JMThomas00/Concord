package client

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/mattn/go-runewidth"
)

// Toasts: cards in a bottom corner, stacked with the newest at the bottom: a
// new one slides in and the rest move up above it, at most maxToasts of them
// (fewer on a short window); past that, the top card counts the ones waiting
// above. In the main window they stay inside the side they're on (the
// server and channel columns on the left, the members panel on the right),
// so they never cover the chat or the message box. Settings > Notifications
// picks the side, and the style: full cards in a border of the theme's
// accent, or compact two-line ones. Notifications (messages, mentions,
// achievements, firsts) stay until they're dealt with: a click dismisses one
// (a message's opens its channel), and Ctrl+X dismisses the bottom one, when
// the next drops down into its place. The login stage's little flourishes
// (brief) come and go by themselves.

const (
	briefToastTime = 4 * time.Second // a flourish's time on screen
	toastSlide     = 250 * time.Millisecond
	maxToasts      = 6
)

// Toast sides and styles (Settings > Notifications).
const (
	ToastSideLeft     = ""      // over the server and channel columns (the default)
	ToastSideRight    = "right" // over the members panel
	ToastStyleFull    = ""      // a bordered card: what, where, and the message
	ToastStyleCompact = "compact"
)

// toastRows is how tall one card is: border, label, text, border; or the
// two lines of a compact one.
func (a *App) toastRows() int {
	if a.notifConfig.ToastStyle == ToastStyleCompact {
		return 2
	}
	return 4
}

type toast struct {
	label string // the first line; "" for an achievement
	title string

	// A compact card's two lines, when they differ from label and title (a
	// message's: the channel, then the server and who wrote it).
	short1, short2 string

	// A message's toast knows where the message is, so a click goes there.
	serverID, channelID uuid.UUID
	mention             bool

	brief bool // goes by itself (a flourish, not a notification)

	created time.Time
	shown   time.Time // when it first appeared on screen

	// Where it is in the stack: it slides from fromRow to toRow from moveAt.
	fromRow, toRow float64
	moveAt         time.Time
}

// row is the toast's top row now, mid-slide or settled.
func (t *toast) row(now time.Time) float64 {
	k := float64(now.Sub(t.moveAt)) / float64(toastSlide)
	if k >= 1 {
		return t.toRow
	}
	return t.fromRow + (t.toRow-t.fromRow)*easeOutCubic(math.Max(0, k))
}

// toastRect is where a toast was drawn, for clicks.
type toastRect struct {
	row, col, rows, cols int
	t                    *toast
}

// toastFit is how many toasts fit in the stack: maxToasts, and never more
// than half the window's height.
func (a *App) toastFit() int {
	return max(1, min(maxToasts, (a.height/2)/a.toastRows()))
}

// toastArea is the columns cards may be drawn in, and whether they enter
// from the right. In the main window that's one side's panels (renderMainView
// records their widths); elsewhere, a corner up to 56 columns wide. A members
// panel too narrow for a card (hidden or collapsed) sends them to the left.
func (a *App) toastArea() (col, width int, right bool) {
	right = a.notifConfig.ToastSide == ToastSideRight
	if a.view == ViewMain && a.toastLeftW > 0 {
		if right && a.toastRightW >= 24 {
			return a.width - a.toastRightW, a.toastRightW, true
		}
		return 0, a.toastLeftW, false
	}
	w := min(56, a.width)
	if right {
		return a.width - w, w, true
	}
	return 0, w, false
}

// Toast orders (Settings > Notifications > Toast Order).
const (
	ToastOrderNewest = ""       // the newest at the bottom, dismissed first (the default)
	ToastOrderOldest = "oldest" // a queue: the oldest at the bottom
)

// visibleToasts retires brief toasts that have had their time and returns
// the ones on screen from the top of the stack to the bottom, with how many
// more are waiting above them: older ones with the newest first, newer ones
// with the oldest first.
func (a *App) visibleToasts(now time.Time) (vis []*toast, waiting int) {
	live := a.toasts[:0]
	for _, t := range a.toasts {
		if !t.brief || t.shown.IsZero() || now.Sub(t.shown) < briefToastTime {
			live = append(live, t)
		}
	}
	a.toasts = live
	n := min(len(a.toasts), a.toastFit())
	if a.notifConfig.ToastOrder == ToastOrderOldest {
		for i := n - 1; i >= 0; i-- { // the oldest n, the oldest lowest
			vis = append(vis, a.toasts[i])
		}
	} else {
		vis = a.toasts[len(a.toasts)-n:]
	}
	rows := a.toastRows()
	last := a.height - 2 // the row above the status bar
	if a.view == ViewMain {
		last-- // inside the panels, above their bottom border
	}
	bottom := float64(last - rows + 1)
	for i, t := range vis {
		slot := float64(len(vis) - 1 - i) // 0 for the newest
		target := bottom - slot*float64(rows)
		switch {
		case t.shown.IsZero():
			t.shown, t.fromRow, t.toRow, t.moveAt = now, target, target, now
		case t.toRow != target:
			t.fromRow, t.toRow, t.moveAt = t.row(now), target, now
		}
	}
	return vis, len(a.toasts) - n
}

// currentToast is the toast at the bottom of the stack (nil for none).
func (a *App) currentToast(now time.Time) *toast {
	vis, _ := a.visibleToasts(now)
	if len(vis) == 0 {
		return nil
	}
	return vis[len(vis)-1]
}

// toastsMoving reports whether the stack needs redrawing on a timer: a toast
// sliding in, up or down, or a flourish counting down.
func (a *App) toastsMoving(now time.Time) bool {
	// Place the stack first: after a dismissal (Ctrl+X) the rest only get
	// their new places when the stack is laid out, and asking before that
	// saw nothing moving, so the timer stopped and the slide jumped along
	// only when something else redrew. A click hid it: the mouse's own
	// movement kept redrawing.
	a.visibleToasts(now)
	for _, t := range a.toasts {
		if t.shown.IsZero() || t.brief || now.Sub(t.moveAt) < toastSlide || now.Sub(t.shown) < toastSlide {
			return true
		}
	}
	return false
}

// paintToasts draws the stack over the bottom of the frame, above the status
// bar, inside toastArea. Only the lines it covers are touched, so the rest of
// the frame (links, pictures) stays exactly as it was.
func (a *App) paintToasts(frame string, now time.Time) string {
	a.toastRects = nil
	vis, waiting := a.visibleToasts(now)
	if len(vis) == 0 || a.width < 30 || a.height < 12 {
		return frame
	}
	area, areaW, right := a.toastArea()
	compact := a.notifConfig.ToastStyle == ToastStyleCompact
	more := ""
	if waiting > 0 {
		more = fmt.Sprintf(" +%d more ", waiting) // the top card counts the ones waiting above it
	}
	// Every card in the stack is as wide as the widest needs, within the
	// side's columns (a column spare at each edge), so their edges line up.
	frameW, capW := 4, 52 // border and padding; the widest text
	if compact {
		frameW, capW = 3, 44 // bar and padding
	}
	w := 0
	for i, t := range vis {
		w = max(w, toastTextWidth(t, compact, i == 0 && more != "", more))
	}
	w = max(8, min(w, capW, areaW-2-frameW))
	for i := 0; i < len(vis); i++ { // oldest first: the newest lands on top while they slide
		t := vis[i]
		cardMore := ""
		if i == 0 {
			cardMore = more
		}
		hint := i == len(vis)-1 && !t.brief // the bottom one says how to dismiss it
		var lines []string
		if compact {
			lines = a.compactToastLines(t, w, cardMore)
		} else {
			lines = a.fullToastLines(t, w, cardMore, hint)
		}
		cols := w + frameW
		// Slide in from the side it lives on; a flourish slides back out at the end.
		slide := 1.0
		if age := now.Sub(t.shown); age < toastSlide {
			slide = easeOutCubic(float64(age) / float64(toastSlide))
		} else if left := briefToastTime - age; t.brief && left < toastSlide {
			slide = easeOutCubic(float64(left) / float64(toastSlide))
		}
		top := int(math.Round(t.row(now)))
		col := area + 1 - int(math.Round(float64(cols+1)*(1-slide)))
		if right {
			home := area + areaW - 1 - cols
			col = home + int(math.Round(float64(a.width-home)*(1-slide)))
		}
		frame = overlayLines(frame, top, col, a.width, lines)
		a.toastRects = append(a.toastRects, toastRect{row: top, col: max(0, col), rows: len(lines), cols: cols, t: t})
	}
	return frame
}

// toastTextWidth is how wide a card's text would like to be.
func toastTextWidth(t *toast, compact, top bool, more string) int {
	extra := 0
	if top {
		extra = runewidth.StringWidth(more)
	}
	if compact {
		one, two := compactToastText(t)
		return max(runewidth.StringWidth(one)+extra, runewidth.StringWidth(two), 12)
	}
	label := t.label
	if label == "" {
		label = "🏆 Achievement unlocked"
	}
	return max(runewidth.StringWidth(label), runewidth.StringWidth(t.title), 18)
}

// compactToastText is a compact card's two lines: for a message, the
// channel, then the server and who wrote it; for anything else, its label
// and title.
func compactToastText(t *toast) (string, string) {
	if t.short1 != "" {
		return t.short1, t.short2
	}
	if t.label == "" {
		return "🏆 " + t.title, "Achievement unlocked"
	}
	return t.label, t.title
}

// toastAccent is a card's colour: yellow for a mention, else the accent.
func (a *App) toastAccent(t *toast) lipgloss.Color {
	if t.mention {
		return lipgloss.Color(a.theme.Colors.Yellow)
	}
	return lipgloss.Color(a.theme.Colors.Purple)
}

// fullToastLines is a bordered card with w columns of text: what, where,
// and the message.
func (a *App) fullToastLines(t *toast, w int, more string, hint bool) []string {
	c := a.theme.Colors
	bg := lipgloss.Color(c.Background)
	label := "🏆 Achievement unlocked"
	if t.label != "" {
		label = t.label
	}
	accent := a.toastAccent(t)
	title := runewidth.Truncate(t.title, w, "…")
	label = runewidth.Truncate(label, w, "…")
	edge := lipgloss.NewStyle().Foreground(accent).Background(bg)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Background(bg)
	body := lipgloss.NewStyle().Width(w+2).Padding(0, 1).Background(bg).Foreground(lipgloss.Color(c.Foreground))
	topBorder := edge.Render("╭" + strings.Repeat("─", w+2) + "╮")
	if more != "" && runewidth.StringWidth(more) <= w {
		topBorder = edge.Render("╭─") + edge.Bold(true).Render(more) +
			edge.Render(strings.Repeat("─", max(0, w+1-runewidth.StringWidth(more)))+"╮")
	}
	bottomBorder := edge.Render("╰" + strings.Repeat("─", w+2) + "╯")
	if ctrlX := " ctrl+x "; hint && len(ctrlX) < w {
		bottomBorder = edge.Render("╰"+strings.Repeat("─", max(0, w+1-len(ctrlX)))) + dim.Render(ctrlX) + edge.Render("─╯")
	}
	return []string{
		topBorder,
		edge.Render("│") + body.Foreground(accent).Bold(true).Render(label) + edge.Render("│"),
		edge.Render("│") + body.Render(title) + edge.Render("│"),
		bottomBorder,
	}
}

// compactToastLines is a two-line card with w columns of text, behind a bar
// of its colour (compactToastText says what the lines are).
func (a *App) compactToastLines(t *toast, w int, more string) []string {
	c := a.theme.Colors
	bg := lipgloss.Color(c.Background)
	one, two := compactToastText(t)
	accent := a.toastAccent(t)
	bar := lipgloss.NewStyle().Foreground(accent).Background(bg).Render("▌")
	line := lipgloss.NewStyle().Width(w+2).Padding(0, 1).Background(bg)
	head := line.Foreground(accent).Bold(true)
	if more != "" && runewidth.StringWidth(more) < w-4 {
		one = runewidth.Truncate(one, w-runewidth.StringWidth(more), "…")
		one = runewidth.FillRight(one, w-runewidth.StringWidth(more))
		return []string{
			bar + head.Render(one+lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Background(bg).Render(more)),
			bar + line.Foreground(lipgloss.Color(c.Foreground)).Render(runewidth.Truncate(two, w, "…")),
		}
	}
	return []string{
		bar + head.Render(runewidth.Truncate(one, w, "…")),
		bar + line.Foreground(lipgloss.Color(c.Foreground)).Render(runewidth.Truncate(two, w, "…")),
	}
}

// restackToasts lays the stack out afresh after its side or style changes:
// every card slides in again where it now lives.
func (a *App) restackToasts() {
	for _, t := range a.toasts {
		t.shown = time.Time{}
	}
}

// dismissToast removes a toast; the rest of the stack drops into place.
func (a *App) dismissToast(t *toast) {
	for i, x := range a.toasts {
		if x == t {
			a.toasts = append(a.toasts[:i], a.toasts[i+1:]...)
			return
		}
	}
}

// dismissBottomToast dismisses the bottom toast (Ctrl+X); false when there
// wasn't one.
func (a *App) dismissBottomToast() bool {
	if t := a.currentToast(time.Now()); t != nil {
		a.dismissToast(t)
		return true
	}
	return false
}

// clickToast handles a click on a toast: a message's opens its channel,
// any toast is dismissed. True when the click landed on one.
func (a *App) clickToast(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	// Last drawn is on top, so it gets the click where cards overlap mid-slide.
	for k := len(a.toastRects) - 1; k >= 0; k-- {
		r := a.toastRects[k]
		if msg.Y >= r.row && msg.Y < r.row+r.rows && msg.X >= r.col && msg.X < r.col+r.cols {
			a.dismissToast(r.t)
			if r.t.channelID != uuid.Nil {
				a.goToChannel(r.t.serverID, r.t.channelID)
			}
			return true
		}
	}
	return false
}

// goToChannel switches to a server (if it isn't the one on screen) and
// opens one of its channels.
func (a *App) goToChannel(serverID, channelID uuid.UUID) {
	if a.view != ViewMain {
		return
	}
	if a.currentClientServer == nil || a.currentClientServer.ID != serverID {
		for i, s := range a.clientServers {
			if s.ID == serverID {
				a.switchToClientServer(i)
				break
			}
		}
	}
	a.selectChannelByID(channelID)
	a.focus = FocusInput
	a.input.Focus()
}

// Toast notification modes and scopes (Settings > Notifications).
const (
	ToastModeAll      = ""         // every message (the default)
	ToastModeMentions = "mentions" // @mentions only
	ToastModeOff      = "off"
)

// shouldToast decides whether a new message gets a toast: never for the
// channel on screen, and then by the In-app Toasts settings.
func shouldToast(mode, scope string, isMention, isCurrentChannel, isCurrentServer bool) bool {
	if isCurrentChannel {
		return false
	}
	switch mode {
	case ToastModeOff:
		return false
	case ToastModeMentions:
		if !isMention {
			return false
		}
	}
	if scope == DesktopNotifyScopeCurrentServer && !isCurrentServer {
		return false
	}
	return true
}

// toastMessage queues a toast for a new message.
func (a *App) toastMessage(serverID, channelID uuid.UUID, author, serverName, channelName, content string, mention bool) {
	label := fmt.Sprintf("💬 #%s", channelName)
	if mention {
		label = fmt.Sprintf("@ Mention in #%s", channelName)
	}
	if serverName != "" && len(a.clientServers) > 1 {
		label += " · " + serverName
	}
	preview := strings.Join(strings.Fields(content), " ")
	short1 := "💬 #" + channelName
	if mention {
		short1 = "@ #" + channelName
	}
	short2 := author
	if serverName != "" {
		short2 = serverName + " · " + author
	}
	a.toasts = append(a.toasts, &toast{label: label, title: author + ": " + preview, short1: short1, short2: short2,
		serverID: serverID, channelID: channelID, mention: mention, created: time.Now()})
}
