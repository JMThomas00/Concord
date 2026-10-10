package client

import (
	"fmt"
	"math"
	"time"
)

// More screensavers: Conway's Game of Life played with grapes, a maze that
// carves itself and is then walked by a grape, wine bottles flying past
// (after the old flying toasters), and Falling Bunches, a falling-blocks
// game that plays itself.

// --- life --------------------------------------------------------------------

type lifeState struct {
	w, h    int
	cells   []bool
	gen     int
	last    time.Time
	history []int // recent populations, to spot a board that's settled
}

func newLife(w, h int, seed uint64) *lifeState {
	l := &lifeState{w: w, h: h, cells: make([]bool, w*h)}
	for i := range l.cells {
		l.cells[i] = cellHash(seed, i, 0, 31) < .28
	}
	return l
}

func (l *lifeState) step() {
	next := make([]bool, len(l.cells))
	pop := 0
	for y := 0; y < l.h; y++ {
		for x := 0; x < l.w; x++ {
			n := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if (dx != 0 || dy != 0) && l.cells[((y+dy+l.h)%l.h)*l.w+(x+dx+l.w)%l.w] {
						n++
					}
				}
			}
			alive := l.cells[y*l.w+x]
			next[y*l.w+x] = n == 3 || (alive && n == 2)
			if next[y*l.w+x] {
				pop++
			}
		}
	}
	l.cells = next
	l.gen++
	l.history = append(l.history, pop)
	if len(l.history) > 12 {
		l.history = l.history[1:]
	}
}

// settled reports a board that's died out or stopped changing much.
func (l *lifeState) settled() bool {
	if len(l.history) < 12 || l.gen > 600 {
		return l.gen > 600
	}
	lo, hi := l.history[0], l.history[0]
	for _, p := range l.history {
		lo, hi = min(lo, p), max(hi, p)
	}
	return hi-lo <= 2
}

func saverLife(s *saverState, g *fxGrid, now time.Time, pal loadingPalette) {
	w, h := g.w/2, g.h-1
	if s.life == nil || s.life.w != w || s.life.h != h || s.life.settled() {
		s.life = newLife(w, h, s.seed+uint64(now.UnixNano()))
		s.life.last = now
	}
	for now.Sub(s.life.last) >= 140*time.Millisecond {
		s.life.step()
		s.life.last = s.life.last.Add(140 * time.Millisecond)
	}
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if s.life.cells[y*w+x] {
				g.set(y, x*2, "🍇", "", 2)
			}
		}
	}
	g.text(g.h-1, 2, fmt.Sprintf("generation %d", s.life.gen), sgrFor(faint(pal.fg, pal, .4), "", false))
}

// --- maze --------------------------------------------------------------------

type mazeState struct {
	w, h     int    // in units (each two columns wide); odd sizes
	open     []bool // carved
	stack    []int
	path     []int // the solution, once carved
	walked   int
	done     time.Time
	last     time.Time
	solved   bool
	visitSet []bool
}

func newMaze(w, h int) *mazeState {
	if w%2 == 0 {
		w--
	}
	if h%2 == 0 {
		h--
	}
	m := &mazeState{w: w, h: h, open: make([]bool, w*h), visitSet: make([]bool, w*h)}
	start := 1*w + 1
	m.open[start], m.visitSet[start] = true, true
	m.stack = []int{start}
	return m
}

// carve takes one step of the recursive-backtracker maze.
func (m *mazeState) carve(seed uint64) {
	if len(m.stack) == 0 {
		return
	}
	cur := m.stack[len(m.stack)-1]
	x, y := cur%m.w, cur/m.w
	var next []int
	for _, d := range [][2]int{{2, 0}, {-2, 0}, {0, 2}, {0, -2}} {
		nx, ny := x+d[0], y+d[1]
		if nx > 0 && nx < m.w-1 && ny > 0 && ny < m.h-1 && !m.visitSet[ny*m.w+nx] {
			next = append(next, ny*m.w+nx)
		}
	}
	if len(next) == 0 {
		m.stack = m.stack[:len(m.stack)-1]
		return
	}
	n := next[int(cellHash(seed, cur, len(m.stack), 32)*float64(len(next)))]
	m.open[n], m.visitSet[n] = true, true
	m.open[(cur+n)/2] = true // the wall between
	m.stack = append(m.stack, n)
}

// solve finds the way from the top left to the bottom right.
func (m *mazeState) solve() {
	start, end := 1*m.w+1, (m.h-2)*m.w+(m.w-2)
	prev := map[int]int{start: -1}
	queue := []int{start}
	for len(queue) > 0 {
		c := queue[0]
		queue = queue[1:]
		if c == end {
			break
		}
		for _, d := range []int{1, -1, m.w, -m.w} {
			n := c + d
			if n >= 0 && n < len(m.open) && m.open[n] {
				if _, seen := prev[n]; !seen {
					prev[n] = c
					queue = append(queue, n)
				}
			}
		}
	}
	for c := end; c != -1; c = prev[c] {
		m.path = append([]int{c}, m.path...)
		if _, ok := prev[c]; !ok {
			break
		}
	}
	m.solved = true
}

func saverMaze(s *saverState, g *fxGrid, now time.Time, pal loadingPalette) {
	w, h := g.w/2, g.h
	if s.maze == nil || s.maze.w > w || s.maze.h > h || (!s.maze.done.IsZero() && now.Sub(s.maze.done) > 2500*time.Millisecond) {
		s.maze = newMaze(w, h)
		s.maze.last = now
	}
	m := s.maze
	steps := int(now.Sub(m.last) / (4 * time.Millisecond))
	if steps > 0 {
		m.last = now
	}
	for i := 0; i < steps; i++ {
		switch {
		case len(m.stack) > 0:
			m.carve(s.seed)
		case !m.solved:
			m.solve()
		case m.walked < len(m.path):
			if i%6 == 0 { // the grape walks slower than the maze carves
				m.walked++
			}
		case m.done.IsZero():
			m.done = now
		}
	}
	wall := sgrFor(faint(pal.purple, pal, .55), "", false)
	trail := sgrFor(pal.pink, "", false)
	onPath := map[int]bool{}
	for _, c := range m.path[:min(m.walked, len(m.path))] {
		onPath[c] = true
	}
	for y := 0; y < m.h; y++ {
		for x := 0; x < m.w; x++ {
			i := y*m.w + x
			switch {
			case !m.open[i]:
				g.text(y, x*2, "██", wall)
			case onPath[i]:
				g.text(y, x*2, "··", trail)
			}
		}
	}
	if m.walked > 0 && m.walked <= len(m.path) {
		c := m.path[m.walked-1]
		g.set(c/m.w, (c%m.w)*2, "🍇", "", 2)
	}
}

// --- flying bottles ----------------------------------------------------------

func saverBottles(g *fxGrid, el float64, seed uint64) {
	n := g.w * g.h / 160
	for i := 0; i < n; i++ {
		speed := 6 + cellHash(seed, i, 33, 1)*10 // near ones fly faster
		span := float64(g.w + g.h*2)
		d := math.Mod(el*speed+cellHash(seed, i, 33, 2)*span, span)
		x := float64(g.w) - d + cellHash(seed, i, 33, 3)*float64(g.h)
		y := d*.5 - cellHash(seed, i, 33, 3)*float64(g.h)*.5
		ch := "🍷"
		if i%3 == 0 {
			ch = "🍇"
		}
		g.set(int(y), int(x), ch, "", 2)
	}
}

// --- falling bunches -----------------------------------------------------------

var bunchPieces = [][][2]int{
	{{0, 0}, {1, 0}, {2, 0}, {3, 0}}, // I
	{{0, 0}, {1, 0}, {0, 1}, {1, 1}}, // O
	{{0, 0}, {1, 0}, {2, 0}, {1, 1}}, // T
	{{1, 0}, {2, 0}, {0, 1}, {1, 1}}, // S
	{{0, 0}, {1, 0}, {1, 1}, {2, 1}}, // Z
	{{0, 0}, {0, 1}, {1, 1}, {2, 1}}, // J
	{{2, 0}, {0, 1}, {1, 1}, {2, 1}}, // L
}

const bunchW, bunchH = 10, 20

type bunchState struct {
	board        [bunchH][bunchW]int // 0 empty, else piece+1
	piece, rot   int
	x, y         int
	targetX      int
	targetRot    int
	lines, count int
	last         time.Time
}

func rotated(p [][2]int, rot int) [][2]int {
	out := make([][2]int, len(p))
	copy(out, p)
	for r := 0; r < rot%4; r++ {
		for i, c := range out {
			out[i] = [2]int{-c[1], c[0]}
		}
	}
	minX, minY := 99, 99
	for _, c := range out {
		minX, minY = min(minX, c[0]), min(minY, c[1])
	}
	for i := range out {
		out[i][0] -= minX
		out[i][1] -= minY
	}
	return out
}

func (b *bunchState) fits(piece, rot, x, y int) bool {
	for _, c := range rotated(bunchPieces[piece], rot) {
		cx, cy := x+c[0], y+c[1]
		if cx < 0 || cx >= bunchW || cy >= bunchH || (cy >= 0 && b.board[cy][cx] != 0) {
			return false
		}
	}
	return true
}

// plan picks where the piece should go: the placement leaving the lowest,
// flattest stack with the fewest holes.
func (b *bunchState) plan() {
	best := math.Inf(-1)
	for rot := 0; rot < 4; rot++ {
		for x := -2; x < bunchW; x++ {
			if !b.fits(b.piece, rot, x, 0) {
				continue
			}
			y := 0
			for b.fits(b.piece, rot, x, y+1) {
				y++
			}
			trial := b.board
			for _, c := range rotated(bunchPieces[b.piece], rot) {
				if y+c[1] >= 0 {
					trial[y+c[1]][x+c[0]] = 1
				}
			}
			if score := bunchScore(trial); score > best {
				best, b.targetX, b.targetRot = score, x, rot
			}
		}
	}
}

func bunchScore(bd [bunchH][bunchW]int) float64 {
	heights := [bunchW]int{}
	holes, lines := 0, 0
	for x := 0; x < bunchW; x++ {
		seen := false
		for y := 0; y < bunchH; y++ {
			if bd[y][x] != 0 {
				if !seen {
					heights[x] = bunchH - y
					seen = true
				}
			} else if seen {
				holes++
			}
		}
	}
	for y := 0; y < bunchH; y++ {
		full := true
		for x := 0; x < bunchW; x++ {
			full = full && bd[y][x] != 0
		}
		if full {
			lines++
		}
	}
	agg, bump := 0, 0
	for x := 0; x < bunchW; x++ {
		agg += heights[x]
		if x > 0 {
			bump += abs(heights[x] - heights[x-1])
		}
	}
	return -0.51*float64(agg) + 0.76*float64(lines) - 0.36*float64(holes) - 0.18*float64(bump)
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

func (b *bunchState) spawn(seed uint64) bool {
	b.piece = int(cellHash(seed, b.count, 0, 34) * float64(len(bunchPieces)))
	b.count++
	b.rot, b.x, b.y = 0, bunchW/2-1, -1
	if !b.fits(b.piece, 0, b.x, 0) {
		return false
	}
	b.plan()
	return true
}

// tick moves the game on a step: turn and slide toward the plan, else drop.
func (b *bunchState) tick(seed uint64) {
	switch {
	case b.rot != b.targetRot && b.fits(b.piece, b.targetRot, b.x, b.y):
		b.rot = b.targetRot
	case b.x < b.targetX && b.fits(b.piece, b.rot, b.x+1, b.y):
		b.x++
	case b.x > b.targetX && b.fits(b.piece, b.rot, b.x-1, b.y):
		b.x--
	case b.fits(b.piece, b.rot, b.x, b.y+1):
		b.y++
	default:
		for _, c := range rotated(bunchPieces[b.piece], b.rot) {
			if b.y+c[1] >= 0 {
				b.board[b.y+c[1]][b.x+c[0]] = b.piece + 1
			}
		}
		for y := bunchH - 1; y >= 0; y-- {
			full := true
			for x := 0; x < bunchW; x++ {
				full = full && b.board[y][x] != 0
			}
			if full {
				copy(b.board[1:y+1], b.board[0:y])
				b.board[0] = [bunchW]int{}
				b.lines++
				y++
			}
		}
		if !b.spawn(seed) {
			*b = bunchState{last: b.last}
			b.spawn(seed + 1)
		}
	}
}

func saverBunches(s *saverState, g *fxGrid, now time.Time, pal loadingPalette) {
	if s.bunch == nil {
		s.bunch = &bunchState{last: now}
		s.bunch.spawn(s.seed)
	}
	b := s.bunch
	for now.Sub(b.last) >= 45*time.Millisecond {
		b.tick(s.seed)
		b.last = b.last.Add(45 * time.Millisecond)
	}
	colours := []string{pal.purple, pal.pink, pal.green, pal.cyan, pal.yellow, "#ffb86c", pal.red}
	top, left := max(0, (g.h-bunchH-2)/2), (g.w-bunchW*2-2)/2
	edge := sgrFor(faint(pal.fg, pal, .5), "", false)
	for y := 0; y <= bunchH; y++ {
		g.text(top+y, left, "│", edge)
		g.text(top+y, left+bunchW*2+1, "│", edge)
	}
	g.text(top+bunchH+1, left, "└"+repeatRune('─', bunchW*2)+"┘", edge)
	draw := func(x, y, piece int) {
		if y >= 0 {
			g.text(top+y, left+1+x*2, "▐▌", sgrFor(colours[piece%len(colours)], "", true))
		}
	}
	for y := 0; y < bunchH; y++ {
		for x := 0; x < bunchW; x++ {
			if p := b.board[y][x]; p != 0 {
				draw(x, y, p-1)
			}
		}
	}
	for _, c := range rotated(bunchPieces[b.piece], b.rot) {
		draw(b.x+c[0], b.y+c[1], b.piece)
	}
	g.text(top+2, left+bunchW*2+5, "FALLING BUNCHES", sgrFor(pal.purple, "", true))
	g.text(top+4, left+bunchW*2+5, fmt.Sprintf("LINES  %d", b.lines), sgrFor(pal.fg, "", false))
}

// saverPlasma: the demoscene plasma, full screen and in full colour (too
// busy for behind the login form, fine with nothing on top of it).
func saverPlasma(g *fxGrid, t float64, pal loadingPalette) {
	shades := []string{" ", "░", "▒", "▓", "█"}
	for r := 0; r < g.h; r++ {
		for c := 0; c < g.w; c++ {
			x, y := float64(c)*.07, float64(r)*.14
			v := (math.Sin(x+t*.9) + math.Sin(y-t*.6) + math.Sin((x+y)*.6+t*.4) +
				math.Sin(math.Hypot(x-4+math.Sin(t*.3)*3, y-3)*1.6-t)) / 4 // -1..1
			hue := math.Mod((v+1)*120+t*25+260, 360)
			ch := shades[min(len(shades)-1, int((v+1)/2*float64(len(shades))))]
			if ch != " " {
				g.set(r, c, ch, sgrFor(hsvHex(hue, .55, .9), "", false), 1)
			}
		}
	}
}
