//go:build !windows

package pty

import (
	"os"
	"os/exec"

	cpty "github.com/creack/pty"
)

type unixProcess struct {
	cmd *exec.Cmd
	tty *os.File
}

func startProcess(opts Options, dataDir string, width, height int) (process, error) {
	cmd := exec.Command(opts.Command, opts.Args...)
	cmd.Dir = opts.Dir
	if cmd.Dir == "" {
		cmd.Dir = dataDir
	}
	cmd.Env = append([]string{
		"TERM=xterm-256color", "COLORTERM=truecolor", "LANG=C.UTF-8",
		"PATH=" + os.Getenv("PATH"), "HOME=" + dataDir,
	}, opts.Env...)
	tty, err := cpty.StartWithSize(cmd, &cpty.Winsize{Cols: uint16(width), Rows: uint16(height)})
	if err != nil {
		return nil, err
	}
	return &unixProcess{cmd: cmd, tty: tty}, nil
}

func (p *unixProcess) Read(b []byte) (int, error)  { return p.tty.Read(b) }
func (p *unixProcess) Write(b []byte) (int, error) { return p.tty.Write(b) }

func (p *unixProcess) Resize(width, height int) error {
	return cpty.Setsize(p.tty, &cpty.Winsize{Cols: uint16(width), Rows: uint16(height)})
}

func (p *unixProcess) Close() error {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
		_, _ = p.cmd.Process.Wait()
	}
	return p.tty.Close()
}
