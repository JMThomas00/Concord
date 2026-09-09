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

// Version/GitCommit/BuildTime are populated at build time via the
// Makefile's shared LDFLAGS ("-X main.Version=..." etc.) -- reported to
// connecting clients via ReadyPayload, see Server Settings > About.
// Defaults here keep a plain `go build`/`go run` (no ldflags) sensible.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	host := flag.String("host", "", "Host to bind to (overrides config)")
	port := flag.Int("port", 0, "Port to bind to (overrides config)")
	dbPath := flag.String("db", "", "Path to database file (overrides config)")
	adminEmail := flag.String("admin-email", "", "Grant admin role to this email on startup")
	fixAdmin := flag.Bool("fix-admin", false, "Repair broken Admin role (permissions, is_hoisted)")
	cleanupDuplicates := flag.Bool("cleanup-duplicates", false, "Merge and remove duplicate roles")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	debugMode := flag.Bool("debug", false, "Enable debug logging (shorthand for --log-level debug)")
	hybridMode := flag.Bool("hybrid", false, "Enable hybrid dashboard with live logs")
	dashboardOnlyMode := flag.Bool("dashboard", false, "Enable full-screen dashboard mode (no live logs)")
	reconfigure := flag.Bool("reconfigure", false, "Re-run setup wizard to reconfigure server")
	flag.Parse()

	// Parse and initialize logger early
	if *debugMode {
		*logLevel = "debug"
	}
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
	server.InitLogger(os.Stderr, level)
	database.SetDebug(level == charmlog.DebugLevel)

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
		// Show ToS first
		if !runToSSetup() {
			fmt.Fprintln(os.Stderr, "\nYou must accept the Terms of Service to run a Concord server.")
			os.Exit(1)
		}

		// ToS accepted, proceed with setup
		config = runFirstRunSetup(nil)
		config.TermsAccepted = true
	} else if *reconfigure {
		// Load existing config to pre-populate setup
		existingConfig := server.DefaultConfig()
		configFileToLoad := configFilename
		if *configPath != "" {
			configFileToLoad = *configPath
		}

		if _, err := os.Stat(configFileToLoad); err == nil {
			if err := loadConfig(configFileToLoad, existingConfig); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load existing config: %v\n", err)
				os.Exit(1)
			}
		}

		// Run setup with existing values
		fmt.Println("\n🔧 Reconfiguring server settings...")
		config = runFirstRunSetup(existingConfig)
		config.TermsAccepted = existingConfig.TermsAccepted // Preserve ToS acceptance
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

	// Clear screen for clean server startup (only in normal mode -- either
	// dashboard mode takes over the whole screen itself).
	if !*hybridMode && !*dashboardOnlyMode {
		fmt.Print("\033[2J\033[H")
	}

	// Print beautiful startup banner and information (only in normal mode)
	if !*hybridMode && !*dashboardOnlyMode {
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

	// Handle --fix-admin: repair broken Admin role
	if *fixAdmin {
		db, err := database.New(config.DatabasePath)
		if err != nil {
			server.Logger.Fatal("Failed to open database for fix-admin", "error", err)
		}
		// Get default server
		srv, _, err := db.EnsureDefaultServer("Concord Server")
		if err != nil {
			server.Logger.Fatal("Failed to get server for fix-admin", "error", err)
		}
		if err := db.FixAdminRole(srv.ID); err != nil {
			server.Logger.Fatal("Failed to fix Admin role", "error", err)
		}
		server.AuthLog.Info("Admin role repaired (permissions + is_hoisted + position)")
		db.Close()
	}

	// Handle --cleanup-duplicates: merge and remove duplicate roles
	if *cleanupDuplicates {
		db, err := database.New(config.DatabasePath)
		if err != nil {
			server.Logger.Fatal("Failed to open database for cleanup-duplicates", "error", err)
		}
		// Get default server
		srv, _, err := db.EnsureDefaultServer("Concord Server")
		if err != nil {
			server.Logger.Fatal("Failed to get server for cleanup-duplicates", "error", err)
		}
		if err := db.CleanupDuplicateRoles(srv.ID); err != nil {
			server.Logger.Fatal("Failed to cleanup duplicate roles", "error", err)
		}
		server.AuthLog.Info("Duplicate roles cleaned up successfully")
		db.Close()
	}

	// Create and run server
	srv, err := server.New(config)
	if err != nil {
		server.Logger.Fatal("Failed to create server", "error", err)
	}

	// Tell the server where its config lives so Grapevine can persist credentials.
	effectiveConfigPath := configFilename
	if *configPath != "" {
		effectiveConfigPath = *configPath
	}
	srv.SetConfigPath(effectiveConfigPath)
	srv.SetBuildInfo(Version, GitCommit, BuildTime)

	// Hybrid wins if both flags are somehow passed together -- it's the
	// more capable of the two (panels plus live scrolling logs), matching
	// Server.Run()'s own precedence.
	if *hybridMode {
		srv.SetDashboardMode(true)
	} else if *dashboardOnlyMode {
		srv.SetFullDashboardMode(true)
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
