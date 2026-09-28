package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	zone "github.com/lrstanley/bubblezone"

	"github.com/concord-chat/concord/internal/client"
	"github.com/concord-chat/concord/internal/themes"
)

// Version/GitCommit/BuildTime are populated at build time via the
// Makefile's shared LDFLAGS ("-X main.Version=..." etc.) -- see
// Settings > About. Defaults here keep a plain `go build`/`go run` (no
// ldflags) sensible instead of showing empty strings.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Set up logging to file for debugging. The log lives in ~/.concord/,
	// alongside the rest of the client's local state, rather than at a path
	// relative to the process's cwd -- a relative path silently failed
	// whenever the client was launched from a directory the user can't write
	// to (e.g. /usr/local/bin after a system-wide install), and every
	// log.Printf call then fell through to stderr, corrupting the Bubbletea
	// alt-screen with raw log text.
	logPath := "concord-client.log"
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		dir := filepath.Join(home, ".concord")
		if mkErr := os.MkdirAll(dir, 0o755); mkErr == nil {
			logPath = filepath.Join(dir, "concord-client.log")
		}
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err == nil {
		log.SetOutput(logFile)
		defer logFile.Close()
		log.Println("=== Concord Client Started ===")
	} else {
		fmt.Printf("Warning: Could not open log file: %v\n", err)
	}

	// Parse command line flags
	flag.Parse()

	// Print banner
	printBanner()

	// Create configuration manager
	configMgr, err := client.NewConfigManager()
	if err != nil {
		log.Fatalf("Failed to create config manager: %v", err)
	}

	// Load servers configuration
	serversConfig, err := configMgr.LoadServers()
	if err != nil {
		log.Printf("Warning: Failed to load servers config: %v", err)
		serversConfig = &client.ServersConfig{
			Version:            1,
			Servers:            []*client.ClientServerInfo{},
			DefaultPreferences: &client.DefaultPreferences{},
		}
	}

	// Load identity (nil on first run — triggers ViewIdentitySetup)
	identity := configMgr.GetIdentity()

	// Load app config for theme preference
	appConfig, cfgErr := configMgr.LoadAppConfig()
	if cfgErr != nil {
		log.Printf("Warning: Failed to load app config: %v", cfgErr)
	}

	// Load theme from config (falls back to Dracula if not found)
	themeName := "dracula"
	if appConfig != nil && appConfig.UI.Theme != "" {
		themeName = appConfig.UI.Theme
	}
	theme, themeErr := themes.GetTheme(themeName)
	if themeErr != nil {
		log.Printf("Warning: Theme %q not found, using Dracula: %v", themeName, themeErr)
		theme = themes.GetDefaultTheme()
	}

	// Create application
	app := client.NewApp(serversConfig.Servers, serversConfig.DefaultPreferences, configMgr, identity)
	app.SetTheme(theme)
	app.SetBuildInfo(Version, GitCommit, BuildTime)

	// Zone manager for mouse hit-testing: components mark their rendered
	// regions with zone.Mark() at View() time, and App.View() scans the
	// final composed output once per frame to record where they actually
	// landed on screen -- see internal/client/mouse.go for the consumer
	// side. Must be initialized before the first render.
	zone.NewGlobal()

	// Create Bubble Tea program
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		// All Motion Tracking (xterm mode 1003), not Cell Motion Tracking
		// (mode 1002): the latter only reports mouse movement while a button
		// is held, which is why the grape logo's follow-the-cursor effect
		// (steerGrapeLight) needed a button held down on Linux/Mac terminals
		// to work at all. Windows was unaffected either way -- its native
		// console mouse input doesn't negotiate this xterm protocol mode in
		// the first place.
		tea.WithMouseAllMotion(),
		// All Motion Tracking means every pixel of mouse movement produces a
		// MouseMsg, and Bubbletea calls model.View() unconditionally after
		// every single message regardless of what Update() does with it --
		// without this filter, just moving the mouse floods the app with a
		// full Update+render pass per pixel, which starved real input
		// processing badly enough to make the whole TUI unresponsive on a
		// live report. See MouseHoverFilter's own comment (grape_logo.go).
		tea.WithFilter(client.MouseHoverFilter),
		// Lets the terminal tell us when the window regains focus, so Concord
		// can force a full repaint on refocus -- see the tea.FocusMsg case in
		// App.Update for why (a real rendering-glitch report on Linux/Wayland
		// terminals, 2026-09-06).
		tea.WithReportFocus(),
	)

	// Run
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}

func printBanner() {
	banner := `
   ____                              _
  / ___|___  _ __   ___ ___  _ __ __| |
 | |   / _ \| '_ \ / __/ _ \| '__/ _' |
 | |__| (_) | | | | (_| (_) | | | (_| |
  \____\___/|_| |_|\___\___/|_|  \__,_|

  Terminal Chat Client v%s
`
	fmt.Printf(banner, Version)
}
