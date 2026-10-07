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

// Toasts: cards in the bottom-left corner, in a border of the theme's
// accent, stacked with the newest at the bottom: a new one slides in and the
// rest slide up above it, as far as a third of the way down the window; past
// that, the top card counts the ones waiting above. Notifications (messages,
// mentions, achievements, firsts) stay until they're dealt with: a click
// dismisses one (a message's opens its channel), and Ctrl+X dismisses the
// bottom one, when the next drops down into its place. The login stage's
// little flourishes (brief) come and go by themselves.

const (
	briefToastTime = 4 * time.Second // a flourish's time on screen
	toastSlide     = 250 * time.Millisecond
	toastRows      = 4 // border, label, text, border
	toastGap       = 1
)

type toast struct {
	label string // the first line; "" for an achievement
	title string

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

// toastFit is how many toasts fit in the stack: from just above the status
// bar up to a third of the way down the window.
func (a *App) toastFit() int {
	bottom := a.height - 2
	return max(1, (bottom-a.height/3+1+toastGap)/(toastRows+toastGap))
}

// visibleToasts retires brief toasts that have had their time and returns
// the ones on screen, oldest first (the newest is drawn at the bottom), with
// how many older ones are waiting above them.
func (a *App) visibleToasts(now time.Time) (vis []*toast, waiting int) {
	live := a.toasts[:0]
	for _, t := range a.toasts {
		if !t.brief || t.shown.IsZero() || now.Sub(t.shown) < briefToastTime {
			live = append(live, t)
		}
	}
	a.toasts = live
	n := min(len(a.toasts), a.toastFit())
	vis = a.toasts[len(a.toasts)-n:]
	bottom := float64(a.height - 2 - toastRows + 1)
	for i, t := range vis {
		slot := float64(len(vis) - 1 - i) // 0 for the newest
		target := bottom - slot*float64(toastRows+toastGap)
		switch {
		case t.shown.IsZero():
			t.shown, t.fromRow, t.toRow, t.moveAt = now, target, target, now
		case t.toRow != target:
			t.fromRow, t.toRow, t.moveAt = t.row(now), target, now
		}
	}
	return vis, len(a.toasts) - n
}

// currentToast is the newest toast on screen (nil for none).
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
	for _, t := range a.toasts {
		if t.shown.IsZero() || t.brief || now.Sub(t.moveAt) < toastSlide || now.Sub(t.shown) < toastSlide {
			return true
		}
	}
	return false
}

// paintToasts draws the stack over the bottom left of the frame, above the
// status bar. Only the lines it covers are touched, so the rest of the
// frame (links, pictures) stays exactly as it was.
func (a *App) paintToasts(frame string, now time.Time) string {
	a.toastRects = nil
	vis, waiting := a.visibleToasts(now)
	if len(vis) == 0 || a.width < 30 || a.height < 12 {
		return frame
	}
	c := a.theme.Colors
	bg := lipgloss.Color(c.Background)
	for i := 0; i < len(vis); i++ { // oldest first: the newest lands on top while they slide
		t := vis[i]
		label := "🏆 Achievement unlocked"
		if t.label != "" {
			label = t.label
		}
		accent := c.Purple
		if t.mention {
			accent = c.Yellow
		}
		w := min(max(runewidth.StringWidth(label), runewidth.StringWidth(t.title), 18), min(52, a.width-6))
		title := runewidth.Truncate(t.title, w, "…")
		label = runewidth.Truncate(label, w, "…")
		edge := lipgloss.NewStyle().Foreground(lipgloss.Color(accent)).Background(bg)
		dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Background(bg)
		body := lipgloss.NewStyle().Width(w+2).Padding(0, 1).Background(bg).Foreground(lipgloss.Color(c.Foreground))
		// The top card counts the ones waiting above it; the bottom one
		// says how to dismiss it.
		topBorder := edge.Render("╭" + strings.Repeat("─", w+2) + "╮")
		if i == 0 && waiting > 0 {
			more := fmt.Sprintf(" +%d more ", waiting)
			topBorder = edge.Render("╭─") + edge.Bold(true).Render(more) +
				edge.Render(strings.Repeat("─", max(0, w+1-runewidth.StringWidth(more)))+"╮")
		}
		bottomBorder := edge.Render("╰" + strings.Repeat("─", w+2) + "╯")
		if i == len(vis)-1 && !t.brief {
			const hint = " ctrl+x "
			bottomBorder = edge.Render("╰"+strings.Repeat("─", max(0, w+1-len(hint)))) + dim.Render(hint) + edge.Render("─╯")
		}
		lines := []string{
			topBorder,
			edge.Render("│") + body.Foreground(lipgloss.Color(accent)).Bold(true).Render(label) + edge.Render("│"),
			edge.Render("│") + body.Render(title) + edge.Render("│"),
			bottomBorder,
		}
		// Slide in from the left; a flourish slides back out at the end.
		slide := 1.0
		if age := now.Sub(t.shown); age < toastSlide {
			slide = easeOutCubic(float64(age) / float64(toastSlide))
		} else if left := briefToastTime - age; t.brief && left < toastSlide {
			slide = easeOutCubic(float64(left) / float64(toastSlide))
		}
		top := int(math.Round(t.row(now)))
		col := 1 - int(float64(w+4)*(1-slide))
		frame = overlayLines(frame, top, col, a.width, lines)
		a.toastRects = append(a.toastRects, toastRect{row: top, col: max(0, col), rows: len(lines), cols: w + 4, t: t})
	}
	return frame
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

// dismissBottomToast dismisses the newest toast (Ctrl+X); false when there
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
	a.toasts = append(a.toasts, &toast{label: label, title: author + ": " + preview,
		serverID: serverID, channelID: channelID, mention: mention, created: time.Now()})
}
