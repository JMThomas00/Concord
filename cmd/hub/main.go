package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	charmlog "github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/hub"
)

const configPath = "grapevine-hub.toml"

func main() {
	setup := flag.Bool("setup", false, "re-run the first-run setup wizard")
	debug := flag.Bool("debug", false, "enable debug logging")
	dashboard := flag.Bool("dashboard", false, "run with a live TUI dashboard (stats, server list, activity log)")
	flag.Parse()

	logLevel := charmlog.InfoLevel
	if *debug {
		logLevel = charmlog.DebugLevel
	}
	hub.InitLogger(os.Stderr, logLevel)

	cfg, err := loadOrSetup(*setup)
	if err != nil {
		hub.SysLog.Fatal("configuration failed", "error", err)
	}

	h, err := hub.New(cfg)
	if err != nil {
		hub.SysLog.Fatal("hub init failed", "error", err)
	}

	// Handle SIGINT / SIGTERM gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		hub.SysLog.Info("shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			hub.SysLog.Error("shutdown error", "error", err)
		}
	}()

	if *dashboard {
		runWithDashboard(h, logLevel)
		return
	}

	// Clear screen for clean hub startup (plain mode only, like the server)
	fmt.Print("\033[2J\033[H")
	hub.PrintBanner()
	h.PrintStartupInfo()

	if err := h.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		hub.SysLog.Fatal("hub error", "error", err)
	}
	hub.SysLog.Info("stopped")
}

// runWithDashboard serves the hub with the live TUI dashboard in the
// foreground. Log output is redirected into the dashboard's activity pane.
func runWithDashboard(h *hub.Hub, logLevel charmlog.Level) {
	// Re-init the loggers onto the stats ring buffer: a non-terminal writer,
	// so lines land uncolored in the dashboard's log pane.
	hub.InitLogger(h.Stats(), logLevel)

	p := tea.NewProgram(hub.NewDashboard(h), tea.WithAltScreen())

	startErrCh := make(chan error, 1)
	go func() {
		if err := h.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			startErrCh <- err
			p.Quit()
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "dashboard error: %v\n", err)
	}

	// Dashboard closed (q / ctrl+c) — stop serving.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = h.Shutdown(ctx)

	hub.InitLogger(os.Stderr, logLevel)
	select {
	case err := <-startErrCh:
		hub.SysLog.Fatal("hub error", "error", err)
	default:
		hub.SysLog.Info("stopped")
	}
}

// loadOrSetup loads config from disk or runs the stdin setup wizard.
func loadOrSetup(forceSetup bool) (*hub.Config, error) {
	if !forceSetup {
		if _, err := os.Stat(configPath); err == nil {
			return hub.LoadConfig(configPath)
		}
		// First run.
		hub.SysLog.Info("no config found, starting setup wizard…")
	}
	return runSetupWizard()
}
