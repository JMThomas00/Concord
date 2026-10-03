package client

import (
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/mattn/go-runewidth"
)

// After unlocking, a short connecting screen shows each server's real
// handshake (a packet running down a line of dots, then connected or
// offline), and the main window bursts open from the middle. It lasts
// until every server has settled or a few seconds pass; any key skips it.

const (
	connectingMin    = 1200 * time.Millisecond
	connectingMax    = 3 * time.Second
	connectingStep   = 250 * time.Millisecond  // between one server's row and the next
	connectingGiveUp = 2200 * time.Millisecond // a server still trying by then shows as still trying
	burstDur         = 450 * time.Millisecond
)

type connectingState struct {
	start   time.Time
	servers []*ClientServerInfo
	readyAt map[uuid.UUID]time.Duration // when each was first seen ready
}

// startConnecting shows the connecting screen, when there's anything to show.
func (a *App) startConnecting() {
	if a.surprise() == surpriseOff || a.configMgr == nil || a.width <= 0 {
		return
	}
	servers := a.configMgr.GetClientServers()
	if len(servers) == 0 {
		return
	}
	a.connecting = &connectingState{start: time.Now(), servers: servers, readyAt: map[uuid.UUID]time.Duration{}}
}

func (a *App) serverState(id uuid.UUID) ConnectionState {
	if a.connMgr == nil {
		return StateDisconnected
	}
	if c := a.connMgr.GetConnection(id); c != nil {
		return c.GetState()
	}
	return StateDisconnected
}

// connectingSettled reports whether every server has finished trying (or
// been given long enough).
func (a *App) connectingSettled(el time.Duration) bool {
	cs := a.connecting
	if el >= connectingMax {
		return true
	}
	if el < connectingMin || el < time.Duration(len(cs.servers))*connectingStep+600*time.Millisecond {
		return false
	}
	for _, s := range cs.servers {
		switch a.serverState(s.ID) {
		case StateReady, StateError, StateDisconnected:
		default:
			if el < connectingGiveUp {
				return false
			}
		}
	}
	return true
}

// endConnecting opens the main window with the burst.
func (a *App) endConnecting() {
	a.connecting = nil
	if a.surprise() == surpriseFull {
		a.fx.burstAt = time.Now()
	}
}

// skipConnecting ends the connecting screen on any key or click.
func (a *App) skipConnecting(msg tea.Msg) bool {
	if a.connecting == nil {
		return false
	}
	switch m := msg.(type) {
	case tea.KeyMsg:
	case tea.MouseMsg:
		if m.Action != tea.MouseActionPress {
			return true
		}
	default:
		return false
	}
	a.endConnecting()
	return true
}

// renderConnecting draws the connecting screen at now.
func (a *App) renderConnecting(now time.Time) string {
	cs := a.connecting
	el := now.Sub(cs.start)
	pal := a.loadingPalette()
	g := newGrid(a.width, a.height)
	if bg := a.renderAtmosphere(now); bg != nil {
		g = bg
	}
	nameW := 0
	for _, s := range cs.servers {
		nameW = max(nameW, runewidth.StringWidth(s.Name))
	}
	nameW = min(nameW, 24)
	const wire = 14
	rowW := nameW + 2 + wire + 2 + 16
	rows := min(len(cs.servers), max(1, a.height-8))
	top := (a.height - rows - 4) / 2
	c0 := max(0, (a.width-rowW)/2)
	clearBox(g, top-1, rowW+6, rows+5)
	centerText(g, top, "C O N N E C T I N G", sgrFor(pal.purple, "", true))

	dim := sgrFor(pal.dim, "", false)
	for i, s := range cs.servers[:rows] {
		r := top + 2 + i
		shown := el - time.Duration(i)*connectingStep
		if shown < 0 {
			break
		}
		state := a.serverState(s.ID)
		if _, seen := cs.readyAt[s.ID]; !seen && state == StateReady {
			cs.readyAt[s.ID] = el
		}
		g.text(r, c0, runewidth.Truncate(s.Name, nameW, "…"), sgrFor(pal.fg, "", true))
		x := c0 + nameW + 2
		// Every row's handshake runs at least once before its result shows.
		settled := shown > 500*time.Millisecond
		switch {
		case settled && state == StateReady:
			g.text(r, x, strings.Repeat("─", wire)+"→", sgrFor(pal.green, "", false))
			status := "● connected"
			if pr := a.pingResults[s.ID]; pr != nil && pr.Success {
				status += fmt.Sprintf("  %d ms", pr.Latency.Milliseconds())
			}
			g.text(r, x+wire+2, status, sgrFor(pal.green, "", true))
		case settled && (state == StateError || state == StateDisconnected) && shown > 900*time.Millisecond:
			g.text(r, x, strings.Repeat("·", wire)+"✕", sgrFor(pal.red, "", false))
			g.text(r, x+wire+2, "offline", sgrFor(pal.red, "", true))
		default:
			// A packet running down the wire.
			pos := int(math.Mod(shown.Seconds()*wire*2.2, wire))
			g.text(r, x, strings.Repeat("·", wire)+"→", dim)
			g.set(r, x+pos, "●", sgrFor(pal.pink, "", true), 1)
			label := "handshake"
			if state == StateReconnecting || state == StateConnecting {
				label = "dialling"
			}
			g.text(r, x+wire+2, label+strings.Repeat(".", int(shown.Seconds()*4)%4), dim)
		}
	}
	if len(cs.servers) > rows {
		g.text(top+2+rows, c0, fmt.Sprintf("and %d more", len(cs.servers)-rows), dim)
	}
	centerText(g, top+rows+3, "any key to skip", dim)
	return g.String()
}

// burst opens the main window from the middle outwards: the left half
// slides in from the left edge, the right half from the right.
func burst(main *fxGrid, p float64) *fxGrid {
	out := newGrid(main.w, main.h)
	off := int(math.Round((1 - easeOutCubic(p)) * float64(main.w) / 2))
	half := main.w / 2
	for r := 0; r < main.h; r++ {
		for c := 0; c < main.w; c++ {
			cell := main.rows[r][c]
			if cell.w == 0 {
				continue
			}
			if c < half {
				putCell(out, r, c-off, cell)
			} else {
				putCell(out, r, c+off, cell)
			}
		}
	}
	return out
}
