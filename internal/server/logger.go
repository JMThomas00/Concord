package server

import (
	"os"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

var (
	// Global logger instance
	Logger *log.Logger

	// Component-specific loggers
	HubLog    *log.Logger
	AuthLog   *log.Logger
	MsgLog    *log.Logger
	DBLog     *log.Logger
	ClientLog *log.Logger
	PluginLog *log.Logger
)

// InitLogger initializes the server logger with beautiful styling
func InitLogger(level log.Level) {
	Logger = log.NewWithOptions(os.Stderr, log.Options{
		ReportCaller:    false, // Don't show caller by default (cleaner output)
		ReportTimestamp: true,
		TimeFormat:      time.Kitchen, // "3:04PM"
		Prefix:          "Concord",
	})

	Logger.SetLevel(level)
	Logger.SetStyles(customStyles())

	// Initialize component-specific loggers with icons and colors
	HubLog = Logger.With("component", "HUB")
	AuthLog = Logger.With("component", "AUTH")
	MsgLog = Logger.With("component", "MSG")
	DBLog = Logger.With("component", "DB")
	ClientLog = Logger.With("component", "CLIENT")
	PluginLog = Logger.With("component", "PLUGIN")
}

// customStyles returns beautifully styled log level rendering
func customStyles() *log.Styles {
	s := log.DefaultStyles()

	// Customize level styles with colors matching the plan
	s.Levels[log.DebugLevel] = lipgloss.NewStyle().
		SetString("DEBUG").
		Foreground(lipgloss.Color("241")) // Dim gray

	s.Levels[log.InfoLevel] = lipgloss.NewStyle().
		SetString("INFO ").
		Foreground(lipgloss.Color("86")). // Cyan
		Bold(true)

	s.Levels[log.WarnLevel] = lipgloss.NewStyle().
		SetString("WARN ").
		Foreground(lipgloss.Color("220")). // Yellow
		Bold(true)

	s.Levels[log.ErrorLevel] = lipgloss.NewStyle().
		SetString("ERROR").
		Foreground(lipgloss.Color("196")). // Red
		Bold(true)

	s.Levels[log.FatalLevel] = lipgloss.NewStyle().
		SetString("FATAL").
		Foreground(lipgloss.Color("201")). // Magenta
		Bold(true)

	// Customize key styles (for structured logging key-value pairs)
	s.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("213")) // Pink
	s.Value = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // White

	// Customize component prefix style
	s.Prefix = lipgloss.NewStyle().
		Foreground(lipgloss.Color("141")). // Purple
		Bold(true)

	return s
}
