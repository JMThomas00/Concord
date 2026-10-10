package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestFilterSlashCommandsEmptyQueryReturnsEverythingVisible confirms an
// empty query (the finder's initial "browse everything" state) returns
// every command the given role level can see, unranked.
func TestFilterSlashCommandsEmptyQueryReturnsEverythingVisible(t *testing.T) {
	got := filterSlashCommands("", roleLevelMember)
	want := visibleSlashCommands(roleLevelMember)
	if len(got) != len(want) {
		t.Fatalf("expected %d commands for an empty query, got %d", len(want), len(got))
	}
}

// TestFilterSlashCommandsRespectsRoleLevel confirms a member never sees
// admin-only commands (e.g. /ban), even with a query that would otherwise
// match.
func TestFilterSlashCommandsRespectsRoleLevel(t *testing.T) {
	results := filterSlashCommands("ban", roleLevelMember)
	for _, c := range results {
		if c.Name == "ban" {
			t.Errorf("expected /ban to be excluded for a plain member, but it appeared in results")
		}
	}

	adminResults := filterSlashCommands("ban", roleLevelAdmin)
	found := false
	for _, c := range adminResults {
		if c.Name == "ban" {
			found = true
		}
	}
	if !found {
		t.Error("expected /ban to appear for an admin querying \"ban\"")
	}
}

// TestFilterSlashCommandsPrefixRanksAboveSubstring is the core "fuzzy
// finder narrows by command or description" behavior: a query matching a
// command name's prefix should rank above one that only matches somewhere
// in the middle of another command's description.
func TestFilterSlashCommandsPrefixRanksAboveSubstring(t *testing.T) {
	// "mute" is a name-prefix match for /mute itself, and also appears
	// inside /unmute's name and /mute-voice's name -- prefix should win.
	results := filterSlashCommands("mute", roleLevelAdmin)
	if len(results) == 0 {
		t.Fatal("expected at least one match for \"mute\"")
	}
	if results[0].Name != "mute" {
		t.Errorf("expected /mute (exact prefix match) to rank first, got %q", results[0].Name)
	}
}

// TestFilterSlashCommandsMatchesByDescription confirms the "or
// description" half of the feature: a query with no command-name match at
// all, but a real word from a description, still finds that command.
func TestFilterSlashCommandsMatchesByDescription(t *testing.T) {
	// "ephemeral" appears only in /whisper's description, not its name.
	results := filterSlashCommands("ephemeral", roleLevelMember)
	if len(results) != 1 || results[0].Name != "whisper" {
		t.Fatalf("expected exactly /whisper to match \"ephemeral\" via its description, got %+v", results)
	}
}

// TestFilterSlashCommandsFuzzySubsequenceFallback confirms the actual
// "fuzzy" part: a query whose letters appear in order but not contiguously
// in a command name still matches, when nothing matched as a literal
// substring.
func TestFilterSlashCommandsFuzzySubsequenceFallback(t *testing.T) {
	// "wsp" is a subsequence of "whisper" (w..s..p) but not a substring of
	// any command name or description.
	results := filterSlashCommands("wsp", roleLevelMember)
	found := false
	for _, c := range results {
		if c.Name == "whisper" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected /whisper to match fuzzy subsequence \"wsp\", got %+v", results)
	}
}

// TestFuzzySubsequenceIndex covers the underlying matcher directly.
func TestFuzzySubsequenceIndex(t *testing.T) {
	cases := []struct {
		s, query string
		wantOK   bool
	}{
		{"whisper", "wsp", true},
		{"whisper", "whisper", true},
		{"whisper", "", true},
		{"whisper", "xyz", false},
		{"whisper", "prw", false}, // right letters, wrong order
	}
	for _, c := range cases {
		_, ok := fuzzySubsequenceIndex(c.s, c.query)
		if ok != c.wantOK {
			t.Errorf("fuzzySubsequenceIndex(%q, %q) ok = %v, want %v", c.s, c.query, ok, c.wantOK)
		}
	}
}

// TestOpenHelpFinderPrefillsQuery confirms "/help <query>" (args joined
// back together) pre-filters the finder instead of always opening empty.
func TestOpenHelpFinderPrefillsQuery(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("mute")

	if a.helpFinderState == nil {
		t.Fatal("expected helpFinderState to be set after openHelpFinder")
	}
	if a.helpFinderState.Query != "mute" {
		t.Errorf("Query = %q, want %q", a.helpFinderState.Query, "mute")
	}
	if len(a.helpFinderState.Results) == 0 {
		t.Error("expected at least one result for query \"mute\"")
	}
}

// TestHandleHelpFinderKeyTypingNarrowsResults drives the finder the way a
// real keystroke would (handleHelpFinderKey's default branch), confirming
// typing actually narrows Results, matching the real UI path rather than
// calling refreshHelpFinderResults directly.
func TestHandleHelpFinderKeyTypingNarrowsResults(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("")
	fullCount := len(a.helpFinderState.Results)

	for _, ch := range "whisper" {
		a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{ch}})
	}

	if a.helpFinderState.Query != "whisper" {
		t.Fatalf("Query = %q, want %q", a.helpFinderState.Query, "whisper")
	}
	if len(a.helpFinderState.Results) >= fullCount {
		t.Errorf("expected typing to narrow results below the full list (%d), got %d", fullCount, len(a.helpFinderState.Results))
	}
	if len(a.helpFinderState.Results) == 0 || a.helpFinderState.Results[0].Name != "whisper" {
		t.Errorf("expected /whisper to be the top result, got %+v", a.helpFinderState.Results)
	}
}

// TestHandleHelpFinderKeyBackspaceWidensResults confirms backspace removes
// a character and re-broadens the result set, not just clears visually.
func TestHandleHelpFinderKeyBackspaceWidensResults(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("whisper")
	narrowCount := len(a.helpFinderState.Results)

	for range "whisper" {
		a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyBackspace})
	}

	if a.helpFinderState.Query != "" {
		t.Fatalf("expected an empty query after backspacing the whole thing, got %q", a.helpFinderState.Query)
	}
	if len(a.helpFinderState.Results) <= narrowCount {
		t.Errorf("expected clearing the query to widen results beyond %d, got %d", narrowCount, len(a.helpFinderState.Results))
	}
}

// TestHandleHelpFinderKeyArrowsMoveSelection confirms up/down move the
// Selected index within bounds.
func TestHandleHelpFinderKeyArrowsMoveSelection(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("")
	if len(a.helpFinderState.Results) < 3 {
		t.Fatal("test setup bug: expected at least 3 visible commands")
	}

	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyDown})
	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyDown})
	if a.helpFinderState.Selected != 2 {
		t.Errorf("Selected = %d, want 2 after two down-presses", a.helpFinderState.Selected)
	}

	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyUp})
	if a.helpFinderState.Selected != 1 {
		t.Errorf("Selected = %d, want 1 after one up-press", a.helpFinderState.Selected)
	}

	// Can't go below 0.
	for i := 0; i < 5; i++ {
		a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyUp})
	}
	if a.helpFinderState.Selected != 0 {
		t.Errorf("Selected = %d, want 0 (clamped)", a.helpFinderState.Selected)
	}

	// Can't go past the last result.
	last := len(a.helpFinderState.Results) - 1
	for i := 0; i < len(a.helpFinderState.Results)+3; i++ {
		a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyDown})
	}
	if a.helpFinderState.Selected != last {
		t.Errorf("Selected = %d, want %d (clamped to last result)", a.helpFinderState.Selected, last)
	}
}

// TestAcceptHelpFinderSelectionPopulatesInputAndCloses is the end-to-end
// "hit enter to have it automatically populate in the message box"
// behavior the feature was asked for.
func TestAcceptHelpFinderSelectionPopulatesInputAndCloses(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("whisper")
	if a.helpFinderState.Results[a.helpFinderState.Selected].Name != "whisper" {
		t.Fatalf("test setup bug: expected /whisper to be selected, got %+v", a.helpFinderState.Results)
	}

	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyEnter})

	if a.helpFinderState != nil {
		t.Error("expected the finder to close after accepting a selection")
	}
	if got := a.input.Value(); got != "/whisper " {
		t.Errorf("input value = %q, want %q", got, "/whisper ")
	}
	if a.focus != FocusInput {
		t.Errorf("expected focus to move to FocusInput, got %v", a.focus)
	}
}

// TestHandleHelpFinderKeyEscClosesWithoutChangingInput confirms Esc backs
// out without touching whatever the user had already typed.
func TestHandleHelpFinderKeyEscClosesWithoutChangingInput(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.input.SetValue("some draft the user was typing")
	a.openHelpFinder("whisper")

	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyEsc})

	if a.helpFinderState != nil {
		t.Error("expected the finder to close on Esc")
	}
	if got := a.input.Value(); got != "some draft the user was typing" {
		t.Errorf("expected Esc to leave the input untouched, got %q", got)
	}
}

// TestAcceptHelpFinderSelectionNoopWhenNoResults guards against a panic
// (index out of range) if Enter is pressed with an empty result set.
func TestAcceptHelpFinderSelectionNoopWhenNoResults(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	a.openHelpFinder("zzz-nonexistent-command-zzz")
	if len(a.helpFinderState.Results) != 0 {
		t.Fatalf("test setup bug: expected zero results, got %d", len(a.helpFinderState.Results))
	}

	a.handleHelpFinderKey(tea.KeyMsg{Type: tea.KeyEnter}) // must not panic

	if a.helpFinderState == nil {
		t.Error("expected the finder to stay open when there's nothing to accept")
	}
}

// TestHandleHelpOpensFinder confirms the /help command itself (via
// CommandHandler, mirroring how a real user invocation reaches it) opens
// the finder rather than returning modal text -- matches /theme's
// no-args "open the interactive browser" shape.
func TestHandleHelpOpensFinder(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	ch := &CommandHandler{app: a}

	out, err := ch.handleHelp(nil)
	if err != nil {
		t.Fatalf("handleHelp: %v", err)
	}
	if out != "" {
		t.Errorf("expected handleHelp to return an empty string (side-effecting, like handleTheme), got %q", out)
	}
	if a.helpFinderState == nil {
		t.Fatal("expected handleHelp to open the finder")
	}
}

// TestHandleHelpJoinsArgsIntoInitialQuery confirms "/help mute" arrives as
// args ["mute"] and becomes the finder's pre-filled query.
func TestHandleHelpJoinsArgsIntoInitialQuery(t *testing.T) {
	a := newLayoutTestApp(t, 120, 40)
	ch := &CommandHandler{app: a}

	if _, err := ch.handleHelp([]string{"mute"}); err != nil {
		t.Fatalf("handleHelp: %v", err)
	}
	if a.helpFinderState.Query != "mute" {
		t.Errorf("Query = %q, want %q", a.helpFinderState.Query, "mute")
	}
}

// TestRenderHelpFinderOverlayDoesNotPanic is a smoke test across a few
// states (empty query, a query with no matches, a real query) confirming
// the render path doesn't panic regardless of result-set shape.
func TestRenderHelpFinderOverlayDoesNotPanic(t *testing.T) {
	for _, query := range []string{"", "whisper", "zzz-no-match-zzz"} {
		a := newLayoutTestApp(t, 100, 30)
		a.openHelpFinder(query)
		out := a.renderHelpFinderOverlay("base view")
		if !strings.Contains(out, "Command Finder") {
			t.Errorf("query %q: expected the overlay header in output", query)
		}
	}
}
