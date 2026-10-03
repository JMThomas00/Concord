// Command concord-install is Concord's installer: one line from the
// README downloads and runs it (scripts/install.sh, scripts/install.ps1).
// It greets you with the grapes, asks what to install where, reviews it
// with you, installs everything (system libraries, auto-start, PATH), then
// helps you take your first steps. The work itself is internal/installer.
package main

import (
	"bufio"
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
	update := flag.Bool("update", false, "update everything installed here to the latest release, keeping its settings")
	configure := flag.Bool("configure", false, "go through the questions again for what's installed, starting from its settings")
	uninstall := flag.Bool("uninstall", false, "remove everything Concord installed here, settings and data included")
	yes := flag.Bool("yes", false, "with --uninstall: don't ask again (Concord's Updates page already did)")
	ret := flag.Bool("return", false, "wait for Enter at the end (when Concord itself started this)")
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

	mode := modeInstall
	switch {
	case *update:
		mode = modeUpdate
	case *configure:
		mode = modeConfigure
	case *uninstall:
		mode = modeUninstall
	}
	if !prepare(plan, mode) {
		fmt.Println("Nothing here was installed with the Concord installer, so there's nothing to " + map[string]string{modeUpdate: "update", modeConfigure: "configure", modeUninstall: "remove"}[mode] + ".")
		fmt.Println("Run it without flags to install Concord.")
		finish(*ret, 1)
	}

	m := newModel(plan, !*noIntro && mode == modeInstall)
	m.mode, m.confirmed = mode, *yes
	m.begin()
	final, err := tea.NewProgram(m, tea.WithAltScreen()).Run()
	if err != nil {
		fmt.Fprintln(os.Stderr, "concord-install:", err)
		finish(*ret, 1)
	}
	fm := final.(*model)
	if fm.aborted {
		fmt.Println(sDim.Render("Nothing more was changed. Run the installer again any time. 🍇"))
		finish(*ret, 1)
	}
	if fm.failed != nil {
		fmt.Println(fm.failureReport())
		finish(*ret, 1)
	}
	if mode == modeUninstall {
		fmt.Println(fm.farewell())
		finish(*ret, 0)
	}
	fmt.Println(fm.summary())
	if fm.openNow && !plan.DryRun && !*ret {
		launchClient(plan)
	}
	finish(*ret, 0)
}

// finish exits, first waiting for Enter when Concord started the installer
// (so what it printed can be read before Concord takes the screen back).
func finish(wait bool, code int) {
	if wait {
		fmt.Print(sDim.Render("\n   Press Enter to go back to Concord. "))
		bufio.NewReader(os.Stdin).ReadString('\n')
	}
	os.Exit(code)
}

// launchClient opens Concord in this terminal, as if you'd typed it.
func launchClient(plan *installer.Plan) {
	cmd := exec.Command(plan.Binary(installer.Client))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "Concord didn't start:", err)
	}
}

// prepare points the plan at what's installed, for every mode but
// install; it reports false when nothing is recorded as installed.
func prepare(plan *installer.Plan, mode string) bool {
	if mode == modeInstall {
		return true
	}
	rec := installer.LoadRecord(plan.Platform.Home)
	if len(rec.Components) == 0 {
		return false
	}
	plan.ApplyRecord(rec)
	if mode == modeConfigure {
		plan.Reconfigure = true
		plan.LoadSettings()
	}
	return true
}
