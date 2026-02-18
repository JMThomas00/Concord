package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/client"
	"github.com/concord-chat/concord/internal/themes"
)

func main() {
	// Set up logging to file for debugging
	logFile, err := os.OpenFile("concord-client.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
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
