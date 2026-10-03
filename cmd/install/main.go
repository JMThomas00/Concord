// Command concord-install is Concord's installer: one line from the
// README downloads and runs it (scripts/install.sh, scripts/install.ps1).
// It greets you with the grapes, asks what to install where, reviews it
// with you, installs everything (system libraries, auto-start, PATH), then
// helps you take your first steps. The work itself is internal/installer.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/installer"
	"golang.org/x/term"
)

// Version is stamped at build time (Makefile, release workflow).
var Version = "dev"

func main() {
	dryRun := flag.Bool("dry-run", false, "go through everything, but change nothing (shows what it would do)")
	from := flag.String("from", "", "install from this release archive or folder of binaries instead of downloading")
	release := flag.String("release", "", "install this release tag instead of the latest")
	noIntro := flag.Bool("no-intro", false, "skip the grapes' entrance")
	showVersion := flag.Bool("version", false, "print the installer's version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println("concord-install", Version)
		return
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		fmt.Fprintln(os.Stderr, "concord-install asks you a few questions, so it needs a terminal.")
		fmt.Fprintln(os.Stderr, "Run it directly (not through a pipe): see https://github.com/"+installer.Repo+"#install")
		os.Exit(1)
	}

	plan := installer.NewPlan(installer.Detect())
	plan.DryRun, plan.Release = *dryRun, *release
	if *from != "" {
		abs, err := filepath.Abs(*from)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		plan.Source = abs
	}

	m := newModel(plan, !*noIntro)
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord-install:", err)
		os.Exit(1)
	}
	fm := final.(*model)
	if fm.aborted {
		fmt.Println(sDim.Render("No changes made after that point. Run the installer again any time. 🍇"))
		os.Exit(1)
	}
	if fm.failed != nil {
		fmt.Println(fm.failureReport())
		os.Exit(1)
	}
	fmt.Println(fm.summary())
	if fm.openNow && !plan.DryRun {
		launchClient(plan)
	}
}

// launchClient opens Concord in this terminal, as if you'd typed it.
func launchClient(plan *installer.Plan) {
	cmd := exec.Command(plan.Binary(installer.Client))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Concord didn't start:", err)
	}
}
