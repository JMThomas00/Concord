//go:build windows

package tor

import (
	"os"
	"os/exec"
	"syscall"
)

// hideWindow keeps tor from opening a console window of its own.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW
}

// terminate stops tor (Windows has no SIGTERM).
func terminate(p *os.Process) { _ = p.Kill() }
