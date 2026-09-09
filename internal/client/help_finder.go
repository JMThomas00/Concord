package client

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// HelpFinderState holds the /help fuzzy finder's live search state --
// replaces the old static, read-only help modal (HelpModalState). See
// this file's own doc comments for the matching logic and
// openHelpFinder/handleHelpFinderKey/renderHelpFinderOverlay for the
// open/interact/render lifecycle.
type HelpFinderState struct {
	Query        string
	Results      []slashCommandInfo
	Selected     int
	ScrollOffset int
}

// slashCommandInfo describes one slash command: its name, an args usage
// hint (empty if it takes none), a one-line description, and the minimum
// role level that can use it. The single source of truth for both the
// /help finder's searchable list and Tab-completion (handleTabCompletion),
// replacing two previously separate, subtly inconsistent hardcoded lists.
type slashCommandInfo struct {
	Name        string
	Usage       string
	Description string
	MinRole     roleLevel
}

// allSlashCommands returns every slash command Concord supports, in a
// fixed, sensible order (member commands first, then moderator, then
// admin) -- unfiltered by role; see visibleSlashCommands for that.
func allSlashCommands() []slashCommandInfo {
	return []slashCommandInfo{
		{"help", "", "Show this command finder", roleLevelMember},
		{"whisper", "@user <msg>", "Send an ephemeral DM (alias: /w)", roleLevelMember},
		{"links", "[N]", "Show links from recent N messages (default: 20)", roleLevelMember},
		{"theme", "[name]", "Open theme browser, or apply theme directly", roleLevelMember},
		{"status", "<message>", "Set your status (use /status clear to remove)", roleLevelMember},
		{"nick", "<nickname>", "Set your own nickname on this server (use clear to remove)", roleLevelMember},
		{"attach", "<path> [caption]", "Share a local file peer-to-peer (you must stay online for others to download it)", roleLevelMember},
		{"download", "<attachment-id>", "Download a file someone else attached", roleLevelMember},
		{"mute", "[@user [minutes]]", "Mute the current channel, or (mods) server-mute a member", roleLevelMember},
		{"unmute", "[@user]", "Unmute the current channel, or (mods) server-unmute a member", roleLevelMember},
		{"join-voice", "[#channel]", "Join a voice channel", roleLevelMember},
		{"leave-voice", "", "Leave the current voice channel", roleLevelMember},

		{"create-channel", "<name>", "Create a new text channel", roleLevelMod},
		{"create-group", "<name>", "Create a new channel group", roleLevelMod},
		{"delete-channel", "", "Delete the current channel", roleLevelMod},
		{"delete-group", "<name>", "Delete an empty channel group", roleLevelMod},
		{"rename-channel", "<name>", "Rename the current channel", roleLevelMod},
		{"move-channel", "<group>", "Move current channel to a channel group", roleLevelMod},
		{"lock", "", "Lock current channel (only mods/admins can post)", roleLevelMod},
		{"unlock", "", "Unlock current channel (all users can post)", roleLevelMod},
		{"mute-voice", "@user", "Server-mute a user in voice", roleLevelMod},
		{"deafen-voice", "@user", "Server-deafen a user in voice", roleLevelMod},
		{"unmute-voice", "@user", "Lift voice mute/deafen", roleLevelMod},
		{"kick", "@user [reason]", "Kick a member from the server", roleLevelMod},
		{"timeout", "@user <minutes>", "Temporarily ban a member", roleLevelMod},
		{"pin", "[N]", "Pin the Nth most recent message (default: 1)", roleLevelMod},
		{"unpin", "[N]", "Unpin the Nth pinned message (default: 1)", roleLevelMod},

		{"roles", "", "List all available roles on this server", roleLevelAdmin},
		{"role", "assign|remove @user <role>", "Manage member roles", roleLevelAdmin},
		{"create-role", "<name> [preset]", "Create a new role (presets: text, moderator, admin)", roleLevelAdmin},
		{"title", "@user <title>", "Assign a custom title to a member (use clear to remove)", roleLevelAdmin},
		{"ban", "@user [reason]", "Permanently ban a member", roleLevelAdmin},
		{"unban", "@user", "Lift a ban from a member", roleLevelAdmin},
		{"move-voice", "@user <channel>", "Force-move user to a voice channel", roleLevelAdmin},
	}
}

// visibleSlashCommands returns the commands level can use, preserving
// allSlashCommands' own order.
func visibleSlashCommands(level roleLevel) []slashCommandInfo {
	all := allSlashCommands()
	out := make([]slashCommandInfo, 0, len(all))
	for _, c := range all {
		if level >= c.MinRole {
			out = append(out, c)
		}
	}
	return out
}

// Match tiers for filterSlashCommands' ranking -- lower sorts first. A
// real "fuzzy finder" per the feature's own ask, not just substring
// search: an exact-ish match on the command name always outranks a
// description hit, and a genuine fuzzy (in-order, not necessarily
// contiguous) subsequence match is the fallback once neither name nor
// description contains the query as a literal substring, rather than
// excluding it from results entirely.
const (
	matchTierNamePrefix = iota
	matchTierNameSubstring
	matchTierDescSubstring
	matchTierNameFuzzy
	matchTierDescFuzzy
)

type scoredCommand struct {
	cmd  slashCommandInfo
	tier int
	pos  int // earliest match position -- tie-breaker within a tier
}

// filterSlashCommands narrows level's visible commands to those matching
// query (case-insensitive), ranked name-prefix first, then
// name-substring, then description-substring, then a fuzzy subsequence
// match against the name and finally the description. An empty query
// returns everything, unranked (allSlashCommands' own declared order) --
// this is also the "browse everything" state when the finder first opens.
func filterSlashCommands(query string, level roleLevel) []slashCommandInfo {
	visible := visibleSlashCommands(level)
	if query == "" {
		return visible
	}
	q := strings.ToLower(query)

	var scored []scoredCommand
	for _, c := range visible {
		name := strings.ToLower(c.Name)
		desc := strings.ToLower(c.Description)

		if strings.HasPrefix(name, q) {
			scored = append(scored, scoredCommand{c, matchTierNamePrefix, 0})
			continue
		}
		if idx := strings.Index(name, q); idx >= 0 {
			scored = append(scored, scoredCommand{c, matchTierNameSubstring, idx})
			continue
		}
		if idx := strings.Index(desc, q); idx >= 0 {
			scored = append(scored, scoredCommand{c, matchTierDescSubstring, idx})
			continue
		}
		if idx, ok := fuzzySubsequenceIndex(name, q); ok {
			scored = append(scored, scoredCommand{c, matchTierNameFuzzy, idx})
			continue
		}
		if idx, ok := fuzzySubsequenceIndex(desc, q); ok {
			scored = append(scored, scoredCommand{c, matchTierDescFuzzy, idx})
			continue
		}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].tier != scored[j].tier {
			return scored[i].tier < scored[j].tier
		}
		return scored[i].pos < scored[j].pos
	})

	out := make([]slashCommandInfo, len(scored))
	for i, s := range scored {
		out[i] = s.cmd
	}
	return out
}

// fuzzySubsequenceIndex reports whether every rune of query appears in s
// in order, not necessarily contiguously -- e.g. query "wsp" matches
// "whisper" (w..s..p). Returns the index of the first matched rune, used
// only to break ties when sorting matches within the same tier.
func fuzzySubsequenceIndex(s, query string) (int, bool) {
	if query == "" {
		return 0, true
	}
	qr := []rune(query)
	qi := 0
	firstIdx := -1
	for i, r := range s {
		if r == qr[qi] {
			if firstIdx == -1 {
				firstIdx = i
			}
			qi++
			if qi == len(qr) {
				return firstIdx, true
			}
		}
	}
	return 0, false
}

// helpFinderVisibleRows caps how many results the finder shows at once;
// beyond that it scrolls, following Selected -- mirrors
// linkBrowserVisibleRows' own role for the link browser. Large enough that
// most role levels see their whole command list without scrolling (an
// admin sees ~34 commands total; a plain member sees ~12).
const helpFinderVisibleRows = 20

// openHelpFinder opens the /help interactive fuzzy finder, optionally
// pre-filled with an initial query (e.g. "/help mute" opens it already
// filtered to mute-related commands).
func (a *App) openHelpFinder(initialQuery string) {
	a.helpFinderState = &HelpFinderState{Query: initialQuery}
	a.refreshHelpFinderResults()
}

// refreshHelpFinderResults re-filters the finder's results against its
// current query and role level, clamping Selected/ScrollOffset to stay in
// bounds -- call after any change to Query.
func (a *App) refreshHelpFinderResults() {
	s := a.helpFinderState
	if s == nil {
		return
	}
	s.Results = filterSlashCommands(s.Query, a.currentUserRoleLevel())
	if s.Selected >= len(s.Results) {
		s.Selected = len(s.Results) - 1
	}
	if s.Selected < 0 {
		s.Selected = 0
	}
	s.ScrollOffset = 0
}

// closeHelpFinder closes the finder without changing the message input.
func (a *App) closeHelpFinder() {
	a.helpFinderState = nil
}

// acceptHelpFinderSelection populates the message input with the
// currently selected command (ready to fill in arguments and send) and
// closes the finder, returning focus to the input.
func (a *App) acceptHelpFinderSelection() {
	s := a.helpFinderState
	if s == nil || len(s.Results) == 0 {
		return
	}
	cmd := s.Results[s.Selected]
	a.input.SetValue("/" + cmd.Name + " ")
	a.input.CursorEnd()
	a.closeHelpFinder()
	a.focus = FocusInput
	a.input.Focus()
}

// handleHelpFinderKey handles all keyboard input while the finder is open
// (see handleKeyPress's own early-intercept guard) -- typing narrows the
// query, up/down moves the selection, Enter accepts it, Esc closes
// without changing the input.
func (a *App) handleHelpFinderKey(msg tea.KeyMsg) tea.Cmd {
	s := a.helpFinderState
	if s == nil {
		return nil
	}

	switch msg.String() {
	case "esc":
		a.closeHelpFinder()
	case "enter":
		a.acceptHelpFinderSelection()
	case "up":
		if s.Selected > 0 {
			s.Selected--
		}
	case "down":
		if s.Selected < len(s.Results)-1 {
			s.Selected++
		}
	case "backspace":
		if len(s.Query) > 0 {
			r := []rune(s.Query)
			s.Query = string(r[:len(r)-1])
			a.refreshHelpFinderResults()
		}
	case "ctrl+u":
		if s.Query != "" {
			s.Query = ""
			a.refreshHelpFinderResults()
		}
	default:
		// Matches the existing search-input idiom used elsewhere in this
		// codebase (server_management_view.go's handleSearchInputKey) --
		// a single printable rune per keypress.
		if len(msg.String()) == 1 {
			s.Query += msg.String()
			a.refreshHelpFinderResults()
		}
	}
	return nil
}

// renderHelpFinderOverlay renders the /help finder as a centered overlay
// on top of the base view -- same compositing approach the old help modal
// used (renderHelpModalOverlay, now removed): a bordered box placed via
// lipgloss.Place, not a full-screen takeover.
func (a *App) renderHelpFinderOverlay(baseView string) string {
	s := a.helpFinderState
	if s == nil {
		return baseView
	}

	overlayWidth := 104
	if overlayWidth > a.width-4 {
		overlayWidth = a.width - 4
	}
	innerWidth := overlayWidth - 4 // border (2) + horizontal padding (2)

	headerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	total := len(visibleSlashCommands(a.currentUserRoleLevel()))
	header := headerStyle.Render("Help — Command Finder") + "  " +
		dimStyle.Render(fmt.Sprintf("(%d of %d commands)", len(s.Results), total))

	queryBoxStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color(a.theme.Semantic.InputFg)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).
		Width(innerWidth).
		Padding(0, 1)
	cursor := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow)).Render("▌")
	searchBox := queryBoxStyle.Render("/ " + s.Query + cursor)

	var rows []string
	if len(s.Results) == 0 {
		rows = append(rows, dimStyle.Render("  No matching commands"))
	} else {
		maxVisible := helpFinderVisibleRows
		if maxAllowed := a.height - 14; maxAllowed < maxVisible {
			maxVisible = maxAllowed
		}
		if maxVisible < 3 {
			maxVisible = 3
		}
		if s.Selected < s.ScrollOffset {
			s.ScrollOffset = s.Selected
		}
		if s.Selected >= s.ScrollOffset+maxVisible {
			s.ScrollOffset = s.Selected - maxVisible + 1
		}
		end := s.ScrollOffset + maxVisible
		if end > len(s.Results) {
			end = len(s.Results)
		}

		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Green)).Bold(true)
		usageStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
		descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground))
		selectedStyle := lipgloss.NewStyle().
			Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
			Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
			Bold(true).
			Width(innerWidth)

		for i := s.ScrollOffset; i < end; i++ {
			c := s.Results[i]
			usage := ""
			if c.Usage != "" {
				usage = " " + c.Usage
			}
			if i == s.Selected {
				line := fmt.Sprintf("▶ /%s%s  —  %s", c.Name, usage, c.Description)
				rows = append(rows, selectedStyle.Render(line))
			} else {
				line := "  " + nameStyle.Render("/"+c.Name) + usageStyle.Render(usage) +
					"  " + dimStyle.Render("—") + " " + descStyle.Render(c.Description)
				rows = append(rows, line)
			}
		}
	}

	hints := dimStyle.Italic(true).Render("↑↓ select · Enter use · Esc close")

	var body strings.Builder
	body.WriteString(header + "\n\n")
	body.WriteString(searchBox + "\n\n")
	body.WriteString(strings.Join(rows, "\n"))
	body.WriteString("\n\n" + hints)

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Width(overlayWidth).
		Padding(1).
		Background(lipgloss.Color(a.theme.Colors.Background))

	modal := boxStyle.Render(body.String())
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, modal,
		lipgloss.WithWhitespaceChars(""),
		lipgloss.WithWhitespaceForeground(lipgloss.Color(a.theme.Colors.Background)))
}
