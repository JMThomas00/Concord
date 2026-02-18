package main

import (
	"flag"
	"fmt"
	"log"
	"os"

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
	flag.Parse()

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
				log.Fatalf("Failed to load config: %v", err)
			}
		} else if _, err := os.Stat(configFilename); err == nil {
			if err := loadConfig(configFilename, config); err != nil {
				log.Fatalf("Failed to load config: %v", err)
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

	// Print banner
	printBanner()

	// Handle --admin-email: open DB, grant role, close, then start normally
	if *adminEmail != "" {
		db, err := database.New(config.DatabasePath)
		if err != nil {
			log.Fatalf("Failed to open database for admin-email: %v", err)
		}
		if err := db.EnsureAdminRole(*adminEmail); err != nil {
			log.Fatalf("Failed to grant admin role to %s: %v", *adminEmail, err)
		}
		log.Printf("Admin role granted to %s", *adminEmail)
		db.Close()
	}

	// Create and run server
	srv, err := server.New(config)
	if err != nil {
		log.Fatalf("Failed to create server: %v", err)
	}

	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
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

func printBanner() {
	banner := `
   ____                              _ 
  / ___|___  _ __   ___ ___  _ __ __| |
 | |   / _ \| '_ \ / __/ _ \| '__/ _' |
 | |__| (_) | | | | (_| (_) | | | (_| |
  \____\___/|_| |_|\___\___/|_|  \__,_|
                                       
  Terminal Chat Server v0.1.0
  ===========================
`
	fmt.Println(banner)
}
