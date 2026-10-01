package client

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"golang.org/x/term"
)

// Terminal graphics (To Do item D): how this terminal can show images,
// found once at startup by asking it. Plugins never deal with any of this:
// they place an image in a pane and Concord draws it the best way here.
//
//   - Kitty graphics with Unicode placeholders (Kitty, Ghostty): the image is
//     made of placeholder characters, so it redraws like any text.
//   - Sixel (Windows Terminal 1.22+, Konsole, foot, mlterm) and iTerm2 inline
//     images (iTerm2, WezTerm): pixels painted at a position, repainted
//     after any redraw of the rows under them (paintRasterImages).
//   - Half-blocks (everywhere, including macOS Terminal.app): each cell is
//     two stacked pixels drawn with ▀.

// gfxProtocol is how images are drawn.
type gfxProtocol int

const (
	gfxOff gfxProtocol = iota
	gfxBlocks
	gfxKitty
	gfxSixel
	gfxITerm2
)

func (p gfxProtocol) String() string {
	return [...]string{"off", "blocks", "kitty", "sixel", "iterm2"}[p]
}

// TerminalGraphics is what the terminal said it supports.
type TerminalGraphics struct {
	Kitty  bool // answered a Kitty graphics query
	Sixel  bool // listed Sixel (4) in its device attributes
	ITerm2 bool // iTerm2 or WezTerm, by environment
	CellW  int  // cell size in pixels (from CSI 16 t), 0 if unknown
	CellH  int
	Term   string // TERM_PROGRAM or similar, for the Display settings page
}

// termGraphics is set once by cmd/client before the UI starts.
var termGraphics TerminalGraphics

// SetTerminalGraphics records what DetectTerminalGraphics found.
func SetTerminalGraphics(g TerminalGraphics) { termGraphics = g }

// DetectTerminalGraphics asks the terminal what it can draw. It must run
// before the UI takes over the terminal: it briefly puts stdin in raw mode
// and reads the replies. Primary Device Attributes is asked last because
// every terminal answers it, which ends the read.
func DetectTerminalGraphics() TerminalGraphics {
	g := TerminalGraphics{Term: os.Getenv("TERM_PROGRAM")}
	if g.Term == "" && os.Getenv("WT_SESSION") != "" {
		g.Term = "Windows Terminal"
	}
	switch g.Term {
	case "iTerm.app", "WezTerm":
		g.ITerm2 = true
	}
	if os.Getenv("LC_TERMINAL") == "iTerm2" {
		g.ITerm2 = true
	}
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return g
	}
	old, err := term.MakeRaw(fd)
	if err != nil {
		return g
	}
	defer term.Restore(fd, old)
	fmt.Fprint(os.Stdout, "\x1b_Gi=31,s=1,v=1,a=q,t=d,f=24;AAAA\x1b\\"+"\x1b[16t"+"\x1b[c")

	got := make(chan []byte, 1)
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
		got <- b
	}()
	select {
	case reply := <-got:
		parseGraphicsReply(&g, string(reply))
	case <-time.After(500 * time.Millisecond):
		// No answer at all: assume nothing beyond half-blocks. (The reader
		// goroutine stays blocked on stdin; its bytes, if any ever come,
		// go to the UI as ordinary input.)
	}
	return g
}

// parseGraphicsReply reads the terminal's answers to the three queries.
func parseGraphicsReply(g *TerminalGraphics, reply string) {
	g.Kitty = strings.Contains(reply, "_Gi=31;OK")
	if i := strings.Index(reply, "\x1b[?"); i >= 0 {
		if j := strings.IndexByte(reply[i:], 'c'); j > 0 {
			for _, p := range strings.Split(reply[i+3:i+j], ";") {
				if p == "4" {
					g.Sixel = true
				}
			}
		}
	}
	var h, w int
	if i := strings.Index(reply, "\x1b[6;"); i >= 0 {
		if _, err := fmt.Sscanf(reply[i:], "\x1b[6;%d;%dt", &h, &w); err == nil && h > 0 && w > 0 {
			g.CellW, g.CellH = w, h
		}
	}
}

// best is the protocol to use when the setting is "auto". Kitty's
// placeholders need Kitty or Ghostty: WezTerm answers the Kitty query but
// doesn't draw placeholders, so it gets iTerm2 images instead.
func (g TerminalGraphics) best() gfxProtocol {
	switch {
	case g.Kitty && g.Term != "WezTerm":
		return gfxKitty
	case g.ITerm2:
		return gfxITerm2
	case g.Sixel:
		return gfxSixel
	}
	return gfxBlocks
}

// graphicsProtocol is how this client draws images: the Display setting,
// or the best the terminal supports.
func (a *App) graphicsProtocol() gfxProtocol {
	setting := ""
	if a.uiConfig != nil {
		setting = a.uiConfig.Display.Images
	}
	switch strings.ToLower(setting) {
	case "off":
		return gfxOff
	case "blocks":
		return gfxBlocks
	case "kitty":
		return gfxKitty
	case "sixel":
		return gfxSixel
	case "iterm2":
		return gfxITerm2
	}
	return termGraphics.best()
}

// cellPixels is the terminal's cell size in pixels (a common default when
// it didn't say).
func cellPixels() (int, int) {
	if termGraphics.CellW > 0 && termGraphics.CellH > 0 {
		return termGraphics.CellW, termGraphics.CellH
	}
	return 10, 20
}
