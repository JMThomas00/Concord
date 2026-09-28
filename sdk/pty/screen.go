package pty

import (
	"io"

	"github.com/charmbracelet/x/vt"
)

// screen is the program's virtual terminal: its output goes in, a styled
// snapshot of the screen comes out.
type screen struct {
	emu           *vt.SafeEmulator
	width, height int
}

func newScreen(w, h int) *screen {
	return &screen{emu: vt.NewSafeEmulator(w, h), width: w, height: h}
}

func (s *screen) write(p []byte) { _, _ = s.emu.Write(p) }

// render is the current screen with its colors and styles as SGR codes.
func (s *screen) render() string { return s.emu.Render() }

func (s *screen) resize(w, h int) {
	s.width, s.height = w, h
	s.emu.Resize(w, h)
}

// pipeReplies forwards the emulator's answers to the program's terminal
// queries (e.g. "where's the cursor?") back to it; many programs wait for
// them on startup.
func (s *screen) pipeReplies(to io.Writer) { _, _ = io.Copy(to, s.emu) }

func (s *screen) close() { _ = s.emu.Close() }
