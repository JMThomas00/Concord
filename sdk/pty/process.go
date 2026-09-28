package pty

import "io"

// process is a program running in a pseudo-terminal: read its output,
// write its input, resize its terminal.
type process interface {
	io.ReadWriter
	Resize(width, height int) error
	Close() error
}
