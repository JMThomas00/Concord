package client

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/rivo/uniseg"

	"github.com/concord-chat/concord/internal/emoji"
)

// The emoji picker (Ctrl+E from the message box): search by name, keyword
// or :shortcode:, recently used first, Unicode's groups on tabs, a grid to
// move through with the arrow keys, and a skin tone that sticks. Enter puts
// the emoji in the message box at the cursor.

const (
	emojiRecentMax  = 30
	emojiCellWidth  = 4 // an emoji is two columns; two more between them
	emojiPickerRows = 7
)

// emojiTabs are the picker's tabs: Recent, then emoji.GroupNames.
var emojiTabIcons = []string{"🕘", "😀", "👋", "🐻", "🍔", "🚗", "⚽", "💡", "🔣", "🏁"}

type emojiPickerState struct {
	Query  string
	Tab    int // 0 recent, 1.. emoji.GroupNames[Tab-1]
	Cursor int
	Scroll int // first grid row shown
	Cols   int // grid columns at the last render, for the arrow keys
	// Where the box was last drawn (its top-left corner), for the mouse.
	Left, Top, Width, Height int
	items                    []*emoji.Emoji
}

// openEmojiPicker opens the picker on Recent, or on the first group if
// nothing's been used yet.
func (a *App) openEmojiPicker() {
	s := &emojiPickerState{Cols: 10}
	if len(a.recentEmoji()) == 0 {
		s.Tab = 1
	}
	a.emojiPicker = s
	a.refreshEmojiPicker()
}

func (a *App) recentEmoji() []string {
	if a.uiConfig == nil {
		return nil
	}
	return a.uiConfig.Display.EmojiRecent
}

func (a *App) emojiTone() int {
	if a.uiConfig == nil {
		return 0
	}
	return a.uiConfig.Display.EmojiTone
}

// refreshEmojiPicker recomputes what the grid shows: search results while
// there's a query, otherwise the tab's emoji.
func (a *App) refreshEmojiPicker() {
	s := a.emojiPicker
	if s == nil {
		return
	}
	switch {
	case strings.TrimSpace(s.Query) != "":
		s.items = emoji.Search(s.Query, 0)
	case s.Tab == 0:
		s.items = nil
		for _, c := range a.recentEmoji() {
			if e, ok := emoji.Lookup(c); ok {
				s.items = append(s.items, e)
			}
		}
	default:
		s.items = nil
		for _, e := range emoji.All() {
			if e.Group == s.Tab-1 {
				s.items = append(s.items, e)
			}
		}
	}
	s.Cursor = min(s.Cursor, max(0, len(s.items)-1))
}

func (a *App) handleEmojiPickerKey(msg tea.KeyMsg) tea.Cmd {
	s := a.emojiPicker
	cols := max(1, s.Cols)
	move := func(d int) {
		if len(s.items) > 0 {
			s.Cursor = min(max(0, s.Cursor+d), len(s.items)-1)
		}
	}
	switch key := msg.String(); key {
	case "esc", "ctrl+e":
		a.emojiPicker = nil
	case "enter":
		if s.Cursor < len(s.items) {
			a.insertEmoji(s.items[s.Cursor])
		}
	case "left":
		move(-1)
	case "right":
		move(1)
	case "up":
		move(-cols)
	case "down":
		move(cols)
	case "pgup":
		move(-cols * emojiPickerRows)
	case "pgdown":
		move(cols * emojiPickerRows)
	case "home":
		s.Cursor = 0
	case "end":
		move(len(s.items))
	case "tab", "shift+tab":
		d := 1
		if key == "shift+tab" {
			d = len(emojiTabIcons) - 1
		}
		s.Tab = (s.Tab + d) % len(emojiTabIcons)
		s.Query, s.Cursor, s.Scroll = "", 0, 0
		a.refreshEmojiPicker()
	case "ctrl+t": // skin tone: none, then light to dark
		if a.uiConfig != nil {
			a.uiConfig.Display.EmojiTone = (a.uiConfig.Display.EmojiTone + 1) % 6
			a.saveDisplayConfig()
		}
	case "backspace":
		if r := []rune(s.Query); len(r) > 0 {
			s.Query = string(r[:len(r)-1])
			s.Cursor, s.Scroll = 0, 0
			a.refreshEmojiPicker()
		}
	case "ctrl+u":
		s.Query, s.Cursor, s.Scroll = "", 0, 0
		a.refreshEmojiPicker()
	default:
		if msg.Type == tea.KeyRunes && !msg.Alt {
			s.Query += string(msg.Runes)
			s.Cursor, s.Scroll = 0, 0
			a.refreshEmojiPicker()
		}
	}
	return nil
}

// insertEmoji puts e (in the chosen skin tone) in the message box at the
// cursor, remembers it as recently used, and closes the picker.
func (a *App) insertEmoji(e *emoji.Emoji) {
	a.emojiPicker = nil
	a.input.InsertString(e.WithTone(a.emojiTone()))
	a.focus = FocusInput
	a.input.Focus()
	a.rememberEmoji(e)
}

// rememberEmoji moves e to the front of the recently used list.
func (a *App) rememberEmoji(e *emoji.Emoji) {
	if a.uiConfig == nil {
		return
	}
	recent := []string{e.Char}
	for _, c := range a.uiConfig.Display.EmojiRecent {
		if c != e.Char && len(recent) < emojiRecentMax {
			recent = append(recent, c)
		}
	}
	a.uiConfig.Display.EmojiRecent = recent
	a.saveDisplayConfig()
}

func (a *App) renderEmojiPickerOverlay(base string) string {
	box := a.emojiPickerBox()
	s := a.emojiPicker
	lines := strings.Split(box, "\n")
	s.Width, s.Height = 0, len(lines)
	for _, l := range lines {
		s.Width = max(s.Width, lipgloss.Width(l))
	}
	s.Left, s.Top = max(0, (a.width-s.Width)/2), max(0, (a.height-s.Height)/2) // as overlayPinned places it
	return overlayPinned(base, box, a.width, a.height)
}

// The picker's rows inside its border (emojiPickerBox's body): the tabs,
// then the group's label, then the grid.
const (
	emojiPickerTabsRow = 4
	emojiPickerGridRow = 6
)

// handleEmojiPickerMouse: click an emoji to put it in the message, a tab
// to change group; the wheel scrolls the grid; a click outside closes the
// picker. Worked out from the picker's own layout (zone marks would count
// each emoji's pre-painted cells, overlayPinned, and land in the wrong place).
func (a *App) handleEmojiPickerMouse(msg tea.MouseMsg) tea.Cmd {
	s := a.emojiPicker
	inside := msg.X >= s.Left && msg.X < s.Left+s.Width && msg.Y >= s.Top && msg.Y < s.Top+s.Height
	row, col := msg.Y-s.Top-1, msg.X-s.Left-2 // inside the border and padding
	switch {
	case msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown:
		if inside && len(s.items) > 0 {
			d := s.Cols
			if msg.Button == tea.MouseButtonWheelUp {
				d = -d
			}
			s.Cursor = min(max(0, s.Cursor+d), len(s.items)-1)
		}
	case msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft:
	case !inside:
		a.emojiPicker = nil
	case col < 0:
	case row == emojiPickerTabsRow:
		if t := col / emojiCellWidth; t < len(emojiTabIcons) {
			s.Tab, s.Query, s.Cursor, s.Scroll = t, "", 0, 0
			a.refreshEmojiPicker()
		}
	case row >= emojiPickerGridRow && row < emojiPickerGridRow+emojiPickerRows && col < s.Cols*emojiCellWidth:
		if i := (s.Scroll+row-emojiPickerGridRow)*s.Cols + col/emojiCellWidth; i < len(s.items) {
			a.insertEmoji(s.items[i])
		}
	}
	return nil
}

// emojiPickerBox draws the picker itself.
func (a *App) emojiPickerBox() string {
	s := a.emojiPicker
	width := min(66, a.width-4)
	inner := width - 4 // border and padding
	s.Cols = max(1, inner/emojiCellWidth)

	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Comment))
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Cyan)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Semantic.SidebarSelected)).
		Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Bold(true)

	tone := "none"
	if t := a.emojiTone(); t > 0 {
		hand, _ := emoji.Shortcode("wave")
		tone = hand.WithTone(t)
	}
	header := title.Render("Emoji") + strings.Repeat(" ", max(1, inner-5-lipgloss.Width("Skin tone: "+tone))) +
		dim.Render("Skin tone: ") + tone

	cursor := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Yellow)).Render("▌")
	query := s.Query
	if query == "" {
		query = dim.Render("search: name, keyword or :shortcode:")
		cursor = ""
	}
	search := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Semantic.InputFg)).
		Background(lipgloss.Color(a.theme.Semantic.InputBg)).Width(inner).Padding(0, 1).
		Render("🔍 " + query + cursor)

	var tabs []string
	for i, icon := range emojiTabIcons {
		cell := " " + icon + " "
		if i == s.Tab && strings.TrimSpace(s.Query) == "" {
			cell = sel.Render(cell)
		}
		tabs = append(tabs, cell)
	}
	label := "Recently used"
	switch {
	case strings.TrimSpace(s.Query) != "":
		label = fmt.Sprintf("%d found", len(s.items))
	case s.Tab > 0:
		label = emoji.GroupNames[s.Tab-1]
	}

	// The grid, scrolled to keep the cursor in view.
	row := s.Cursor / s.Cols
	if row < s.Scroll {
		s.Scroll = row
	}
	if row >= s.Scroll+emojiPickerRows {
		s.Scroll = row - emojiPickerRows + 1
	}
	var grid []string
	for r := s.Scroll; r < s.Scroll+emojiPickerRows; r++ {
		var line strings.Builder
		for c := 0; c < s.Cols; c++ {
			i := r*s.Cols + c
			if i >= len(s.items) {
				break
			}
			ch := s.items[i].WithTone(a.emojiTone())
			cell := " " + ch + strings.Repeat(" ", max(1, emojiCellWidth-1-lipgloss.Width(ch)))
			if i == s.Cursor {
				cell = sel.Render(cell)
			}
			line.WriteString(cell)
		}
		grid = append(grid, line.String())
	}
	if len(s.items) == 0 {
		empty := "Nothing yet: emoji you use show up here."
		if strings.TrimSpace(s.Query) != "" {
			empty = "No emoji match."
		}
		grid[0] = dim.Render(" " + empty)
	}

	info := ""
	if s.Cursor < len(s.items) {
		e := s.items[s.Cursor]
		info = e.WithTone(a.emojiTone()) + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Foreground)).Render(e.Name)
		if len(e.Shortcodes) > 0 {
			info += "  " + dim.Render(":"+e.Shortcodes[0]+":")
		}
		// One line, however long the name ("woman and man holding hands:
		// light skin tone, medium-dark skin tone"): wrapping moved the
		// whole picker up a row.
		info = ansi.Truncate(info, inner, "…")
	}
	hints := dim.Italic(true).Render("type to search · arrows move · Tab group · Ctrl+T skin tone · Enter insert · Esc close")

	body := strings.Join([]string{header, "", search, "", strings.Join(tabs, ""), dim.Render(label), strings.Join(grid, "\n"), "", info, "", hints}, "\n")
	box := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
		Background(lipgloss.Color(a.theme.Colors.Background)).
		Width(width).Padding(0, 1).Render(body)
	// Every coloured span ends in a reset that also drops the box's
	// background, leaving gaps of the terminal's own; put it back.
	return fillBoxBackground(box, a.theme.Colors.Background)
}

// fillBoxBackground is reassertBackgroundAfterResets for a box drawn over
// the screen: inside it, every reset is followed by the box's background
// again (each coloured span's reset dropped it, leaving gaps), but not the
// reset that ends a line, or the background would run on past the box's
// right edge into whatever's beside it.
func fillBoxBackground(box, bg string) string {
	open := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(bg)))
	if open == "" {
		return box
	}
	lines := strings.Split(box, "\n")
	for i, l := range lines {
		l = reassertBackgroundAfterResets(l, bg)
		for _, reset := range []string{ansiResetSeq, ansiResetSeqShort} {
			if strings.HasSuffix(l, reset+open) {
				l = strings.TrimSuffix(l, open)
			}
		}
		lines[i] = l
	}
	return strings.Join(lines, "\n")
}

// overlayPinned is overlayCenter for a box with emoji in it. Terminals
// don't agree with each other (or with us) on how wide some emoji are:
// skin-toned ones, ones joined from several (🐻‍❄️), ones with a
// presentation selector (☺️). A line the terminal draws wider than we
// measured runs past the edge, wraps, and pushes everything below it down
// a row (broken borders, the message box drawn twice). So the terminal is
// told where to carry on after each emoji (CHA, "cursor to column n"),
// and where the box starts and ends, so whatever width it gives an emoji
// the rest of the line lands where we laid it out.
func overlayPinned(bg, fg string, termW, termH int) string {
	fgLines := strings.Split(fg, "\n")
	fgW := 0
	for _, l := range fgLines {
		fgW = max(fgW, lipgloss.Width(l))
	}
	startX, startY := max(0, (termW-fgW)/2), max(0, (termH-len(fgLines))/2)
	bgLines := strings.Split(bg, "\n")
	for len(bgLines) < termH {
		bgLines = append(bgLines, strings.Repeat(" ", termW))
	}
	for y, l := range fgLines {
		by := startY + y
		if by >= len(bgLines) {
			break
		}
		line := bgLines[by]
		if w := lipgloss.Width(line); w < startX+fgW {
			line += strings.Repeat(" ", startX+fgW-w)
		}
		bgLines[by] = visualTake(line, startX) + cha(startX) + pinWide(l, startX) + cha(startX+fgW) + visualSkip(line, startX+fgW)
	}
	if len(bgLines) > termH {
		bgLines = bgLines[:termH]
	}
	return strings.Join(bgLines, "\n")
}

// cha moves the cursor to column col (0-based).
func cha(col int) string { return fmt.Sprintf("\x1b[%dG", col+1) }

// pinWide adds a cha after every wide or joined character in s, a line
// starting at column start: escape sequences pass through as they are.
func pinWide(s string, start int) string {
	var b strings.Builder
	col := start
	for s != "" {
		if s[0] == 0x1b { // an escape sequence: copy it whole
			n := 1
			if len(s) > 1 && s[1] == '[' {
				n = 2
				for n < len(s) && (s[n] < 0x40 || s[n] > 0x7e) {
					n++
				}
				n = min(n+1, len(s))
			}
			b.WriteString(s[:n])
			s = s[n:]
			continue
		}
		cluster, rest, w, _ := uniseg.FirstGraphemeClusterInString(s, -1)
		if w > 1 || utf8.RuneCountInString(cluster) > 1 {
			// Paint the emoji's cells first (in the current colours), so a
			// terminal that draws it narrower leaves no unpainted gap, then
			// draw it over them and carry on from where we measured.
			b.WriteString(strings.Repeat(" ", w) + cha(col) + cluster + cha(col+w))
		} else {
			b.WriteString(cluster)
		}
		col += w
		s = rest
	}
	return b.String()
}

// shortcodeBeforeCursor matches a :shortcode: just typed, at the start of
// the line or after a space ("see you :wave:", not "12:30:" or a URL).
var shortcodeBeforeCursor = regexp.MustCompile(`(?:^|\s):([a-z0-9_+\-]+):$`)

// expandEmojiShortcode turns a :shortcode: into its emoji as its closing
// colon is typed: ":grapes:" becomes 🍇, in the skin tone the picker uses.
// Only on a typed ":", so pasted text and editing keep what's written; an
// unknown code stays as typed.
func (a *App) expandEmojiShortcode(msg tea.Msg) {
	k, ok := msg.(tea.KeyMsg)
	if !ok || k.Type != tea.KeyRunes || string(k.Runes) != ":" || a.inPasteBurst {
		return
	}
	lines := strings.Split(a.input.Value(), "\n")
	row := a.input.Line()
	if row >= len(lines) {
		return
	}
	line := []rune(lines[row])
	li := a.input.LineInfo()
	col := min(li.StartColumn+li.ColumnOffset, len(line))
	m := shortcodeBeforeCursor.FindStringSubmatch(string(line[:col]))
	if m == nil {
		return
	}
	e, ok := emoji.Shortcode(m[1])
	if !ok {
		return
	}
	for range []rune(m[1]) {
		a.input, _ = a.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	}
	a.input, _ = a.input.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // the two colons
	a.input, _ = a.input.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	a.input.InsertString(e.WithTone(a.emojiTone()))
	a.rememberEmoji(e)
}

// editWholeEmoji makes Backspace, Delete and the arrow keys treat an emoji
// made of several characters as one, as people see it. The message box
// works a character at a time, so 🐻‍❄️ (bear, joiner, snowflake, selector)
// took four Backspaces, turning into other emoji on the way. Reports
// whether it handled the key.
func (a *App) editWholeEmoji(msg tea.Msg) bool {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return false
	}
	back := k.Type == tea.KeyBackspace || k.Type == tea.KeyLeft
	if !back && k.Type != tea.KeyDelete && k.Type != tea.KeyRight {
		return false
	}
	lines := strings.Split(a.input.Value(), "\n")
	row := a.input.Line()
	if row >= len(lines) {
		return false
	}
	li := a.input.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	// The characters of the cluster just before (or just after) the cursor.
	n, at := 0, 0
	for g := uniseg.NewGraphemes(lines[row]); g.Next(); {
		size := utf8.RuneCountInString(g.Str())
		if back && at+size == col || !back && at == col {
			n = size
			break
		}
		at += size
		if at > col {
			break
		}
	}
	if n < 2 {
		return false
	}
	for range n {
		a.input, _ = a.input.Update(k)
	}
	return true
}
