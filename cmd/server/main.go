package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/pelletier/go-toml/v2"
	"github.com/concord-chat/concord/internal/server"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	host := flag.String("host", "", "Host to bind to (overrides config)")
	port := flag.Int("port", 0, "Port to bind to (overrides config)")
	dbPath := flag.String("db", "", "Path to database file (overrides config)")
	flag.Parse()

	// Load configuration
	config := server.DefaultConfig()

	if *configPath != "" {
		if err := loadConfig(*configPath, config); err != nil {
			log.Fatalf("Failed to load config: %v", err)
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
