package client

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

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
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > a.width {
			t.Fatalf("line %d is %d wide in a %d-wide window", i, w, a.width)
		}
	}
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
