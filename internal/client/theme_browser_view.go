package client

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/themes"
	"github.com/google/uuid"
)

// ThemeBrowserState holds state for the interactive theme selection UI
type ThemeBrowserState struct {
	ThemeNames    []string      // all available theme slugs
	SelectedIndex int           // cursor position in the list
	PreviousTheme *themes.Theme // theme active before the browser was opened
	PreviousView  View          // view to return to on Esc/Enter
}

// SettingsState holds state for the Settings view (app-level preferences)
type SettingsState struct {
	Categories       []string      // Category names (e.g., "Theme", "Notifications")
	SelectedCategory int           // Cursor position in category list
	FocusOnForm      bool          // Toggle: false = category list, true = form/content
	PreviousView     View          // View to return to on Esc

	// Theme category state (migrated from theme browser)
	AvailableThemes  []string      // All theme slugs
	SelectedTheme    int           // Cursor position in theme list
	OriginalTheme    string        // Theme name before opening Settings (for Esc revert)

	// Manage Servers category state
	SelectedServer   int           // Cursor position in server list

	// Server form state (add/edit server)
	ServerFormOpen   bool
	ServerFormState  *ServerFormState
}

// ServerFormState holds state for the add/edit server form
type ServerFormState struct {
	Mode        string // "add" or "edit"
	ServerID    *uuid.UUID
	ServerIndex int
	NameInput   string
	NameCursor  int
	AddressInput string
	AddressCursor int
	PortInput   string
	PortCursor  int
	UseTLS      bool
	FocusField  int // 0=name, 1=address, 2=port, 3=tls, 4=submit, 5=cancel
	ErrorMsg    string
}

// ServerManagementState holds state for the Server Management view (server-level admin)
type ServerManagementState struct {
	Categories       []string      // Category names (e.g., "Roles", "Members", "Channels")
	SelectedCategory int           // Cursor position in category list
	FocusOnForm      bool          // Toggle: false = category list, true = form/content
	PreviousView     View          // View to return to on Esc

	// Roles category state
	RoleList         []*models.Role
	SelectedRole     int

	// Role creation/edit modal state
	RoleFormOpen     bool
	RoleFormState    *RoleFormState

	// Role delete confirmation dialog
	DeleteConfirmOpen         bool
	DeleteConfirmRole         *models.Role
	DeleteConfirmChannel      *models.Channel
	DeleteConfirmFocusedButton int // 0 = Yes/Delete, 1 = No/Cancel

	// Permissions editor state (full-page modal)
	PermissionsEditorOpen bool         // Is permissions editor open?
	PermissionsEditorRole *models.Role // Which role's permissions are being edited
	PermSelectedIndex     int          // Cursor position in permissions list
	PermScrollOffset      int          // Scroll offset for long list
	PermModifiedBits      uint64       // Modified permission bitfield (for previewing changes)

	// Members category state
	MemberList       []*MemberDisplay
	SelectedMember   int
	FilterRole       string             // "" = all roles, or role name
	FilterOnline     string             // "all", "online", "offline"
	SearchQuery      string
	SortBy           string             // "username", "joined", "role"
	BulkSelectMode   bool
	BulkSelected     map[uuid.UUID]bool // Selected member user IDs

	// Member filter panel UI
	FilterPanelOpen   bool
	FilterPanelFocus  int // 0=role, 1=status, 2=sort

	// Member search input
	SearchInputOpen   bool
	SearchInputValue  string
	SearchInputCursor int

	// Member role assignment
	RoleAssignOpen        bool
	RoleAssignMember      *MemberDisplay      // Member being assigned roles
	RoleAssignSelections  map[uuid.UUID]bool  // Role ID -> selected state
	RoleAssignFocus       int                 // Focused role index in list

	// Member moderation
	KickConfirmOpen       bool
	KickConfirmMember     *MemberDisplay
	KickConfirmFocusedBtn int // 0 = Yes, 1 = No

	BanConfirmOpen        bool
	BanConfirmMember      *MemberDisplay
	BanConfirmFocusedBtn  int // 0 = Yes, 1 = No

	UnmuteConfirmOpen     bool
	UnmuteConfirmMember   *MemberDisplay
	UnmuteConfirmFocusedBtn int // 0 = Yes, 1 = No

	MuteDurationOpen      bool
	MuteDurationMember    *MemberDisplay
	MuteDurationFocus     int    // Radio button/field index
	MuteDurationCustom    string // Custom duration text input
	MuteDurationReason    string // Optional reason

	// Channels category state
	ChannelList         []*models.Channel
	SelectedChannel     int
	ChannelFormOpen     bool
	ChannelFormState    *ChannelFormState
	MoveDialogOpen      bool
	MoveDialogState     *MoveDialogState

	// Messages category state
	RetentionPolicy      *models.MessageRetentionPolicy
	RetentionFormState   *RetentionFormState
	PruneConfirmOpen     bool
	PruneResultsOpen     bool
	PruneResults         *protocol.MessagesPrunedPayload
	ChannelOverrides     []*models.MessageRetentionPolicy
	SelectedOverride     int

	// Channel override picker (add exempt)
	OverrideChannelPickerOpen bool
	OverrideChannelList       []*models.Channel
	OverrideChannelSelected   int

	// Remove exempt picker
	RemoveExemptPickerOpen bool
	RemoveExemptSelected   int
}

// RoleFormState holds state for the role creation/edit modal
type RoleFormState struct {
	Mode          string      // "create" or "edit"
	EditingRoleID *uuid.UUID  // Role being edited (nil for create)
	NameInput     string
	NameCursor    int
	PresetIndex   int    // 0=Text, 1=Moderator, 2=Admin, 3=Custom
	ColorIndex    int    // 0=Gold, 1=Blue, 2=Red, 3=Green, 4=Purple
	IsHoisted        bool
	IsMentionable    bool
	DisplayOrder     string // Numeric input field
	DisplayOrderCursor int
	FocusField       int    // 0=name, 1=preset, 2=color, 3=hoisted, 4=mentionable, 5=displayorder, 6=submit, 7=cancel
	ErrorMsg         string

	// Permissions editor state
	PermissionsEditorOpen bool   // Is permissions editor modal open?
	CustomPermissions     uint64 // Custom permission bitfield
	PermSelectedIndex     int    // Cursor position in permissions list
	PermScrollOffset      int    // Scroll offset for long list
}

// ChannelFormState holds state for channel/category creation/editing
type ChannelFormState struct {
	Mode              string          // "create" or "edit"
	EditingChannelID  *uuid.UUID      // Channel being edited (nil for create)
	NameTextInput     textinput.Model // Text input for channel name
	TypeIndex         int             // 0=Text Channel, 1=Category
	CategoryID        *uuid.UUID      // Pre-filled based on selection
	FocusField        int             // 0=name, 1=type, 2=submit, 3=cancel
	ErrorMsg          string
}

// MoveDialogState holds state for the move channel dialog
type MoveDialogState struct {
	Channel         *models.Channel
	CategoryList    []*models.Channel // Categories only
	SelectedIndex   int
}

// RetentionFormState holds state for the retention policy editor
type RetentionFormState struct {
	Mode                    string      // "server" or "channel"
	ChannelID               *uuid.UUID  // nil = server default
	TimeRetentionDays       string
	SystemTimeRetentionDays string
	MaxMessageCount         string
	FocusField              int         // 0=time, 1=system_time, 2=count, 3=save, 4=cancel
	CursorPos               int
}

// openThemeBrowser transitions the app into the theme browser.
// Call from any view with a Ctrl+T or /theme action.
func (a *App) openThemeBrowser(returnTo View) {
	names := themes.ListAvailableThemes()
	if len(names) == 0 {
		names = []string{"dracula"}
	}

	// Find current theme in the list so the cursor starts there
	currentIdx := 0
	if a.uiConfig != nil {
		for i, n := range names {
			if n == a.uiConfig.Theme {
				currentIdx = i
				break
			}
		}
	}

	a.themeBrowserState = &ThemeBrowserState{
		ThemeNames:    names,
		SelectedIndex: currentIdx,
		PreviousTheme: a.theme,
		PreviousView:  returnTo,
	}
	a.view = ViewThemeBrowser
}

// handleThemeBrowserKey processes key events when in ViewThemeBrowser.
func (a *App) handleThemeBrowserKey(msg tea.KeyMsg) tea.Cmd {
	s := a.themeBrowserState
	if s == nil {
		a.view = ViewMain
		return nil
	}

	switch msg.String() {
	case "up", "k":
		if s.SelectedIndex > 0 {
			s.SelectedIndex--
			a.previewTheme(s.ThemeNames[s.SelectedIndex])
		}

	case "down", "j":
		if s.SelectedIndex < len(s.ThemeNames)-1 {
			s.SelectedIndex++
			a.previewTheme(s.ThemeNames[s.SelectedIndex])
		}

	case "enter":
		// Confirm selection — save to config
		chosen := s.ThemeNames[s.SelectedIndex]
		a.applyAndSaveTheme(chosen)
		returnTo := s.PreviousView
		a.themeBrowserState = nil
		a.view = returnTo
		a.statusMessage = fmt.Sprintf("Theme set to %q", themes.GetThemeDisplayName(chosen))

	case "esc", "ctrl+t":
		// Cancel — restore previous theme
		if s.PreviousTheme != nil {
			a.SetTheme(s.PreviousTheme)
		}
		returnTo := s.PreviousView
		a.themeBrowserState = nil
		a.view = returnTo
	}

	return nil
}

// previewTheme applies a theme temporarily (without saving) for live preview.
func (a *App) previewTheme(name string) {
	t, err := themes.GetTheme(name)
	if err != nil {
		return
	}
	a.SetTheme(t)
}

// applyAndSaveTheme applies a theme and persists it to config.json.
func (a *App) applyAndSaveTheme(name string) {
	t, err := themes.GetTheme(name)
	if err != nil {
		return
	}
	a.SetTheme(t)

	if a.uiConfig != nil {
		a.uiConfig.Theme = name
	}
	if a.configMgr != nil {
		cfg, err := a.configMgr.LoadAppConfig()
		if err != nil || cfg == nil {
			cfg = &AppConfig{Version: 1}
		}
		cfg.UI.Theme = name
		_ = a.configMgr.SaveAppConfig(cfg)
	}
}

// renderThemeBrowserView renders the full-screen theme browser.
func (a *App) renderThemeBrowserView() string {
	s := a.themeBrowserState
	if s == nil {
		return ""
	}

	totalWidth := a.width
	totalHeight := a.height
	if totalWidth < 40 {
		totalWidth = 40
	}
	if totalHeight < 10 {
		totalHeight = 10
	}

	// Split: left list (~28 chars) | right preview (rest)
	listWidth := 28
	previewWidth := totalWidth - listWidth - 1 // -1 for separator
	if previewWidth < 30 {
		previewWidth = 30
	}
	innerHeight := totalHeight - 4 // title + border

	// ── Left: theme list ──────────────────────────────────────────
	var listBuf strings.Builder
	listHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Purple)).
		Bold(true).
		Width(listWidth - 2)
	listBuf.WriteString(listHeaderStyle.Render("SELECT THEME"))
	listBuf.WriteString("\n")
	listBuf.WriteString(lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Colors.Comment)).
		Width(listWidth - 2).
		Render("↑↓ navigate · Enter save · Esc cancel"))
	listBuf.WriteString("\n\n")

	for i, slug := range s.ThemeNames {
		displayName := themes.GetThemeDisplayName(slug)
		// Truncate if needed
		maxLen := listWidth - 6
		if len([]rune(displayName)) > maxLen {
			displayName = string([]rune(displayName)[:maxLen-1]) + "…"
		}

		var line string
		if i == s.SelectedIndex {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Background)).
				Background(lipgloss.Color(a.theme.Colors.Purple)).
				Bold(true).
				Width(listWidth - 2).
				Render("▶ " + displayName)
		} else {
			line = lipgloss.NewStyle().
				Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
				Width(listWidth - 2).
				Render("  " + displayName)
		}
		listBuf.WriteString(line)
		listBuf.WriteString("\n")
	}

	listPanel := lipgloss.NewStyle().
		Width(listWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Render(listBuf.String())

	// ── Right: live preview ───────────────────────────────────────
	var prevBuf strings.Builder
	t := a.theme // currently previewed theme (already applied)

	titleStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Purple)).
		Bold(true)
	prevBuf.WriteString(titleStyle.Render(t.Meta.Name))
	if t.Meta.Author != "" {
		prevBuf.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Colors.Comment)).
			Render("  by " + t.Meta.Author))
	}
	prevBuf.WriteString("\n")
	if t.Meta.Description != "" {
		prevBuf.WriteString(lipgloss.NewStyle().
			Foreground(lipgloss.Color(t.Colors.Comment)).
			Italic(true).
			Render(t.Meta.Description))
		prevBuf.WriteString("\n")
	}
	prevBuf.WriteString("\n")

	// Color swatches
	swatchLabel := lipgloss.NewStyle().Foreground(lipgloss.Color(t.Colors.Comment)).Render
	swatch := func(color, label string) string {
		var block string
		if color == "" {
			// Empty color means "terminal default" — show a placeholder instead of invisible blank
			block = lipgloss.NewStyle().
				Foreground(lipgloss.Color(t.Colors.Comment)).
				Render("··")
		} else {
			block = lipgloss.NewStyle().
				Background(lipgloss.Color(color)).
				Foreground(lipgloss.Color(color)).
				Render("  ")
		}
		return block + " " + swatchLabel(label)
	}
	swatches := []string{
		swatch(t.Colors.Background, "Background"),
		swatch(t.Colors.Foreground, "Foreground"),
		swatch(t.Colors.Purple, "Purple"),
		swatch(t.Colors.Cyan, "Cyan"),
		swatch(t.Colors.Green, "Green"),
		swatch(t.Colors.Red, "Red"),
		swatch(t.Colors.Orange, "Orange"),
		swatch(t.Colors.Yellow, "Yellow"),
	}
	cols := 2
	for i := 0; i < len(swatches); i += cols {
		for j := 0; j < cols && i+j < len(swatches); j++ {
			if j > 0 {
				prevBuf.WriteString("   ")
			}
			prevBuf.WriteString(lipgloss.NewStyle().Width((previewWidth-6)/cols).Render(swatches[i+j]))
		}
		prevBuf.WriteString("\n")
	}

	prevBuf.WriteString("\n")

	// Sample chat preview
	chatHeaderStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(t.Colors.Comment)).Bold(true)
	prevBuf.WriteString(chatHeaderStyle.Render("── Chat Preview ──"))
	prevBuf.WriteString("\n")

	sampleMessages := []struct{ name, color, text string }{
		{"alice", t.Semantic.ChatUsernameOther, "Hey everyone! Welcome to Concord."},
		{"you", t.Semantic.ChatUsernameSelf, "Thanks! Love the new " + t.Meta.Name + " theme."},
		{"bob", t.Semantic.ChatUsernameOther, "Don't forget: @you mentioned something!"},
	}
	for _, msg := range sampleMessages {
		nameStr := lipgloss.NewStyle().Foreground(lipgloss.Color(msg.color)).Bold(true).Render(msg.name)
		// highlight @you in mentions
		content := msg.text
		if strings.Contains(content, "@you") {
			parts := strings.SplitN(content, "@you", 2)
			mentionStr := lipgloss.NewStyle().
				Foreground(lipgloss.Color(t.Semantic.ChatMention)).Bold(true).Render("@you")
			content = parts[0] + mentionStr + parts[1]
		}
		prevBuf.WriteString(nameStr + "  " + content + "\n")
	}

	prevBuf.WriteString("\n")

	// Status bar preview
	statusStyle := lipgloss.NewStyle().
		Background(lipgloss.Color(t.Colors.Selection)).
		Foreground(lipgloss.Color(t.Colors.Foreground)).
		Width(previewWidth - 4)
	prevBuf.WriteString(statusStyle.Render(
		fmt.Sprintf(" [you@server:8080] #general  variant: %s", t.Meta.Variant)))

	_ = innerHeight // used implicitly through Height() below

	previewPanel := lipgloss.NewStyle().
		Width(previewWidth).
		Height(totalHeight - 2).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).
		Render(prevBuf.String())

	// ── Assemble ──────────────────────────────────────────────────
	content := lipgloss.JoinHorizontal(lipgloss.Top, listPanel, previewPanel)

	// Title bar
	titleBar := lipgloss.NewStyle().
		Width(totalWidth).
		Background(lipgloss.Color(a.theme.Colors.Purple)).
		Foreground(lipgloss.Color(a.theme.Colors.Background)).
		Bold(true).
		Render("  Concord Theme Browser")

	return lipgloss.JoinVertical(lipgloss.Left, titleBar, content)
}
