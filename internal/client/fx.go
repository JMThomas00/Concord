package client

import (
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
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
	started  bool      // the first frame has been drawn
	stageAt  time.Time // when the login stage was last arrived at
	last     string    // the last login-stage frame drawn, for transitions
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
	if a.view != fx.prevView {
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
	if a.fx.trans != nil || len(a.toasts) > 0 {
		return true
	}
	return isStageView(a.view) && a.atmosphereMoving()
}

func (a *App) handleFxTick(msg fxTickMsg) tea.Cmd {
	if msg.gen != a.fx.gen {
		return nil
	}
	if !a.fxMoving() {
		a.fx.ticking = false
		return nil
	}
	return fxTick(a.fx.gen)
}

// applyFx draws this frame's effects over the rendered frame.
func (a *App) applyFx(out string) string {
	if a.width <= 0 || a.height <= 0 {
		return out
	}
	now := time.Now()
	if isStageView(a.view) {
		bg := a.renderAtmosphere(now)
		t := a.fx.trans
		if bg != nil || t != nil {
			g := parseFrame(out, a.width, a.height)
			if bg != nil {
				g.underlay(bg, 3, 1)
			}
			if t != nil {
				p := float64(now.Sub(t.start)) / float64(t.duration())
				if p < 1 {
					from := parseFrame(t.from, a.width, a.height)
					g = t.render(from, g, p)
				}
			}
			out = g.String()
		}
		a.fx.last = out
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
