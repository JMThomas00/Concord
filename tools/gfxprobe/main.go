// gfxprobe checks which ways of drawing images work in this terminal while
// a Bubble Tea UI redraws around them: the spike for client-side plugin
// images (vault: "Concord - Client-Side Plugins Plan", phase D0).
//
//	go run ./tools/gfxprobe
//
// It detects what the terminal says it supports, then draws the Concord
// logo in a pane while the column beside it changes ten times a second
// (the worst case for line-redrawing renderers). Keys: 1 Kitty placeholders,
// 2 Sixel, 3 iTerm2, 4 half-blocks, space pauses the churn, q quits.
package main

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
	"github.com/mattn/go-sixel"
	"golang.org/x/term"
)

//go:embed logo.png
var logoPNG []byte

const (
	imgCols = 24 // image size in cells
	imgRows = 12
	imgTop  = 5 // screen row (0-based) of the image's top edge
	imgLeft = 3 // screen column of its left edge
	kittyID = 42
)

type mode int

const (
	modeKitty mode = iota + 1
	modeSixel
	modeITerm2
	modeBlocks
)

func (m mode) String() string {
	return map[mode]string{modeKitty: "Kitty placeholders", modeSixel: "Sixel", modeITerm2: "iTerm2 inline", modeBlocks: "Half-blocks"}[m]
}

// caps is what the terminal told us.
type caps struct {
	Kitty, Sixel  bool
	CellW, CellH  int
	Hints         []string
	RawDA1, Reply string
}

// detect asks the terminal: a Kitty graphics query, the cell size in
// pixels, and Primary Device Attributes (always answered, so it ends the
// read; a 4 among its parameters means Sixel).
func detect() caps {
	c := caps{CellW: 10, CellH: 20}
	for _, k := range []string{"TERM_PROGRAM", "TERM", "WT_SESSION", "KITTY_WINDOW_ID", "GHOSTTY_RESOURCES_DIR", "WEZTERM_EXECUTABLE", "LC_TERMINAL"} {
		if v := os.Getenv(k); v != "" {
			if len(v) > 24 {
				v = v[:24] + "…"
			}
			c.Hints = append(c.Hints, k+"="+v)
		}
	}
	fd := int(os.Stdin.Fd())
	old, err := term.MakeRaw(fd)
	if err != nil {
		c.Hints = append(c.Hints, "raw mode failed: "+err.Error())
		return c
	}
	defer term.Restore(fd, old)
	fmt.Print("\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\" + "\x1b[16t" + "\x1b[c")

	got := make(chan string, 1)
	go func() {
		var b []byte
		buf := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				break
			}
			b = append(b, buf[:n]...)
			if i := bytes.Index(b, []byte("\x1b[?")); i >= 0 && bytes.IndexByte(b[i:], 'c') > 0 {
				break
			}
		}
		got <- string(b)
	}()
	var reply string
	select {
	case reply = <-got:
	case <-time.After(1500 * time.Millisecond):
		c.Hints = append(c.Hints, "no reply to the terminal queries")
	}
	c.Reply = strings.ReplaceAll(reply, "\x1b", "ESC")
	c.Kitty = strings.Contains(reply, "_Gi=31;OK")
	if i := strings.Index(reply, "\x1b[?"); i >= 0 {
		if j := strings.IndexByte(reply[i:], 'c'); j > 0 {
			c.RawDA1 = reply[i+3 : i+j]
			for _, p := range strings.Split(c.RawDA1, ";") {
				if p == "4" {
					c.Sixel = true
				}
			}
		}
	}
	var h, w int
	if i := strings.Index(reply, "\x1b[6;"); i >= 0 {
		if _, err := fmt.Sscanf(reply[i:], "\x1b[6;%d;%dt", &h, &w); err == nil && h > 0 && w > 0 {
			c.CellW, c.CellH = w, h
		}
	}
	return c
}

type tickMsg time.Time

type model struct {
	caps       caps
	mode       mode
	logo       image.Image
	paused     bool
	n          int
	w, h       int
	sentKitty  bool
	prevLines  []string
	flip       bool
	gfxSize    int // bytes of graphics data in the last frame
	kittyBlob  string
	sixelBlob  string
	iterm2Blob string
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m *model) Init() tea.Cmd { return tick() }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "1":
			m.mode, m.sentKitty = modeKitty, false
		case "2":
			m.mode = modeSixel
		case "3":
			m.mode = modeITerm2
		case "4":
			m.mode = modeBlocks
		case " ":
			m.paused = !m.paused
		}
		m.prevLines = nil // the next frame repaints the image
		return m, tea.ClearScreen
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.prevLines, m.sentKitty = nil, false
	case tickMsg:
		if !m.paused {
			m.n++
		}
		return m, tick()
	}
	return m, nil
}

// churn is the fast-changing text beside the image: every image row
// changes every tick, so the renderer redraws all of them.
func (m *model) churn(row int) string {
	spin := `|/-\`
	return fmt.Sprintf("  %c row %2d · frame %6d · %s", spin[(m.n+row)%4], row, m.n, strings.Repeat("▮", (m.n+row*3)%18))
}

func (m *model) View() string {
	pane := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#BD93F9"))
	hdr := lipgloss.NewStyle().Foreground(lipgloss.Color("#BD93F9")).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color("#6272A4"))

	var lines []string
	lines = append(lines, hdr.Render("Concord gfxprobe")+dim.Render(fmt.Sprintf("   mode: %s   kitty=%v sixel=%v cell=%dx%dpx", m.mode, m.caps.Kitty, m.caps.Sixel, m.caps.CellW, m.caps.CellH)))
	lines = append(lines, dim.Render("hints: "+strings.Join(m.caps.Hints, "  ")))
	lines = append(lines, dim.Render("reply: "+truncate(m.caps.Reply, 100)))
	lines = append(lines, dim.Render("keys: 1 kitty · 2 sixel · 3 iterm2 · 4 blocks · space pause · q quit")+dim.Render(fmt.Sprintf("   last gfx: %d bytes", m.gfxSize)))

	// The image cells: placeholders, half-blocks, or blanks for the raster
	// protocols to paint over.
	cells := make([]string, imgRows)
	for r := 0; r < imgRows; r++ {
		switch m.mode {
		case modeKitty:
			var b strings.Builder
			b.WriteString(fmt.Sprintf("\x1b[38;5;%dm", kittyID))
			for c := 0; c < imgCols; c++ {
				b.WriteRune(kitty.Placeholder)
				b.WriteRune(kitty.Diacritic(r))
				b.WriteRune(kitty.Diacritic(c))
			}
			b.WriteString("\x1b[39m")
			cells[r] = b.String()
		case modeBlocks:
			cells[r] = m.blockRow(r)
		default:
			cells[r] = strings.Repeat(" ", imgCols)
		}
	}
	box := strings.Split(pane.Render(strings.Join(cells, "\n")), "\n")
	// box row 0 is the top border; the image starts on screen row imgTop.
	for i, row := range box {
		lines = append(lines, " "+row+m.churn(i))
	}
	lines = append(lines, "", dim.Render("If the logo looks right in a mode while the column churns, that mode works here."))

	m.gfxSize = 0
	switch m.mode {
	case modeKitty:
		if !m.sentKitty {
			// Sent once; after that the placeholders are plain text.
			lines[0] = m.kittyTransmit() + lines[0]
			m.gfxSize = len(m.kittyBlob)
			m.sentKitty = true
		}
	case modeSixel, modeITerm2:
		// Repaint after any frame that redraws a row under the image. The
		// renderer only rewrites changed lines, top to bottom, so putting
		// the image at the end of the last line paints it after the rows.
		redrawn := len(m.prevLines) != len(lines)
		for r := imgTop; r < imgTop+imgRows && !redrawn && r < len(lines); r++ {
			redrawn = m.prevLines[r] != lines[r]
		}
		m.prevLines = append(m.prevLines[:0], lines...)
		if redrawn {
			blob := m.rasterBlob()
			m.gfxSize = len(blob)
			marker := "\x1b[0m"
			if m.flip = !m.flip; m.flip {
				marker = "\x1b[m"
			}
			lines[len(lines)-1] += marker + "\x1b7" + ansi.CursorPosition(imgLeft+1, imgTop+1) + blob + "\x1b8"
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) kittyTransmit() string {
	if m.kittyBlob == "" {
		var b bytes.Buffer
		_ = ansi.WriteKittyGraphics(&b, m.logo, &kitty.Options{
			ID: kittyID, Action: kitty.TransmitAndPut, Transmission: kitty.Direct, Format: kitty.PNG,
			VirtualPlacement: true, Columns: imgCols, Rows: imgRows, Quite: 2, Chunk: true,
		})
		m.kittyBlob = b.String()
	}
	return m.kittyBlob
}

func (m *model) rasterBlob() string {
	pw, ph := imgCols*m.caps.CellW, imgRows*m.caps.CellH
	scaled := scale(m.logo, pw, ph)
	if m.mode == modeSixel {
		if m.sixelBlob == "" {
			var b bytes.Buffer
			_ = sixel.NewEncoder(&b).Encode(scaled)
			// go-sixel asks for background fill (P2=0) and gives no size, so
			// some terminals paint far past the image. Ask for transparent
			// unset pixels (P2=1) and state the exact raster size instead.
			s := strings.TrimPrefix(b.String(), "\x1bP0;0;8q\"1;1")
			m.sixelBlob = fmt.Sprintf("\x1bP0;1;0q\"1;1;%d;%d", pw, ph) + s
		}
		return m.sixelBlob
	}
	if m.iterm2Blob == "" {
		var b bytes.Buffer
		_ = png.Encode(&b, scaled)
		m.iterm2Blob = fmt.Sprintf("\x1b]1337;File=inline=1;width=%d;height=%d;preserveAspectRatio=1;size=%d:%s\x07",
			imgCols, imgRows, b.Len(), base64.StdEncoding.EncodeToString(b.Bytes()))
	}
	return m.iterm2Blob
}

// blockRow draws one row of the image as half-blocks: each cell is two
// stacked pixels, the top one the foreground and the bottom one the
// background of ▀.
func (m *model) blockRow(r int) string {
	img := scale(m.logo, imgCols, imgRows*2)
	var b strings.Builder
	for c := 0; c < imgCols; c++ {
		t := color.NRGBAModel.Convert(img.At(c, r*2)).(color.NRGBA)
		u := color.NRGBAModel.Convert(img.At(c, r*2+1)).(color.NRGBA)
		t, u = over(t), over(u)
		fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// over composites a pixel onto the Dracula background.
func over(c color.NRGBA) color.NRGBA {
	bg := color.NRGBA{0x28, 0x2A, 0x36, 255}
	a := int(c.A)
	mix := func(f, b uint8) uint8 { return uint8((int(f)*a + int(b)*(255-a)) / 255) }
	return color.NRGBA{mix(c.R, bg.R), mix(c.G, bg.G), mix(c.B, bg.B), 255}
}

// scale resizes src to w×h by averaging (a box filter), keeping alpha.
func scale(src image.Image, w, h int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, w, h))
	sb := src.Bounds()
	for y := 0; y < h; y++ {
		y0, y1 := sb.Min.Y+y*sb.Dy()/h, sb.Min.Y+(y+1)*sb.Dy()/h
		for x := 0; x < w; x++ {
			x0, x1 := sb.Min.X+x*sb.Dx()/w, sb.Min.X+(x+1)*sb.Dx()/w
			var r, g, bl, a, n uint32
			for yy := y0; yy < max(y1, y0+1); yy++ {
				for xx := x0; xx < max(x1, x0+1); xx++ {
					c := color.NRGBAModel.Convert(src.At(xx, yy)).(color.NRGBA)
					r += uint32(c.R) * uint32(c.A)
					g += uint32(c.G) * uint32(c.A)
					bl += uint32(c.B) * uint32(c.A)
					a += uint32(c.A)
					n++
				}
			}
			if a > 0 {
				dst.SetNRGBA(x, y, color.NRGBA{uint8(r / a), uint8(g / a), uint8(bl / a), uint8(a / n)})
			}
		}
	}
	return dst
}

func main() {
	logo, err := png.Decode(bytes.NewReader(logoPNG))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	c := detect()
	m := &model{caps: c, logo: logo, mode: modeBlocks}
	switch {
	case c.Kitty:
		m.mode = modeKitty
	case c.Sixel:
		m.mode = modeSixel
	case os.Getenv("TERM_PROGRAM") == "iTerm.app":
		m.mode = modeITerm2
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("gfxprobe: kitty=%v sixel=%v DA1=%q cell=%dx%d reply=%q\n", c.Kitty, c.Sixel, c.RawDA1, c.CellW, c.CellH, c.Reply)
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
