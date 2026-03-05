package main

import (
	"flag"
	"fmt"
	"os"

	charmlog "github.com/charmbracelet/log"
	"github.com/pelletier/go-toml/v2"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/server"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	host := flag.String("host", "", "Host to bind to (overrides config)")
	port := flag.Int("port", 0, "Port to bind to (overrides config)")
	dbPath := flag.String("db", "", "Path to database file (overrides config)")
	adminEmail := flag.String("admin-email", "", "Grant admin role to this email on startup")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	hybridMode := flag.Bool("hybrid", false, "Enable hybrid dashboard with live logs")
	flag.Parse()

	// Parse and initialize logger early
	var level charmlog.Level
	switch *logLevel {
	case "debug":
		level = charmlog.DebugLevel
	case "info":
		level = charmlog.InfoLevel
	case "warn":
		level = charmlog.WarnLevel
	case "error":
		level = charmlog.ErrorLevel
	default:
		level = charmlog.InfoLevel
	}
	server.InitLogger(level)

	// Detect first-run: no config file specified and default config file absent
	isFirstRun := *configPath == ""
	if isFirstRun {
		if _, err := os.Stat(configFilename); err == nil {
			isFirstRun = false // config file already exists
		}
	}

	// Load configuration
	var config *server.Config
	if isFirstRun {
		config = runFirstRunSetup()
	} else {
		config = server.DefaultConfig()
		if *configPath != "" {
			if err := loadConfig(*configPath, config); err != nil {
				server.Logger.Fatal("Failed to load config", "error", err)
			}
		} else if _, err := os.Stat(configFilename); err == nil {
			if err := loadConfig(configFilename, config); err != nil {
				server.Logger.Fatal("Failed to load config", "error", err)
			}
		}
	}

	// Apply command line overrides
	if *host != "" {
		config.Host = *host
	}
	if *port != 0 {
		config.Port = *port
	}
	if *dbPath != "" {
		config.DatabasePath = *dbPath
	}

	// Print beautiful startup banner and information (only in normal mode)
	if !*hybridMode {
		server.PrintBanner()
		addr := fmt.Sprintf("%s:%d", config.Host, config.Port)
		server.PrintStartupInfo(addr, config.DatabasePath)
	}

	// Handle --admin-email: open DB, grant role, close, then start normally
	if *adminEmail != "" {
		db, err := database.New(config.DatabasePath)
		if err != nil {
			server.Logger.Fatal("Failed to open database for admin-email", "error", err)
		}
		if err := db.EnsureAdminRole(*adminEmail); err != nil {
			server.Logger.Fatal("Failed to grant admin role", "email", *adminEmail, "error", err)
		}
		server.AuthLog.Info("Admin role granted", "email", *adminEmail)
		db.Close()
	}

	// Create and run server
	srv, err := server.New(config)
	if err != nil {
		server.Logger.Fatal("Failed to create server", "error", err)
	}

	// Enable hybrid mode if requested
	if *hybridMode {
		srv.SetDashboardMode(true)
	}

	if err := srv.Run(); err != nil {
		server.Logger.Fatal("Server error", "error", err)
	}
}

func loadConfig(path string, config *server.Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := toml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Old printBanner removed - now using server.PrintBanner() with beautiful ASCII art
