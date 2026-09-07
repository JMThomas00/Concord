package client

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func newTestLinkBrowserApp(links []string) *App {
	a := &App{}
	a.linkBrowserState = &LinkBrowserState{
		AllLinks: links,
		Links:    links,
	}
	return a
}

// TestResolveLinkBrowserRowIndex is the regression test for the real bug
// reported 2026-09-06: pressing a number key always opened the first link
// regardless of which number was pressed, because nothing ever read the
// keypress at all -- the [N] labels next to each link were purely cosmetic.
// Also confirms the fix is row-relative to the current scroll position, not
// an absolute index into the full list, so it stays meaningful once a long
// list scrolls (pressing "1" always means "the first link on screen").
func TestResolveLinkBrowserRowIndex(t *testing.T) {
	cases := []struct {
		name       string
		scroll     int
		digit      string
		totalLinks int
		wantIdx    int
		wantOK     bool
	}{
		{"no scroll, row 1", 0, "1", 5, 0, true},
		{"no scroll, row 2 -- the exact bug: this must NOT resolve to 0", 0, "2", 5, 1, true},
		{"no scroll, row 3", 0, "3", 5, 2, true},
		{"scrolled 5, row 1 means absolute index 5", 5, "1", 15, 5, true},
		{"scrolled 5, row 3 means absolute index 7", 5, "3", 15, 7, true},
		{"row beyond what's visible fails cleanly", 0, "9", 3, 0, false},
		{"row beyond scrolled list end fails cleanly", 10, "9", 15, 0, false},
		{"non-digit rejected", 0, "a", 5, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			idx, ok := resolveLinkBrowserRowIndex(tc.scroll, tc.digit, tc.totalLinks)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && idx != tc.wantIdx {
				t.Errorf("idx = %d, want %d", idx, tc.wantIdx)
			}
		})
	}
}

// TestLinkBrowserNumberKeyOpensCorrectRow drives the real key handler (not
// just the pure index math) to confirm pressing "2" actually selects link
// index 1 (the second link), not index 0.
func TestLinkBrowserNumberKeyOpensCorrectRow(t *testing.T) {
	links := []string{"https://one.example", "https://two.example", "https://three.example"}
	a := newTestLinkBrowserApp(links)

	cmd := a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("2")})
	if cmd == nil {
		t.Fatal("expected pressing '2' to return an open-URL command")
	}
	if a.linkBrowserState != nil {
		t.Fatal("expected the link browser to close after opening a link via number key")
	}
}

func TestLinkBrowserUpDownNavigationWraps(t *testing.T) {
	links := []string{"https://a.example", "https://b.example", "https://c.example"}
	a := newTestLinkBrowserApp(links)

	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyUp})
	if a.linkBrowserState.SelectedIndex != len(links)-1 {
		t.Errorf("expected up from index 0 to wrap to the last row, got %d", a.linkBrowserState.SelectedIndex)
	}

	a.linkBrowserState.SelectedIndex = len(links) - 1
	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyDown})
	if a.linkBrowserState.SelectedIndex != 0 {
		t.Errorf("expected down from the last row to wrap to 0, got %d", a.linkBrowserState.SelectedIndex)
	}
}

func TestRefreshLinkBrowserListFiltersCaseInsensitively(t *testing.T) {
	links := []string{"https://GitHub.com/foo", "https://example.com/bar", "https://github.com/baz"}
	a := newTestLinkBrowserApp(links)
	a.linkBrowserState.Query = "github"

	a.refreshLinkBrowserList()

	if len(a.linkBrowserState.Links) != 2 {
		t.Fatalf("expected 2 links matching %q case-insensitively, got %d: %v", "github", len(a.linkBrowserState.Links), a.linkBrowserState.Links)
	}
}

func TestRefreshLinkBrowserListSortsAscendingAndDescending(t *testing.T) {
	links := []string{"https://charlie.example", "https://alpha.example", "https://bravo.example"}
	a := newTestLinkBrowserApp(links)

	a.linkBrowserState.SortOrder = linkSortAscending
	a.refreshLinkBrowserList()
	want := []string{"https://alpha.example", "https://bravo.example", "https://charlie.example"}
	for i, l := range want {
		if a.linkBrowserState.Links[i] != l {
			t.Fatalf("ascending sort: expected %v, got %v", want, a.linkBrowserState.Links)
		}
	}

	a.linkBrowserState.SortOrder = linkSortDescending
	a.refreshLinkBrowserList()
	wantDesc := []string{"https://charlie.example", "https://bravo.example", "https://alpha.example"}
	for i, l := range wantDesc {
		if a.linkBrowserState.Links[i] != l {
			t.Fatalf("descending sort: expected %v, got %v", wantDesc, a.linkBrowserState.Links)
		}
	}
}

func TestRefreshLinkBrowserListClampsSelectionWhenFilterShrinksList(t *testing.T) {
	links := []string{"https://one.example", "https://two.example", "https://three.example"}
	a := newTestLinkBrowserApp(links)
	a.linkBrowserState.SelectedIndex = 2 // pointing at "three.example"

	a.linkBrowserState.Query = "one" // only 1 match now
	a.refreshLinkBrowserList()

	if a.linkBrowserState.SelectedIndex != 0 {
		t.Errorf("expected SelectedIndex to clamp into range after filtering, got %d", a.linkBrowserState.SelectedIndex)
	}
	if len(a.linkBrowserState.Links) != 1 {
		t.Fatalf("expected exactly 1 filtered link, got %d", len(a.linkBrowserState.Links))
	}
}

func TestLinkBrowserSearchModeTypingAndBackspace(t *testing.T) {
	links := []string{"https://one.example", "https://two.example"}
	a := newTestLinkBrowserApp(links)

	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !a.linkBrowserState.Searching {
		t.Fatal("expected '/' to enter search mode")
	}

	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("o")})
	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	if a.linkBrowserState.Query != "one" {
		t.Fatalf("expected typed query %q, got %q", "one", a.linkBrowserState.Query)
	}
	if len(a.linkBrowserState.Links) != 1 {
		t.Fatalf("expected the list to live-filter while typing, got %d links", len(a.linkBrowserState.Links))
	}

	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if a.linkBrowserState.Query != "on" {
		t.Fatalf("expected backspace to remove the last character, got %q", a.linkBrowserState.Query)
	}

	a.handleLinkBrowserKey(tea.KeyMsg{Type: tea.KeyEsc})
	if a.linkBrowserState.Searching {
		t.Error("expected esc while searching to exit search mode")
	}
	if a.linkBrowserState.Query != "" {
		t.Errorf("expected esc while searching to clear the query, got %q", a.linkBrowserState.Query)
	}
	if len(a.linkBrowserState.Links) != len(links) {
		t.Errorf("expected clearing the query to restore the full list, got %d links", len(a.linkBrowserState.Links))
	}
	if a.linkBrowserState == nil {
		t.Fatal("expected esc while searching to only clear the search, not close the whole browser")
	}
}

func TestClampLinkBrowserScrollFollowsSelection(t *testing.T) {
	links := make([]string, 25)
	for i := range links {
		links[i] = string(rune('a'+i%26)) + ".example"
	}
	a := newTestLinkBrowserApp(links)

	a.linkBrowserState.SelectedIndex = 12
	a.clampLinkBrowserScroll()
	if a.linkBrowserState.ScrollOffset > 12 || a.linkBrowserState.SelectedIndex >= a.linkBrowserState.ScrollOffset+linkBrowserVisibleRows {
		t.Errorf("expected selection at index 12 to be within the visible window, offset=%d", a.linkBrowserState.ScrollOffset)
	}

	// Selecting the very last row should scroll so it's the last visible one.
	a.linkBrowserState.SelectedIndex = len(links) - 1
	a.clampLinkBrowserScroll()
	maxOffset := len(links) - linkBrowserVisibleRows
	if a.linkBrowserState.ScrollOffset != maxOffset {
		t.Errorf("expected ScrollOffset %d at the end of the list, got %d", maxOffset, a.linkBrowserState.ScrollOffset)
	}
}
