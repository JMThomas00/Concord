package hub

import (
	"io"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/log"
)

var (
	// Logger is the hub's root logger (see InitLogger).
	Logger *log.Logger

	// Component-specific loggers
	SysLog *log.Logger // lifecycle: startup, shutdown, config
	ApiLog *log.Logger // REST handlers: register, heartbeat, join
	RegLog *log.Logger // registry maintenance: offline sweep, stale purge
	FedLog *log.Logger // federation: peer sync
)

// InitLogger initializes the hub loggers, styled to match the Concord server.
// Plain mode writes to stderr (colorized); dashboard mode re-initializes with
// the Stats ring buffer as the writer, which yields uncolored lines for the
// log pane.
func InitLogger(w io.Writer, level log.Level) {
	Logger = log.NewWithOptions(w, log.Options{
		ReportCaller:    false,
		ReportTimestamp: true,
		TimeFormat:      time.Kitchen,
		Prefix:          "Grapevine",
	})

	Logger.SetLevel(level)
	Logger.SetStyles(customStyles())

	SysLog = Logger.With("component", "SYS")
	ApiLog = Logger.With("component", "API")
	RegLog = Logger.With("component", "REG")
	FedLog = Logger.With("component", "FED")
}

// customStyles matches internal/server/logger.go so hub and server logs read
// as one family.
func customStyles() *log.Styles {
	s := log.DefaultStyles()

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

	s.Key = lipgloss.NewStyle().Foreground(lipgloss.Color("213"))   // Pink
	s.Value = lipgloss.NewStyle().Foreground(lipgloss.Color("255")) // White

	s.Prefix = lipgloss.NewStyle().
		Foreground(lipgloss.Color("141")). // Purple
		Bold(true)

	return s
}
