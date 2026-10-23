package client

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
	"github.com/rivo/uniseg"

	"github.com/concord-chat/concord/internal/models"
)

// newEmojiTestApp is a main view with the message box focused, whose
// settings go to a temporary folder (not the real ~/.concord).
func newEmojiTestApp(t *testing.T) *App {
	t.Helper()
	a := newLayoutTestApp(t, 120, 40)
	dir := t.TempDir()
	a.configMgr = &ConfigManager{
		serversFilePath:     filepath.Join(dir, "servers.json"),
		configFilePath:      filepath.Join(dir, "config.json"),
		sharedFilesFilePath: filepath.Join(dir, "shared_files.json"),
	}
	a.uiConfig = &UIConfig{}
	a.view = ViewMain
	a.focus = FocusInput
	a.input = newTestMessageInput()
	a.input.Focus()
	a.currentChannel = &models.Channel{ID: uuid.New(), Name: "general", Type: models.ChannelTypeText}
	return a
}

func pickerKeys(a *App, keys ...string) {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "ctrl+e":
			msg = tea.KeyMsg{Type: tea.KeyCtrlE}
		case "ctrl+t":
			msg = tea.KeyMsg{Type: tea.KeyCtrlT}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "tab":
			msg = tea.KeyMsg{Type: tea.KeyTab}
		case "right":
			msg = tea.KeyMsg{Type: tea.KeyRight}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "backspace":
			msg = tea.KeyMsg{Type: tea.KeyBackspace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		a.Update(msg)
	}
}

// Ctrl+E, a search, Enter: the emoji goes in the message box (and the
// message isn't sent), and it's first in Recently used next time.
func TestThePickerPutsAnEmojiInTheMessage(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "h", "i", " ")
	pickerKeys(a, "ctrl+e")
	if a.emojiPicker == nil {
		t.Fatal("Ctrl+E didn't open the picker")
	}
	if a.emojiPicker.Tab != 1 {
		t.Fatalf("with nothing used yet it should open on Smileys, not tab %d", a.emojiPicker.Tab)
	}
	pickerKeys(a, "t", "h", "u", "m", "b", "s")
	if got := a.input.Value(); got != "hi " {
		t.Fatalf("typing a search leaked into the message: %q", got)
	}
	pickerKeys(a, "enter")
	if a.emojiPicker != nil {
		t.Fatal("Enter didn't close the picker")
	}
	if got := a.input.Value(); got != "hi 👍" {
		t.Fatalf("the message box has %q", got)
	}
	if r := a.uiConfig.Display.EmojiRecent; len(r) != 1 || r[0] != "👍" {
		t.Fatalf("recently used: %v", r)
	}
	cfg, err := a.configMgr.LoadAppConfig()
	if err != nil || cfg == nil || len(cfg.UI.Display.EmojiRecent) != 1 {
		t.Fatalf("recently used wasn't saved: %+v %v", cfg, err)
	}

	pickerKeys(a, "ctrl+e")
	if a.emojiPicker.Tab != 0 || len(a.emojiPicker.items) != 1 || a.emojiPicker.items[0].Char != "👍" {
		t.Fatalf("the picker should open on Recently used with 👍: tab %d", a.emojiPicker.Tab)
	}
	pickerKeys(a, "esc")
	if a.emojiPicker != nil || a.input.Value() != "hi 👍" {
		t.Fatalf("Esc should close without inserting: %q", a.input.Value())
	}
}

// The grid moves with the arrows, Tab changes group, and the skin tone
// sticks and applies.
func TestThePickerGridTabsAndSkinTone(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e")
	a.View() // the grid's width comes from the render
	first := a.emojiPicker.items[0].Char
	pickerKeys(a, "right", "down")
	if a.emojiPicker.Cursor != 1+a.emojiPicker.Cols {
		t.Fatalf("cursor at %d after right, down (cols %d)", a.emojiPicker.Cursor, a.emojiPicker.Cols)
	}
	pickerKeys(a, "tab")
	if a.emojiPicker.Tab != 2 || a.emojiPicker.Cursor != 0 || a.emojiPicker.items[0].Char == first {
		t.Fatalf("Tab: tab %d cursor %d", a.emojiPicker.Tab, a.emojiPicker.Cursor)
	}
	pickerKeys(a, "ctrl+t", "ctrl+t", "ctrl+t") // medium
	pickerKeys(a, "w", "a", "v", "e", "enter")
	if got := a.input.Value(); got != "👋🏽" {
		t.Fatalf("with a medium skin tone, got %q", got)
	}
	if a.uiConfig.Display.EmojiTone != 3 {
		t.Fatalf("tone %d", a.uiConfig.Display.EmojiTone)
	}
}

// The picker draws inside the window, over the chat, and says what's
// highlighted.
func TestThePickerDraws(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e", "g", "r", "a", "p", "e")
	view := ansi.Strip(a.View())
	for _, want := range []string{"Emoji", "grape", "🍇", ":grapes:"} {
		if !strings.Contains(view, want) {
			t.Fatalf("the picker doesn't show %q:\n%s", want, view)
		}
	}
	lines := strings.Split(view, "\n")
	if len(lines) > a.height {
		t.Fatalf("%d lines in a %d-line window", len(lines), a.height)
	}
	// (Line widths, which need the cursor moves followed, are
	// TestThePickerLinesUpWhateverWidthTheTerminalGivesEmoji's.)
	pickerKeys(a, "backspace", "backspace", "backspace", "backspace", "backspace", "z", "z", "q", "x")
	if !strings.Contains(ansi.Strip(a.View()), "No emoji match") {
		t.Fatal("a search with no results should say so")
	}
}

// A :shortcode: becomes its emoji as its closing colon is typed, anywhere
// in the message; times, URLs and unknown codes stay as typed.
func TestShortcodesBecomeEmojiAsTheyAreTyped(t *testing.T) {
	for typed, want := range map[string]string{
		"see you :wave:":      "see you 👋",
		":grapes:":            "🍇",
		"nice :+1: :tada: ok": "nice 👍 🎉 ok",
		"at 12:30:":           "at 12:30:",
		"http://x:80:":        "http://x:80:",
		"a :notanemoji: b":    "a :notanemoji: b",
		"no:space:":           "no:space:",
		"🐻‍❄️ and 🍇 :wave:":   "🐻‍❄️ and 🍇 👋", // emoji before it on the line
	} {
		a := newEmojiTestApp(t)
		for _, r := range typed {
			typeSlowly(a, string(r))
		}
		if got := a.input.Value(); got != want {
			t.Errorf("typed %q, got %q, want %q", typed, got, want)
		}
	}

	// In the middle of a message: the cursor moved back, then typed there.
	a := newEmojiTestApp(t)
	for _, r := range "hot  today" {
		typeSlowly(a, string(r))
	}
	for range " today" {
		a.Update(tea.KeyMsg{Type: tea.KeyLeft})
	}
	for _, r := range ":fire:" {
		typeSlowly(a, string(r))
	}
	if got := a.input.Value(); got != "hot 🔥 today" {
		t.Fatalf("mid-message: %q", got)
	}
}

// typeSlowly types a key the way a person does: back-to-back keys in a
// test look like a paste (lastKeystrokeAt), which shortcodes leave alone.
func typeSlowly(a *App, key string) {
	a.lastKeystrokeAt = time.Now().Add(-time.Second)
	a.inPasteBurst = false
	pickerKeys(a, key)
}

// A terminal that disagrees with us about emoji widths (Windows Terminal
// drew some wider than measured) used to get a picker whose lines ran past
// the edge: borders broke and rows doubled. With the cursor told where to
// go after each emoji, every row's right border lands in one column, and
// nothing runs past the window, however wide the terminal draws them.
func TestThePickerLinesUpWhateverWidthTheTerminalGivesEmoji(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e", "tab", "ctrl+t", "ctrl+t") // People & Body, skin-toned
	pickerKeys(a, "b", "e", "a", "r")                  // 🐻‍❄️ and friends
	// Draw the frame the way a disagreeing terminal would: a joined or
	// toned emoji takes two columns per character in it.
	wide := func(cluster string) int {
		if n := utf8.RuneCountInString(cluster); n > 1 {
			return 2 * n
		}
		return ansi.StringWidth(cluster)
	}
	rightBorder := -1
	for i, line := range strings.Split(a.View(), "\n") {
		col, maxCol, border := 0, 0, -1
		for line != "" {
			if line[0] == 0x1b {
				n := 2
				for n < len(line) && (line[n] < 0x40 || line[n] > 0x7e) {
					n++
				}
				seq := line[:n+1]
				if strings.HasSuffix(seq, "G") { // cursor to column
					fmt.Sscanf(seq[2:], "%d", &col)
					col--
				}
				line = line[n+1:]
				continue
			}
			cluster, rest, _, _ := uniseg.FirstGraphemeClusterInString(line, -1)
			if cluster == "│" || cluster == "╮" || cluster == "╯" {
				border = col
			}
			col += wide(cluster)
			maxCol = max(maxCol, col)
			line = rest
		}
		if maxCol > a.width {
			t.Fatalf("line %d runs to column %d in a %d-wide window", i, maxCol, a.width)
		}
		if border > 60 { // the picker's right border (the chat's are further left or right)
			if rightBorder >= 0 && border != rightBorder && border < 100 {
				t.Fatalf("line %d: right border at %d, others at %d", i, border, rightBorder)
			}
			if border < 100 {
				rightBorder = border
			}
		}
	}
}

// 🐻‍❄️ took four Backspaces, turning into other emoji on the way: it's
// four characters. Backspace, Delete and the arrows now take an emoji
// whole, and plain text still goes a character at a time.
func TestTheMessageBoxEditsWholeEmoji(t *testing.T) {
	for _, e := range []string{"🐻‍❄️", "👨‍👩‍👧", "👍🏽", "🇺🇸", "❤️", "😮‍💨"} {
		a := newEmojiTestApp(t)
		a.input.InsertString("a" + e + "b")
		a.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // the b
		a.Update(tea.KeyMsg{Type: tea.KeyBackspace}) // the emoji, in one
		if got := a.input.Value(); got != "a" {
			t.Errorf("%s: after two Backspaces the box has %q", e, got)
		}

		a = newEmojiTestApp(t)
		a.input.InsertString("a" + e + "b")
		a.Update(tea.KeyMsg{Type: tea.KeyLeft}) // before the b
		a.Update(tea.KeyMsg{Type: tea.KeyLeft}) // before the emoji, in one
		a.Update(tea.KeyMsg{Type: tea.KeyDelete})
		if got := a.input.Value(); got != "ab" {
			t.Errorf("%s: Left, Left, Delete left %q", e, got)
		}
		a.Update(tea.KeyMsg{Type: tea.KeyRight})
		a.Update(tea.KeyMsg{Type: tea.KeyBackspace})
		if got := a.input.Value(); got != "a" {
			t.Errorf("%s: plain text should still go a character at a time: %q", e, got)
		}
	}
}

// screenOf draws view on a model terminal pre-filled with "X" (what was on
// screen before), drawing each wide or joined emoji cellsFor(cluster)
// wide, following cursor-to-column moves. Returns the screen's rows.
func screenOf(view string, width int, cellsFor func(string) int) [][]string {
	var rows [][]string
	for _, line := range strings.Split(view, "\n") {
		row := make([]string, width+8)
		for i := range row {
			row[i] = "X"
		}
		col := 0
		for line != "" {
			if line[0] == 0x1b {
				n := 2
				for n < len(line) && (line[n] < 0x40 || line[n] > 0x7e) {
					n++
				}
				if seq := line[:n+1]; strings.HasSuffix(seq, "G") {
					fmt.Sscanf(seq[2:], "%d", &col)
					col--
				} else if strings.HasSuffix(seq, "X") { // erase n cells; the cursor stays
					var k int
					fmt.Sscanf(seq[2:], "%d", &k)
					for i := col; i < col+k && i < len(row); i++ {
						row[i] = " "
					}
				}
				line = line[n+1:]
				continue
			}
			cluster, rest, w, _ := uniseg.FirstGraphemeClusterInString(line, -1)
			if w > 1 || utf8.RuneCountInString(cluster) > 1 {
				w = cellsFor(cluster)
			}
			for i := 0; i < w && col+i < len(row); i++ {
				row[col+i] = ""
			}
			if col < len(row) {
				row[col] = cluster
			}
			col += w
			line = rest
		}
		rows = append(rows, row)
	}
	return rows
}

// A terminal that draws emoji narrower than measured used to be left
// with unpainted cells (gaps of whatever was there before, or of the
// terminal's own background) after them.
func TestThePickerLeavesNoUnpaintedCells(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e", "tab", "ctrl+t", "ctrl+t", "m", "a", "n")
	a.View()
	boxW := min(66, a.width-4) + 2
	left := (a.width - boxW) / 2
	narrow := func(string) int { return 1 }
	for y, row := range screenOf(a.View(), a.width, narrow) {
		if !strings.Contains(strings.Join(row[left:left+boxW], ""), "Emoji") && y == 0 {
			continue
		}
		for x := left; x < left+boxW; x++ {
			if row[x] == "X" {
				t.Fatalf("row %d, column %d inside the picker was never painted", y, x)
			}
		}
	}
}

// Every coloured span's reset used to drop the picker's background too,
// leaving gaps; it's put back after each.
func TestThePickerKeepsItsBackground(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e", "g", "r", "a", "p", "e")
	bg := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Colors.Background)))
	if bg == "" {
		t.Skip("no colour in this test environment")
	}
	box := a.emojiPickerBox()
	resets := 0
	for _, reset := range []string{ansiResetSeq, ansiResetSeqShort} {
		for i := strings.Index(box, reset); i >= 0; {
			resets++
			after := box[i+len(reset):]
			// (A line's last reset is the box's right edge: nothing after it.)
			if after != "" && after[0] != '\n' && !strings.HasPrefix(after, bg) {
				t.Fatalf("a reset isn't followed by the background: %q", box[i:min(len(box), i+40)])
			}
			next := strings.Index(box[i+len(reset):], reset)
			if next < 0 {
				break
			}
			i += len(reset) + next
		}
	}
	if resets == 0 {
		t.Fatal("no resets found: the check didn't check anything")
	}
}

// A long name stays on one line, so the picker doesn't shift up a row.
func TestALongEmojiNameKeepsThePickerStill(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e", "g", "r", "a", "p", "e", "s")
	short := len(strings.Split(ansi.Strip(a.renderEmojiPickerOverlay("")), "\n"))
	a.emojiPicker = nil
	pickerKeys(a, "ctrl+e")
	for _, r := range "woman and man holding hands light medium-dark" {
		pickerKeys(a, string(r))
	}
	view := ansi.Strip(a.renderEmojiPickerOverlay(""))
	if long := len(strings.Split(view, "\n")); long != short {
		t.Fatalf("the picker is %d lines with a long name, %d with a short one:\n%s", long, short, view)
	}
	if !strings.Contains(view, "…") {
		t.Fatalf("the long name wasn't cut short:\n%s", view)
	}
}

// The Help finder (/help) had the same gaps along every line.
func TestTheHelpFinderKeepsItsBackground(t *testing.T) {
	a := newEmojiTestApp(t)
	a.openHelpFinder("the")
	bg := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Colors.Background)))
	if bg == "" {
		t.Skip("no colour in this test environment")
	}
	n := 0
	for _, line := range strings.Split(a.renderHelpFinderOverlay(""), "\n") {
		i := strings.IndexAny(line, "│╭╰") // the box, not the screen's padding around it
		if i < 0 {
			continue
		}
		line = line[i:strings.LastIndexAny(line, "│╮╯")] // inside the box
		for _, reset := range []string{ansiResetSeq, ansiResetSeqShort} {
			parts := strings.Split(line, reset)
			for _, p := range parts[1:] {
				n++
				if !strings.HasPrefix(p, bg) {
					t.Fatalf("a reset isn't followed by the background: %q", p[:min(len(p), 40)])
				}
			}
		}
	}
	if n == 0 {
		t.Fatal("no resets found: the check didn't check anything")
	}
}

// The background put back after each reset ran on past the boxes' right
// edges, painting a block beside the Help finder. It stops at the edge,
// and long rows (/attach's) stay on one line.
func TestBoxBackgroundsStopAtTheirEdge(t *testing.T) {
	a := newEmojiTestApp(t)
	bg := ansiOpenCode(lipgloss.NewStyle().Background(lipgloss.Color(a.theme.Colors.Background)))
	if bg == "" {
		t.Skip("no colour in this test environment")
	}
	a.openHelpFinder("")
	help := a.renderHelpFinderOverlay("")
	a.helpFinderState = nil
	pickerKeys(a, "ctrl+e")
	for name, out := range map[string]string{"help finder": help, "emoji picker": a.emojiPickerBox()} {
		for i, line := range strings.Split(out, "\n") {
			edge := strings.LastIndexAny(line, "│╮╯")
			if edge < 0 {
				continue
			}
			if strings.Contains(line[edge:], bg) {
				t.Fatalf("%s line %d: the background goes on past the right edge: %q", name, i, line[edge:])
			}
		}
	}
	for _, line := range strings.Split(ansi.Strip(help), "\n") {
		if strings.HasPrefix(strings.TrimLeft(line, " │"), "download it)") {
			t.Fatalf("/attach's description wrapped:\n%s", ansi.Strip(help))
		}
	}
	if !strings.Contains(ansi.Strip(help), "/attach") {
		t.Fatal("the test expects /attach in the list")
	}
}

// where finds cluster on the drawn screen (the first from the top), as
// the model terminal draws it.
func where(t *testing.T, a *App, cluster string) (x, y int) {
	t.Helper()
	for y, row := range screenOf(a.View(), a.width, ansi.StringWidth) {
		for x, c := range row {
			if c == cluster {
				return x, y
			}
		}
	}
	t.Fatalf("%s isn't on the screen", cluster)
	return 0, 0
}

func click(a *App, x, y int) {
	a.Update(tea.MouseMsg{X: x, Y: y, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
}

// The mouse: click an emoji to put it in the message, a tab to change
// group, the wheel to scroll; a click outside closes the picker.
func TestThePickerWorksWithTheMouse(t *testing.T) {
	a := newEmojiTestApp(t)
	pickerKeys(a, "ctrl+e")
	x, y := where(t, a, "🍔") // the Food & Drink tab
	click(a, x+1, y)         // (its right half)
	if a.emojiPicker == nil || a.emojiPicker.Tab != 4 {
		t.Fatalf("clicking the 🍔 tab: %+v", a.emojiPicker)
	}

	pickerKeys(a, "g", "r", "a", "p", "e", "s")
	a.View()
	before := a.emojiPicker.Cursor
	a.Update(tea.MouseMsg{X: x, Y: y + 3, Action: tea.MouseActionPress, Button: tea.MouseButtonWheelDown})
	if a.emojiPicker.Cursor == before && len(a.emojiPicker.items) > a.emojiPicker.Cols {
		t.Fatal("the wheel didn't move through the grid")
	}

	pickerKeys(a, "ctrl+u", "g", "r", "a", "p", "e", "s")
	x, y = where(t, a, "🍇")
	click(a, x, y)
	if a.emojiPicker != nil || a.input.Value() != "🍇" {
		t.Fatalf("clicking 🍇: picker open %v, message %q", a.emojiPicker != nil, a.input.Value())
	}

	pickerKeys(a, "ctrl+e")
	a.View()
	click(a, 0, 0)
	if a.emojiPicker != nil {
		t.Fatal("a click outside didn't close the picker")
	}
	if a.input.Value() != "🍇" {
		t.Fatalf("a click outside changed the message: %q", a.input.Value())
	}
}

// withEmojiMessages puts a few messages full of emoji in the test app's
// channel.
func withEmojiMessages(t *testing.T, a *App) {
	t.Helper()
	now := time.Now()
	var msgs []*MessageDisplay
	for i, text := range []string{
		"polar bears 🐻‍❄️🐻‍❄️🐻‍❄️ and a family 👨‍👩‍👧 👨‍👩‍👧",
		"thumbs 👍🏽👍🏿 hearts ❤️☺️ flags 🇺🇸🇯🇵 sighs 😮‍💨😮‍💨",
		"plain text for comparison",
	} {
		msgs = append(msgs, &MessageDisplay{
			Message:    &models.Message{ID: uuid.New(), ChannelID: a.currentChannel.ID, AuthorID: uuid.New(), Content: text, CreatedAt: now.Add(time.Duration(i) * time.Minute)},
			AuthorName: "grape", ShowHeader: i == 0,
		})
	}
	if a.styles == nil {
		a.styles = a.theme.BuildStyles()
	}
	if a.activeConn == nil {
		a.activeConn = &ServerConnection{}
	}
	a.activeConn.Messages = map[uuid.UUID][]*MessageDisplay{a.currentChannel.ID: msgs}
	a.View() // sizes the chat, which the messages wrap to
	a.updateChatContent()
	if !strings.Contains(ansi.Strip(a.View()), "plain text for comparison") {
		t.Fatal("the test messages aren't on screen")
	}
}

// Bubbletea cuts any line it measures wider than the window. Filling an
// emoji's cells with spaces before drawing it made picker rows measure 150
// in a 120-wide window (their right ends were cut off); no line of a frame
// may measure wider than the window, emoji in the chat or the picker.
func TestNoLineMeasuresWiderThanTheWindow(t *testing.T) {
	a := newEmojiTestApp(t)
	withEmojiMessages(t, a)
	check := func(when string) {
		for i, l := range strings.Split(a.View(), "\n") {
			if w := ansi.StringWidth(l); w > a.width {
				t.Fatalf("%s: line %d measures %d in a %d-wide window", when, i, w, a.width)
			}
		}
	}
	check("chat with emoji")
	pickerKeys(a, "ctrl+e", "tab", "ctrl+t", "ctrl+t")
	check("the picker on People, skin-toned")
}

// The whole screen is pinned: messages full of joined and toned emoji stay
// inside the window on a terminal that draws them wider than measured.
func TestEmojiInMessagesCantPushLinesOut(t *testing.T) {
	a := newEmojiTestApp(t)
	withEmojiMessages(t, a)
	wide := func(cluster string) int { return 2 * utf8.RuneCountInString(cluster) }
	for y, row := range screenOf(a.View(), a.width, wide) {
		last := -1
		for x, c := range row {
			if c != "X" && c != "" {
				last = x
			}
		}
		if last >= a.width {
			t.Fatalf("row %d runs to column %d in a %d-wide window", y, last, a.width)
		}
	}
}
