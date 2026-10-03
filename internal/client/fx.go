package client

import (
	"fmt"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	zone "github.com/lrstanley/bubblezone"
)

// The login experience's moving parts (Concord - Login Experience Plan in
// the vault): page transitions, backgrounds, loading screens and toasts.
// They share one ticker, started and stopped from the Update wrapper like
// the grape light, and are drawn over finished frames (fx_canvas.go), so
// no page's own code knows about them.

// fxFrameDur caps every effect at about 30 frames a second.
const fxFrameDur = 33 * time.Millisecond

type fxTickMsg struct{ gen int }

func fxTick(gen int) tea.Cmd {
	return tea.Tick(fxFrameDur, func(time.Time) tea.Msg { return fxTickMsg{gen: gen} })
}

type fxState struct {
	gen      int
	ticking  bool
	prevView View
	prevKey  string    // stageKey at the last Update, so steps within a page transition too
	stageAt  time.Time // when the login stage was last arrived at
	last     string    // the last login-stage frame drawn, for transitions
	burstAt  time.Time // when the main window started arriving (landing)
	shakeAt  time.Time // when the form started shaking (stage.go)
	lastErr  string    // the problem shown at the last Update
	trans    *transition
}

// isStageView reports the views that make up the login stage.
func isStageView(v View) bool {
	switch v {
	case ViewToS, ViewIdentitySetup, ViewLogin, ViewRegister, ViewAddServer, ViewProfiles, ViewAccountCode:
		return true
	}
	return false
}

// syncFx starts a transition when the login stage changes page, and starts
// the ticker when anything is moving. Called after every Update.
func (a *App) syncFx() tea.Cmd {
	fx := &a.fx
	a.noticeReactions(time.Now())
	if key := a.stageKey(); a.view != fx.prevView || key != fx.prevKey {
		if isStageView(a.view) && !isStageView(fx.prevView) {
			fx.stageAt = time.Now()
		}
		if isStageView(fx.prevView) && isStageView(a.view) && fx.last != "" {
			if kind := a.pick(layerTransition); kind != "" {
				fx.trans = &transition{kind: kind, from: fx.last, start: time.Now(), seed: rng.Uint64()}
				a.discover(layerTransition, kind)
			}
		}
		fx.prevView = a.view
		fx.prevKey = key
	}
	if !a.fxMoving() || fx.ticking {
		return nil
	}
	fx.ticking = true
	fx.gen++
	return fxTick(fx.gen)
}

// fxMoving reports whether anything needs redrawing on a timer.
func (a *App) fxMoving() bool {
	now := time.Now()
	if t := a.fx.trans; t != nil && now.Sub(t.start) >= t.duration() {
		a.fx.trans = nil
	}
	if a.connecting != nil && a.view != ViewMain {
		a.connecting = nil // something else came first (a code screen)
	}
	a.trackCodeTyping(now)
	if a.loading != nil || a.connecting != nil || a.fx.trans != nil || len(a.toasts) > 0 || a.bursting(now) ||
		a.codeAnimating(now) || a.shaking(now) || a.grapeReact != nil || a.eggPlaying(now) || a.saver != nil {
		return true
	}
	return isStageView(a.view) && (a.atmosphereMoving() || a.calendar() != "" || a.dozing())
}

func (a *App) handleFxTick(msg fxTickMsg) tea.Cmd {
	if msg.gen != a.fx.gen {
		return nil
	}
	var done tea.Cmd
	if l := a.loading; l != nil && time.Since(l.start) >= l.dur {
		done = a.endLoading()
	}
	if c := a.connecting; c != nil && a.connectingSettled(time.Since(c.start)) {
		a.endConnecting()
	}
	if !a.fxMoving() {
		a.fx.ticking = false
		return done
	}
	return tea.Batch(done, fxTick(a.fx.gen))
}

// applyFx draws this frame's effects over the rendered frame.
func (a *App) applyFx(out string) string {
	if a.width <= 0 || a.height <= 0 {
		return out
	}
	now := time.Now()
	if isStageView(a.view) {
		bg := a.renderAtmosphere(now)
		if cal := a.calendarAtmosphere(now, now.Sub(a.fx.stageAt).Seconds()); cal != nil {
			bg = cal
		}
		t := a.fx.trans
		accents := a.pick(layerAccent)
		if bg != nil || t != nil || a.shaking(now) || (accents != "" && accents != "none") ||
			a.eggPlaying(now) || a.calendar() != "" || a.dozing() {
			g := parseFrame(out, a.width, a.height)
			if bg != nil {
				g.underlay(bg, 3, 1)
			}
			a.drawAccents(g)
			a.paintCalendarText(g, now)
			a.paintDoze(g, now)
			if a.shaking(now) {
				if z := zone.Get("stage-form"); z != nil && !z.IsZero() {
					shake(g, z.StartY, z.EndY, z.StartX-2, float64(now.Sub(a.fx.shakeAt))/float64(shakeDur))
				}
			}
			if t != nil {
				p := float64(now.Sub(t.start)) / float64(t.duration())
				if p < 1 {
					from := parseFrame(t.from, a.width, a.height)
					g = t.render(from, g, p)
				}
			}
			if a.eggPlaying(now) {
				a.paintEgg(g, now)
			}
			out = g.String()
		}
		a.fx.last = out
	}
	if !isStageView(a.view) && a.eggPlaying(now) {
		g := parseFrame(out, a.width, a.height)
		a.paintEgg(g, now)
		out = g.String()
	}
	if a.view == ViewMain && a.bursting(now) {
		p := float64(now.Sub(a.fx.burstAt)) / float64(burstDur)
		out = landing(parseFrame(out, a.width, a.height), p, a.loadingPalette()).String()
	}
	if t := a.currentToast(now); t != nil {
		out = a.paintToast(out, t, now)
	}
	return out
}

// sgrFor is the escape sequence that starts text in these colours, as the
// terminal's colour profile allows (nothing at all under NO_COLOR).
func sgrFor(fg, bg string, bold bool) string {
	key := fg + "|" + bg
	if bold {
		key += "|b"
	}
	sgrMu.Lock()
	defer sgrMu.Unlock()
	if s, ok := sgrCache[key]; ok {
		return s
	}
	st := lipgloss.NewStyle().Bold(bold)
	if fg != "" {
		st = st.Foreground(lipgloss.Color(fg))
	}
	if bg != "" {
		st = st.Background(lipgloss.Color(bg))
	}
	r := st.Render("x")
	s := r[:strings.IndexByte(r, 'x')]
	sgrCache[key] = s
	return s
}

var (
	sgrMu    sync.Mutex
	sgrCache = map[string]string{}
)

// bursting reports whether the main window is still arriving (landing).
func (a *App) bursting(now time.Time) bool {
	return !a.fx.burstAt.IsZero() && now.Sub(a.fx.burstAt) < burstDur
}

// stageKey names the page showing on the login stage, steps included, so
// moving between them plays a transition.
func (a *App) stageKey() string {
	k := fmt.Sprint(int(a.view))
	switch a.view {
	case ViewIdentitySetup:
		k += fmt.Sprint(":", a.identityFocus)
	case ViewAccountCode:
		if st := a.codeState; st != nil {
			k += fmt.Sprint(":", st.Mode, st.Step, st.Fixing, st.ServerID)
		}
	case ViewProfiles:
		if s := a.profilesState; s != nil {
			k += fmt.Sprint(":", s.EditID != "", s.Password != nil)
		}
	}
	return k
}
