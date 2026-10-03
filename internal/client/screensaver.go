package client

import (
	"math"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Screensavers: after a while with no input on the login stage, one of
// these takes over the screen until a key or click (which is otherwise
// ignored). The bouncing logo celebrates hitting a corner exactly.

const (
	saverAfter     = 90 * time.Second
	idleCheckEvery = 5 * time.Second
)

var saverKinds = []string{"bounce", "flyers", "stars", "pipes", "lava"}

type idleCheckMsg struct{}

func idleCheck() tea.Cmd {
	return tea.Tick(idleCheckEvery, func(time.Time) tea.Msg { return idleCheckMsg{} })
}

type saverState struct {
	kind  string
	start time.Time
	seed  uint64
	last  time.Time // the last simulation step

	// bounce
	x, y, dx, dy float64
	colour       int

	// pipes
	canvas *fxGrid
	heads  []pipeHead
	placed int
}

type pipeHead struct {
	r, c, dir int // dir: 0 right, 1 down, 2 left, 3 up
	colour    string
}

// handleIdleCheck starts a screensaver once the stage has sat idle.
func (a *App) handleIdleCheck() tea.Cmd {
	if a.saver == nil && a.loading == nil && a.connecting == nil && isStageView(a.view) &&
		a.surprise() == surpriseFull && time.Since(a.lastInput) >= saverAfter && a.width > 30 && a.height > 10 {
		kind := saverKinds[rng.Intn(len(saverKinds))]
		a.saver = &saverState{kind: kind, start: time.Now(), last: time.Now(), seed: rng.Uint64(),
			x: 3, y: 2, dx: 14, dy: 5}
		a.findEgg("screensaver")
	}
	return idleCheck()
}

// noteInput records activity, and ends a screensaver; true when the input
// was used up waking the screen.
func (a *App) noteInput(msg tea.Msg) bool {
	switch m := msg.(type) {
	case tea.KeyMsg:
	case tea.MouseMsg:
		if m.Action == tea.MouseActionMotion {
			return a.saver != nil
		}
	default:
		return false
	}
	a.lastInput = time.Now()
	if a.saver != nil {
		a.saver = nil
		return true
	}
	return false
}

// renderSaver draws the screensaver at now.
func (a *App) renderSaver(now time.Time) string {
	s := a.saver
	pal := a.loadingPalette()
	g := newGrid(a.width, a.height)
	el := now.Sub(s.start).Seconds()
	dt := math.Min(.1, now.Sub(s.last).Seconds())
	s.last = now
	switch s.kind {
	case "bounce":
		a.saverBounce(g, dt, pal)
	case "flyers":
		saverFlyers(g, el, s.seed, pal)
	case "stars":
		g = atmosStars(a.width, a.height, el*4, uint32(s.seed), loadingPalette{
			purple: pal.purple, pink: pal.pink, fg: pal.fg, dim: pal.dim, dark: pal.dark})
	case "pipes":
		saverPipes(s, g, dt, pal)
	case "lava":
		lavaLamp(g, el, s.seed, pal)
	}
	return g.String()
}

// saverBounce: the CONCORD block letters drifting and bouncing, changing
// colour off each wall. An exact corner is celebrated.
func (a *App) saverBounce(g *fxGrid, dt float64, pal loadingPalette) {
	s := a.saver
	w := len([]rune(concordBlock[0]))
	maxX, maxY := float64(g.w-w), float64(g.h-2)
	s.x += s.dx * dt
	s.y += s.dy * dt
	hitX, hitY := false, false
	if s.x <= 0 || s.x >= maxX {
		s.x, s.dx, hitX = math.Max(0, math.Min(maxX, s.x)), -s.dx, true
	}
	if s.y <= 0 || s.y >= maxY {
		s.y, s.dy, hitY = math.Max(0, math.Min(maxY, s.y)), -s.dy, true
	}
	if hitX || hitY {
		s.colour++
	}
	if hitX && hitY {
		a.startEgg("corner", 3*time.Second) // the moment everyone waits for
		a.egg.kind = "confetti"
		a.findEgg("corner")
	}
	colours := []string{pal.purple, pal.pink, pal.cyan, pal.green, pal.yellow, pal.red}
	col := colours[s.colour%len(colours)]
	for i, line := range concordBlock {
		g.text(int(s.y)+i, int(s.x), line, sgrFor(col, "", true))
	}
	if a.egg != nil {
		a.paintEgg(g, time.Now())
	}
}

// saverFlyers: grapes flying out of the screen at you, through the stars.
func saverFlyers(g *fxGrid, el float64, seed uint64, pal loadingPalette) {
	cx, cy := float64(g.w)/2, float64(g.h)/2
	for i := 0; i < 70; i++ {
		ang := cellHash(seed, i, 5, 1) * 2 * math.Pi
		d := math.Mod(el*(.15+cellHash(seed, i, 5, 2)*.2)+cellHash(seed, i, 5, 3), 1)
		d = d * d * d
		x := cx + math.Cos(ang)*d*cx*1.3
		y := cy + math.Sin(ang)*d*cy*1.3
		switch {
		case i%5 == 0 && d > .08:
			g.set(int(y), int(x), "🍇", "", 2)
		case d > .5:
			g.set(int(y), int(x), "•", sgrFor(pal.fg, "", false), 1)
		default:
			g.set(int(y), int(x), "·", sgrFor(faint(pal.fg, pal, .2+d), "", false), 1)
		}
	}
}

var pipeTurns = map[[2]int]string{
	{0, 0}: "━", {2, 2}: "━", {1, 1}: "┃", {3, 3}: "┃",
	{0, 1}: "┓", {3, 2}: "┓", {0, 3}: "┛", {1, 2}: "┛",
	{2, 1}: "┏", {3, 0}: "┏", {2, 3}: "┗", {1, 0}: "┗",
}

// saverPipes: the 1990s pipes, growing and turning until the screen fills,
// then starting over.
func saverPipes(s *saverState, g *fxGrid, dt float64, pal loadingPalette) {
	if s.canvas == nil || s.canvas.w != g.w || s.canvas.h != g.h || s.placed > g.w*g.h*3/5 {
		s.canvas, s.heads, s.placed = newGrid(g.w, g.h), nil, 0
	}
	colours := []string{pal.purple, pal.pink, pal.cyan, pal.green, pal.yellow}
	for len(s.heads) < 3 {
		k := len(s.heads) + s.placed
		s.heads = append(s.heads, pipeHead{
			r: int(cellHash(s.seed, k, 6, 1) * float64(g.h)), c: int(cellHash(s.seed, k, 6, 2) * float64(g.w)),
			dir: k % 4, colour: colours[k%len(colours)]})
	}
	steps := max(1, int(dt*60))
	for st := 0; st < steps; st++ {
		for i := range s.heads {
			h := &s.heads[i]
			next := h.dir
			if cellHash(s.seed, s.placed, i, 7) < .18 {
				next = (h.dir + 1 + 2*int(cellHash(s.seed, s.placed, i, 8)*2)) % 4 // turn left or right
			}
			s.canvas.set(h.r, h.c, pipeTurns[[2]int{h.dir, next}], sgrFor(h.colour, "", true), 1)
			s.placed++
			h.dir = next
			h.c += []int{1, 0, -1, 0}[h.dir]
			h.r += []int{0, 1, 0, -1}[h.dir]
			if h.r < 0 || h.r >= g.h || h.c < 0 || h.c >= g.w {
				s.heads = append(s.heads[:i], s.heads[i+1:]...)
				break
			}
		}
	}
	for r := range g.rows {
		copy(g.rows[r], s.canvas.rows[r])
	}
}
