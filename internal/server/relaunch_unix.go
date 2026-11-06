//go:build !windows

package server

import (
	"fmt"
	"os"
	"syscall"
)

// Relaunch replaces this process with a fresh start of the same program
// and flags. The process keeps its ID, terminal and output, so systemd,
// launchd, Docker and a terminal all just see the server come back; Go
// opens every file and socket close-on-exec, so the old listener is gone.
func Relaunch() error {
	if startedFrom == "" {
		return fmt.Errorf("can't tell where this program is")
	}
	Logger.Info("Starting again", "program", startedFrom)
	return syscall.Exec(startedFrom, os.Args, os.Environ()) // #nosec G702 -- this server's own program, read at start
}
