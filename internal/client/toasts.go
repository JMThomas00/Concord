package client

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
	"github.com/mattn/go-runewidth"
)

// Toasts: small cards in the bottom-left corner, stacked with the newest
// at the bottom. Achievements and the login stage's little messages use
// them, and so do new messages (Settings > Notifications > In-app Toasts):
// clicking a message toast opens its channel.

const (
	toastTime        = 4 * time.Second // an achievement or a notice
	messageToastTime = 6 * time.Second // a message: a little longer to read
	maxToasts        = 4               // on screen at once; the oldest give way
	toastSlide       = 250 * time.Millisecond
)

type toast struct {
	label string // the first line; "" for an achievement
	title string

	// A message's toast knows where the message is, so a click goes there.
	serverID, channelID uuid.UUID
	mention             bool

	created time.Time
	shown   time.Time // when it first appeared on screen
}

func (t *toast) life() time.Duration {
	if t.channelID != uuid.Nil {
		return messageToastTime
	}
	return toastTime
}

// toastRect is where a toast was drawn, for clicks.
type toastRect struct {
	row, col, rows, cols int
	t                    *toast
}

// visibleToasts retires finished toasts and returns those on screen now,
// oldest first (so the newest is drawn at the bottom).
func (a *App) visibleToasts(now time.Time) []*toast {
	live := a.toasts[:0]
	for _, t := range a.toasts {
		if t.shown.IsZero() || now.Sub(t.shown) < t.life() {
			live = append(live, t)
		}
	}
	a.toasts = live
	// When more are waiting than fit, the oldest on screen leave early
	// (once they've had a moment to be read).
	for len(a.toasts) > maxToasts && !a.toasts[0].shown.IsZero() && now.Sub(a.toasts[0].shown) >= 1500*time.Millisecond {
		a.toasts = a.toasts[1:]
	}
	n := min(len(a.toasts), maxToasts)
	vis := a.toasts[:n]
	for _, t := range vis {
		if t.shown.IsZero() {
			t.shown = now
		}
	}
	return vis
}

// currentToast is the newest toast on screen (nil for none).
func (a *App) currentToast(now time.Time) *toast {
	vis := a.visibleToasts(now)
	if len(vis) == 0 {
		return nil
	}
	return vis[len(vis)-1]
}

// paintToasts draws the stack over the bottom left of the frame, above the
// status bar. Only the lines it covers are touched, so the rest of the
// frame (links, pictures) stays exactly as it was.
func (a *App) paintToasts(frame string, now time.Time) string {
	a.toastRects = nil
	vis := a.visibleToasts(now)
	if len(vis) == 0 || a.width < 30 || a.height < 12 {
		return frame
	}
	c := a.theme.Colors
	accent := c.Purple
	bottom := a.height - 2 // the row above the status bar
	for i := len(vis) - 1; i >= 0; i-- {
		t := vis[i]
		label := "🏆 Achievement unlocked"
		if t.label != "" {
			label = t.label
		}
		labelColour := accent
		if t.mention {
			labelColour = c.Yellow
		}
		w := min(max(runewidth.StringWidth(label), runewidth.StringWidth(t.title))+4, min(52, a.width-4))
		title := runewidth.Truncate(t.title, w-4, "…")
		label = runewidth.Truncate(label, w-4, "…")
		// Slide in from the left, and back out at the end.
		age := now.Sub(t.shown)
		slide := 1.0
		if age < toastSlide {
			slide = easeOutCubic(float64(age) / float64(toastSlide))
		} else if left := t.life() - age; left < toastSlide {
			slide = easeOutCubic(float64(left) / float64(toastSlide))
		}
		box := lipgloss.NewStyle().Width(w).Padding(0, 2).
			Background(lipgloss.Color(c.Background)).
			Foreground(lipgloss.Color(c.Foreground))
		edge := lipgloss.NewStyle().Foreground(lipgloss.Color(labelColour)).Background(lipgloss.Color(c.Background))
		lines := []string{
			edge.Render("▎") + box.Render(""),
			edge.Render("▎") + box.Foreground(lipgloss.Color(labelColour)).Bold(true).Render(label),
			edge.Render("▎") + box.Render(title),
			edge.Render("▎") + box.Render(""),
		}
		top := bottom - len(lines) + 1
		col := 1 - int(float64(w+2)*(1-slide))
		frame = overlayLines(frame, top, col, a.width, lines)
		a.toastRects = append(a.toastRects, toastRect{row: top, col: max(0, col), rows: len(lines), cols: w + 1, t: t})
		bottom = top - 2 // a row between toasts
		if bottom < 4 {
			break
		}
	}
	return frame
}

// clickToast handles a click on a toast: a message's opens its channel,
// any toast is dismissed. True when the click landed on one.
func (a *App) clickToast(msg tea.MouseMsg) bool {
	if msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return false
	}
	for _, r := range a.toastRects {
		if msg.Y >= r.row && msg.Y < r.row+r.rows && msg.X >= r.col && msg.X < r.col+r.cols {
			for i, t := range a.toasts {
				if t == r.t {
					a.toasts = append(a.toasts[:i], a.toasts[i+1:]...)
					break
				}
			}
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
