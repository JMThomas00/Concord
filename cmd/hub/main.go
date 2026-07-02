package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/hub"
)

const configPath = "grapevine-hub.toml"

func main() {
	setup := flag.Bool("setup", false, "re-run the first-run setup wizard")
	debug := flag.Bool("debug", false, "enable debug logging")
	dashboard := flag.Bool("dashboard", false, "run with a live TUI dashboard (stats, server list, activity log)")
	flag.Parse()

	if *debug {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	} else {
		log.SetFlags(log.LstdFlags)
	}

	cfg, err := loadOrSetup(*setup)
	if err != nil {
		log.Fatalf("configuration: %v", err)
	}

	h, err := hub.New(cfg)
	if err != nil {
		log.Fatalf("init hub: %v", err)
	}

	// Handle SIGINT / SIGTERM gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		log.Println("[hub] shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := h.Shutdown(ctx); err != nil {
			log.Printf("[hub] shutdown error: %v", err)
		}
	}()

	if *dashboard {
		runWithDashboard(h)
		return
	}

	printBanner(cfg)

	if err := h.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("hub error: %v", err)
	}
	log.Println("[hub] stopped")
}

// runWithDashboard serves the hub with the live TUI dashboard in the
// foreground. Log output is redirected into the dashboard's activity pane.
func runWithDashboard(h *hub.Hub) {
	log.SetFlags(log.Ltime)
	log.SetOutput(h.Stats()) // feed the dashboard's log pane

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

	log.SetOutput(os.Stderr)
	select {
	case err := <-startErrCh:
		log.Fatalf("hub error: %v", err)
	default:
		log.Println("[hub] stopped")
	}
}

// loadOrSetup loads config from disk or runs the stdin setup wizard.
func loadOrSetup(forceSetup bool) (*hub.Config, error) {
	if !forceSetup {
		if _, err := os.Stat(configPath); err == nil {
			return hub.LoadConfig(configPath)
		}
		// First run.
		log.Println("[hub] no config found, starting setup wizard…")
	}
	return runSetupWizard()
}

func printBanner(cfg *hub.Config) {
	fmt.Printf("\n  Grapevine Hub · %s\n", cfg.HubName)
	fmt.Printf("  Listening on %s:%d\n\n", cfg.Host, cfg.Port)
}
