//go:build windows

package server

import (
	"fmt"
	"os"
	"os/exec"
)

// Relaunch starts a fresh copy of the same program and flags, writing
// where this one writes (the startup task's log, or this console), then
// exits. Windows has no exec, so the new copy is a new process.
// Not yet tried on a real Windows startup task.
func Relaunch() error {
	if startedFrom == "" {
		return fmt.Errorf("can't tell where this program is")
	}
	cmd := exec.Command(startedFrom, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if dir, err := os.Getwd(); err == nil {
		cmd.Dir = dir
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	Logger.Info("Started again", "program", startedFrom, "pid", cmd.Process.Pid)
	os.Exit(0)
	return nil
}
