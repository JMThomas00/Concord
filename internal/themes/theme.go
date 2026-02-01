package themes

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/charmbracelet/lipgloss"
	"github.com/pelletier/go-toml/v2"
)

// Theme represents a complete color theme for Concord
type Theme struct {
	Meta     ThemeMeta     `toml:"meta"`
	Colors   ThemeColors   `toml:"colors"`
	Semantic SemanticColors `toml:"semantic"`
}

// ThemeMeta contains metadata about the theme
type ThemeMeta struct {
	Name    string `toml:"name"`
	Author  string `toml:"author"`
	Variant string `toml:"variant"` // "dark" or "light"
}

// ThemeColors contains the base color palette
type ThemeColors struct {
	Background  string `toml:"background"`
	CurrentLine string `toml:"current_line"`
	Selection   string `toml:"selection"`
	Foreground  string `toml:"foreground"`
	Comment     string `toml:"comment"`
	Red         string `toml:"red"`
	Orange      string `toml:"orange"`
	Yellow      string `toml:"yellow"`
	Green       string `toml:"green"`
	Cyan        string `toml:"cyan"`
	Purple      string `toml:"purple"`
	Pink        string `toml:"pink"`
}

// SemanticColors maps colors to specific UI purposes
type SemanticColors struct {
	SidebarBg       string `toml:"sidebar_bg"`
	SidebarFg       string `toml:"sidebar_fg"`
	SidebarSelected string `toml:"sidebar_selected"`
	SidebarHover    string `toml:"sidebar_hover"`

	ChatBg            string `toml:"chat_bg"`
	ChatFg            string `toml:"chat_fg"`
	ChatTimestamp     string `toml:"chat_timestamp"`
	ChatUsernameSelf  string `toml:"chat_username_self"`
	ChatUsernameOther string `toml:"chat_username_other"`
	ChatMention       string `toml:"chat_mention"`

	InputBg          string `toml:"input_bg"`
	InputFg          string `toml:"input_fg"`
	InputBorder      string `toml:"input_border"`
	InputBorderFocus string `toml:"input_border_focus"`

	StatusOnline  string `toml:"status_online"`
	StatusIdle    string `toml:"status_idle"`
	StatusDND     string `toml:"status_dnd"`
	StatusOffline string `toml:"status_offline"`

	Error   string `toml:"error"`
	Warning string `toml:"warning"`
	Success string `toml:"success"`
	Info    string `toml:"info"`

	Scrollbar      string `toml:"scrollbar"`
	ScrollbarHover string `toml:"scrollbar_hover"`
	Border         string `toml:"border"`
}

// Styles contains pre-computed lipgloss styles for the theme
type Styles struct {
	// Sidebar styles
	SidebarContainer lipgloss.Style
	SidebarItem      lipgloss.Style
	SidebarSelected  lipgloss.Style
	ChannelName      lipgloss.Style
	CategoryName     lipgloss.Style

	// Chat styles
	ChatContainer  lipgloss.Style
	MessageContent lipgloss.Style
	Timestamp      lipgloss.Style
	UsernameSelf   lipgloss.Style
	UsernameOther  lipgloss.Style
	Mention        lipgloss.Style
	SystemMessage  lipgloss.Style

	// Input styles
	InputContainer lipgloss.Style
	InputField     lipgloss.Style
	InputFocused   lipgloss.Style

	// Status indicators
	StatusOnline  lipgloss.Style
	StatusIdle    lipgloss.Style
	StatusDND     lipgloss.Style
	StatusOffline lipgloss.Style

	// Feedback styles
	Error   lipgloss.Style
	Warning lipgloss.Style
	Success lipgloss.Style
	Info    lipgloss.Style

	// Misc
	Border    lipgloss.Style
	Scrollbar lipgloss.Style
}

// LoadTheme loads a theme from a TOML file
func LoadTheme(path string) (*Theme, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read theme file: %w", err)
	}

	var theme Theme
	if err := toml.Unmarshal(data, &theme); err != nil {
		return nil, fmt.Errorf("failed to parse theme file: %w", err)
	}

	return &theme, nil
}

// LoadThemeByName loads a theme by name from the themes directory
func LoadThemeByName(themesDir, name string) (*Theme, error) {
	path := filepath.Join(themesDir, name+".toml")
	return LoadTheme(path)
}

// ListThemes returns a list of available theme names
func ListThemes(themesDir string) ([]string, error) {
	entries, err := os.ReadDir(themesDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read themes directory: %w", err)
	}

	var themes []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".toml" {
			name := entry.Name()[:len(entry.Name())-5] // Remove .toml extension
			themes = append(themes, name)
		}
	}

	return themes, nil
}

// BuildStyles creates lipgloss styles from a theme
func (t *Theme) BuildStyles() *Styles {
	s := &Styles{}

	// Sidebar styles
	s.SidebarContainer = lipgloss.NewStyle().
		Background(lipgloss.Color(t.Semantic.SidebarBg)).
		Foreground(lipgloss.Color(t.Semantic.SidebarFg)).
		Padding(1)

	s.SidebarItem = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.SidebarFg)).
		PaddingLeft(2)

	s.SidebarSelected = lipgloss.NewStyle().
		Background(lipgloss.Color(t.Semantic.SidebarSelected)).
		Foreground(lipgloss.Color(t.Semantic.SidebarFg)).
		PaddingLeft(2).
		Bold(true)

	s.ChannelName = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.SidebarFg))

	s.CategoryName = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Comment)).
		Bold(true).
		MarginTop(1)

	// Chat styles
	s.ChatContainer = lipgloss.NewStyle().
		Background(lipgloss.Color(t.Semantic.ChatBg)).
		Foreground(lipgloss.Color(t.Semantic.ChatFg)).
		Padding(0, 1)

	s.MessageContent = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.ChatFg))

	s.Timestamp = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.ChatTimestamp)).
		Faint(true)

	s.UsernameSelf = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.ChatUsernameSelf)).
		Bold(true)

	s.UsernameOther = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.ChatUsernameOther)).
		Bold(true)

	s.Mention = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.ChatMention)).
		Bold(true)

	s.SystemMessage = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Comment)).
		Italic(true)

	// Input styles
	s.InputContainer = lipgloss.NewStyle().
		Background(lipgloss.Color(t.Semantic.InputBg)).
		Padding(0, 1)

	s.InputField = lipgloss.NewStyle().
		Background(lipgloss.Color(t.Semantic.InputBg)).
		Foreground(lipgloss.Color(t.Semantic.InputFg)).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Semantic.InputBorder)).
		Padding(0, 1)

	s.InputFocused = s.InputField.
		BorderForeground(lipgloss.Color(t.Semantic.InputBorderFocus))

	// Status styles
	s.StatusOnline = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.StatusOnline))

	s.StatusIdle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.StatusIdle))

	s.StatusDND = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.StatusDND))

	s.StatusOffline = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.StatusOffline))

	// Feedback styles
	s.Error = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.Error))

	s.Warning = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.Warning))

	s.Success = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.Success))

	s.Info = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.Info))

	// Misc styles
	s.Border = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(t.Semantic.Border))

	s.Scrollbar = lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Semantic.Scrollbar))

	return s
}

// GetDefaultTheme returns the default Dracula theme
func GetDefaultTheme() *Theme {
	return &Theme{
		Meta: ThemeMeta{
			Name:    "Dracula",
			Author:  "Zeno Rocha",
			Variant: "dark",
		},
		Colors: ThemeColors{
			Background:  "#282A36",
			CurrentLine: "#6272A4",
			Selection:   "#44475A",
			Foreground:  "#F8F8F2",
			Comment:     "#6272A4",
			Red:         "#FF5555",
			Orange:      "#FFB86C",
			Yellow:      "#F1FA8C",
			Green:       "#50FA7B",
			Cyan:        "#8BE9FD",
			Purple:      "#BD93F9",
			Pink:        "#FF79C6",
		},
		Semantic: SemanticColors{
			SidebarBg:         "#282A36",
			SidebarFg:         "#F8F8F2",
			SidebarSelected:   "#44475A",
			SidebarHover:      "#6272A4",
			ChatBg:            "#282A36",
			ChatFg:            "#F8F8F2",
			ChatTimestamp:     "#6272A4",
			ChatUsernameSelf:  "#BD93F9",
			ChatUsernameOther: "#8BE9FD",
			ChatMention:       "#FF79C6",
			InputBg:           "#44475A",
			InputFg:           "#F8F8F2",
			InputBorder:       "#6272A4",
			InputBorderFocus:  "#BD93F9",
			StatusOnline:      "#50FA7B",
			StatusIdle:        "#F1FA8C",
			StatusDND:         "#FF5555",
			StatusOffline:     "#6272A4",
			Error:             "#FF5555",
			Warning:           "#FFB86C",
			Success:           "#50FA7B",
			Info:              "#8BE9FD",
			Scrollbar:         "#44475A",
			ScrollbarHover:    "#6272A4",
			Border:            "#6272A4",
		},
	}
}
