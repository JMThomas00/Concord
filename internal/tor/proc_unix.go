//go:build !windows

package tor

import (
	"os"
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// terminate asks tor to stop (SIGTERM: it closes cleanly).
func terminate(p *os.Process) { _ = p.Signal(syscall.SIGTERM) }
