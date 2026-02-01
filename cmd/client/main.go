package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/pelletier/go-toml/v2"
	"github.com/concord-chat/concord/internal/client"
	"github.com/concord-chat/concord/internal/themes"
)

// Config holds the client configuration
type Config struct {
	Server    ServerConfig `toml:"server"`
	Theme     string       `toml:"theme"`
	ThemesDir string       `toml:"themes_dir"`
}

// ServerConfig holds server connection settings
type ServerConfig struct {
	Address string `toml:"address"`
	Token   string `toml:"token"`
}

// DefaultConfig returns the default client configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Address: "ws://localhost:8080",
		},
		Theme:     "dracula",
		ThemesDir: "./themes",
	}
}

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	serverAddr := flag.String("server", "", "Server address (overrides config)")
	themeName := flag.String("theme", "", "Theme name (overrides config)")
	flag.Parse()

	// Load configuration
	config := DefaultConfig()

	// Try default config paths if not specified
	if *configPath == "" {
		defaultPaths := []string{
			"./concord.toml",
			"./config/client.toml",
			os.ExpandEnv("$HOME/.config/concord/client.toml"),
		}
		for _, path := range defaultPaths {
			if _, err := os.Stat(path); err == nil {
				*configPath = path
				break
			}
		}
	}

	if *configPath != "" {
		if err := loadConfig(*configPath, config); err != nil {
			log.Printf("Warning: Failed to load config: %v", err)
		}
	}

	// Apply command line overrides
	if *serverAddr != "" {
		config.Server.Address = *serverAddr
	}
	if *themeName != "" {
		config.Theme = *themeName
	}

	// Print banner
	printBanner()

	// Load theme
	var theme *themes.Theme
	if config.ThemesDir != "" {
		var err error
		theme, err = themes.LoadThemeByName(config.ThemesDir, config.Theme)
		if err != nil {
			log.Printf("Warning: Failed to load theme '%s': %v, using default", config.Theme, err)
			theme = themes.GetDefaultTheme()
		}
	} else {
		theme = themes.GetDefaultTheme()
	}

	// Create application
	app := client.NewApp(config.Server.Address)
	app.SetTheme(theme)

	// If we have a saved token, set it
	if config.Server.Token != "" {
		app.SetToken(config.Server.Token)
	}

	// Create Bubble Tea program
	p := tea.NewProgram(
		app,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	// Run
	if _, err := p.Run(); err != nil {
		log.Fatalf("Error running program: %v", err)
	}
}

func loadConfig(path string, config *Config) error {
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
                                       
  Terminal Chat Client v0.1.0
`
	fmt.Println(banner)
}
