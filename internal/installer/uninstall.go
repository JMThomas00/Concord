package installer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Uninstalling (the client's Updates page, or `concord-install
// --uninstall`) removes everything the installer put on this computer: the
// server and hub stopped and their start-up entries gone, every folder
// (with the server's database and plugins), concord off the PATH and out
// of Windows Terminal, and the client's own settings (~/.concord).

// UninstallSteps is the checklist for removing the plan's components.
func (r *Runner) UninstallSteps() []Step {
	pl := r.Plan
	var steps []Step
	for _, c := range []string{Hub, Server, Client} {
		if !pl.Has(c) {
			continue
		}
		c := c
		if c == Client {
			steps = append(steps, Step{"Taking concord off your PATH", r.unpath})
		} else {
			steps = append(steps, Step{"Stopping the " + c, func(ctx context.Context) error { r.stop(c); r.clearAutostart(c, "none"); return nil }})
		}
		steps = append(steps, Step{"Removing the " + c, func(ctx context.Context) error { return r.removeDir(pl.Dir(c)) }})
	}
	steps = append(steps, Step{"Clearing Concord's settings", func(ctx context.Context) error { return r.removeDir(ConfigDir(pl.Platform.Home)) }})
	return steps
}

// removeDir deletes a folder. On Windows a program that's still running
// (the client that started this, a server that's slow to stop) can't be
// deleted, so the folder is removed once that's over.
func (r *Runner) removeDir(dir string) error {
	if dir == "" || dir == string(filepath.Separator) || filepath.Clean(dir) == filepath.Clean(r.Plan.Platform.Home) {
		return fmt.Errorf("refusing to remove %q", dir)
	}
	return r.change("remove "+dir, func() error {
		var err error
		for i := 0; i < 3; i++ {
			if err = os.RemoveAll(dir); err == nil {
				return nil
			}
			time.Sleep(time.Second)
		}
		if runtime.GOOS == "windows" {
			r.log("%s is still in use: it goes when Concord closes", dir)
			return removeWhenParentExits(dir)
		}
		return err
	})
}

// unpath undoes clientPath: the PATH entry, the concord commands, the
// Windows Terminal profile.
func (r *Runner) unpath(ctx context.Context) error {
	pl := r.Plan
	if pl.Platform.OS == Windows {
		if err := r.change("take "+pl.ClientDir+" off your PATH", func() error { return removeFromUserPath(pl.ClientDir) }); err != nil {
			return err
		}
		if base := os.Getenv("LOCALAPPDATA"); base != "" {
			frag := filepath.Join(base, "Microsoft", "Windows Terminal", "Fragments", "Concord")
			return r.change("remove the Windows Terminal profile", func() error { return os.RemoveAll(frag) })
		}
		return nil
	}
	bin := filepath.Join(pl.Platform.Home, ".local", "bin")
	for _, name := range []string{"concord", "concord-client"} {
		link := filepath.Join(bin, name)
		if target, err := os.Readlink(link); err == nil && strings.HasPrefix(target, pl.ClientDir) {
			if err := r.change("remove "+link, func() error { return os.Remove(link) }); err != nil {
				return err
			}
		}
	}
	rc := shellRC(pl.Platform)
	return r.change("remove the installer's PATH line from "+rc, func() error { return removeRCBlock(rc) })
}

// removeRCBlock takes out the lines clientPath added to a shell's startup
// file (the marker comment and the line after it).
func removeRCBlock(path string) error {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	lines := strings.Split(string(b), "\n")
	var out []string
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "# Added by concord-install" {
			i++ // and the PATH line
			if len(out) > 0 && out[len(out)-1] == "" {
				out = out[:len(out)-1] // the blank line before it
			}
			continue
		}
		out = append(out, lines[i])
	}
	if len(out) == len(lines) {
		return nil
	}
	return os.WriteFile(path, []byte(strings.Join(out, "\n")), 0o644)
}
