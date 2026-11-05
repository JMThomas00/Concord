package client

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func hitFor(ch uuid.UUID, text, snippet string, score float64, at time.Time) protocol.SearchHit {
	return protocol.SearchHit{
		Message: &protocol.MessageDisplay{Message: &models.Message{ID: uuid.New(), ChannelID: ch, Content: text, CreatedAt: at}, Author: &models.User{ID: uuid.New(), Username: "juniper"}},
		Snippet: snippet, Score: score,
	}
}

func TestSearchView(t *testing.T) {
	msgs := []*MessageDisplay{readMsg(1, false, "dominoes tonight"), readMsg(2, false, "chess")}
	a, general := newTestAppForRendering(80, msgs)
	a.width, a.height = 120, 40
	sid := uuid.New()
	a.activeConn.ServerID = sid
	a.activeConn.ServerVersion = "0.1.9"
	a.openSearch("")
	a.search.input.SetValue("domino*")
	a.runSearch() // no connection manager: nothing sent, but the search is on screen
	if a.search.query != "domino*" || a.search.servers[sid] == nil {
		t.Fatalf("search %+v", a.search)
	}

	// A stale answer (another query) is ignored; the right one is shown.
	a.handleSearchResults(sid, a.activeConn, protocol.SearchResultsPayload{ServerID: sid, Query: "domin", Items: []protocol.SearchHit{hitFor(general, "old", "", 0, time.Now())}})
	if len(a.searchList()) != 0 {
		t.Fatal("a stale result was shown")
	}
	a.handleSearchResults(sid, a.activeConn, protocol.SearchResultsPayload{ServerID: sid, Query: "domino*", HasMore: true, Items: []protocol.SearchHit{
		hitFor(general, "dominoes tonight", "\x02dominoes\x03 tonight", -2, time.Now()),
	}})
	out := ansi.Strip(a.renderSearch())
	for _, want := range []string{"Search", "Best match", "dominoes tonight", "juniper"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}

	// Typing changes the query and waits for a pause.
	cmd := a.handleSearchKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if cmd == nil || a.search.input.Value() != "domino*x" || a.search.query != "domino*" {
		t.Errorf("typing: %q / %q", a.search.input.Value(), a.search.query)
	}
	a.searchPaused(searchDebounceMsg{gen: a.search.gen - 1})
	if a.search.query != "domino*" {
		t.Error("an earlier pause searched")
	}
	a.searchPaused(searchDebounceMsg{gen: a.search.gen})
	if a.search.query != "domino*x" {
		t.Error("the pause didn't search")
	}
}

func TestSnippetText(t *testing.T) {
	hot := lipgloss.NewStyle().Bold(true)
	if got := snippetText("say \x02hello\x03\nto \x1b[31mall", hot, true); got != "say hello to [31mall" {
		t.Errorf("%q", got)
	}
}

// Results from several servers merge by score; newest first by time.
func TestSearchMerge(t *testing.T) {
	a, ch := newTestAppForRendering(80, nil)
	s1, s2 := uuid.New(), uuid.New()
	now := time.Now()
	a.search = &searchView{query: "x", order: []uuid.UUID{s1, s2}, servers: map[uuid.UUID]*searchServer{
		s1: {hits: []searchHit{{serverID: s1, msg: &MessageDisplay{Message: &models.Message{ChannelID: ch, Content: "weak", CreatedAt: now}}, score: -1}}},
		s2: {hits: []searchHit{{serverID: s2, msg: &MessageDisplay{Message: &models.Message{ChannelID: ch, Content: "strong", CreatedAt: now.Add(-time.Hour)}}, score: -5}}},
	}}
	if l := a.searchList(); l[0].msg.Content != "strong" {
		t.Errorf("by score: %s first", l[0].msg.Content)
	}
	a.search.newest = true
	if l := a.searchList(); l[0].msg.Content != "weak" {
		t.Errorf("newest: %s first", l[0].msg.Content)
	}
}

// Found 2026-11-04: typing in the search box also typed into the message
// box, and searches carried this client's handle for the connection instead
// of the server's own ID, so the server answered "Not a member".
func TestSearchKeysAndServerID(t *testing.T) {
	a := newEmojiTestApp(t)
	for _, open := range []func(){func() { a.openSearch("") }, func() { a.mentions = &mentionsInbox{servers: map[uuid.UUID]*inboxServer{}} }} {
		a.search, a.mentions = nil, nil
		a.input.SetValue("")
		open()
		for _, r := range "yazi" {
			a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		if v := a.input.Value(); v != "" {
			t.Errorf("the message box got %q while an overlay was open (search %v, mentions %v)", v, a.search != nil, a.mentions != nil)
		}
	}

	wire := uuid.New()
	sc := &ServerConnection{ServerID: uuid.New(), Servers: []*models.Server{{ID: wire}}}
	if got := wireServerID(sc); got != wire {
		t.Errorf("wireServerID = %v, want the server's own %v", got, wire)
	}
	if got := wireServerID(&ServerConnection{ServerID: uuid.New()}); got != uuid.Nil {
		t.Errorf("before READY: %v", got)
	}
}
