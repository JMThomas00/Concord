package client

// Message search (Ctrl+F, /search; servers v0.1.9+): an overlay with a
// search box. The server parses the query (words, "phrases", prefix*,
// from: in: has: before: after:) and searches its full-text index; this
// server by default, Tab for every connected one. Results come best match
// first (Ctrl+S: newest first); Enter jumps to one the way the mentions
// inbox does (mentions.go).

import (
	"log"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/protocol"
)

// searchDelay is how long typing pauses before the search runs.
const searchDelay = 300 * time.Millisecond

type searchView struct {
	input      textinput.Model
	allServers bool
	newest     bool
	cursor     int
	query      string // the search on screen (what was last sent)
	gen        int    // bumped by each edit, so only the last pause searches
	servers    map[uuid.UUID]*searchServer
	order      []uuid.UUID
}

type searchServer struct {
	name    string
	hits    []searchHit
	loading bool
	more    bool
	err     string
	old     bool // before v0.1.9: no search
}

type searchHit struct {
	serverID uuid.UUID
	msg      *MessageDisplay
	snippet  string
	score    float64
}

type searchDebounceMsg struct{ gen int }

// openSearch opens the search box, with query typed in (and run) if given.
func (a *App) openSearch(query string) tea.Cmd {
	in := textinput.New()
	in.Placeholder = `words, "a phrase", pref*, from:name in:#channel has:link before:2026-11-05`
	in.Prompt = "  "
	in.CharLimit = 200
	in.SetValue(query)
	a.search = &searchView{input: in}
	cmd := a.search.input.Focus()
	if strings.TrimSpace(query) != "" {
		a.runSearch()
	}
	return cmd
}

// searchTargets is which servers a search goes to.
func (a *App) searchTargets() []*ServerConnection {
	if a.search.allServers && a.connMgr != nil {
		return a.connMgr.GetConnectedServers()
	}
	if a.activeConn != nil {
		return []*ServerConnection{a.activeConn}
	}
	return nil
}

// runSearch sends what's typed to each target server, replacing what's
// shown.
func (a *App) runSearch() {
	s := a.search
	s.query = strings.TrimSpace(s.input.Value())
	s.servers, s.order, s.cursor = map[uuid.UUID]*searchServer{}, nil, 0
	if s.query == "" {
		return
	}
	for _, sc := range a.searchTargets() {
		srv := &searchServer{name: serverName(sc), old: !keepsReads(sc)}
		s.servers[sc.ServerID] = srv
		s.order = append(s.order, sc.ServerID)
		if !srv.old {
			a.sendSearch(sc.ServerID, 0)
		}
	}
}

func (a *App) sendSearch(serverID uuid.UUID, offset int) {
	s := a.search
	if a.connMgr == nil {
		return
	}
	_, off := time.Now().In(viewerZone).Zone()
	msg, err := protocol.NewMessage(protocol.OpSearch, protocol.SearchRequest{
		ServerID: wireServerID(a.connMgr.GetConnection(serverID)), Query: s.query, Newest: s.newest, Offset: offset, UTCOffset: off,
	})
	if err != nil {
		return
	}
	if err := a.connMgr.SendRaw(serverID, msg); err != nil {
		log.Printf("search: %v", err)
		return
	}
	s.servers[serverID].loading = true
}

// handleSearchResults takes a page of results (SEARCH_RESULTS), unless
// it's for a search no longer on screen.
func (a *App) handleSearchResults(serverID uuid.UUID, sc *ServerConnection, p protocol.SearchResultsPayload) {
	s := a.search
	if s == nil || s.servers[serverID] == nil || p.Query != s.query || p.Newest != s.newest {
		return
	}
	srv := s.servers[serverID]
	if p.Offset == 0 {
		srv.hits = nil
	} else if p.Offset != len(srv.hits) {
		return // a page we already have (or one out of order)
	}
	self := selfID(sc)
	for _, it := range p.Items {
		if it.Message == nil || it.Message.Message == nil {
			continue
		}
		srv.hits = append(srv.hits, searchHit{serverID: serverID, msg: threadDisplay(it.Message, self, a.theme.Colors.Purple), snippet: it.Snippet, score: it.Score})
	}
	srv.loading, srv.more, srv.err = false, p.HasMore, p.Error
	s.cursor = min(s.cursor, max(0, len(a.searchList())-1))
}

// searchList is every target's results: one server's in its own order,
// several merged by score (or by time, newest first).
func (a *App) searchList() []searchHit {
	s := a.search
	if s == nil {
		return nil
	}
	var out []searchHit
	for _, id := range s.order {
		out = append(out, s.servers[id].hits...)
	}
	if len(s.order) > 1 {
		sort.SliceStable(out, func(i, j int) bool {
			if s.newest || out[i].score == out[j].score {
				return out[i].msg.CreatedAt.After(out[j].msg.CreatedAt)
			}
			return out[i].score < out[j].score
		})
	}
	return out
}

// handleSearchKey: arrows move through results, Enter searches (or jumps
// once the results are for what's typed), Tab changes scope, Ctrl+S the
// order, Esc closes; everything else edits the query.
func (a *App) handleSearchKey(msg tea.KeyMsg) tea.Cmd {
	s := a.search
	list := a.searchList()
	switch msg.String() {
	case "esc", "ctrl+f":
		a.search = nil
		return nil
	case "up":
		s.cursor = max(0, s.cursor-1)
	case "down":
		s.cursor = min(max(0, len(list)-1), s.cursor+1)
	case "pgup":
		s.cursor = max(0, s.cursor-10)
	case "pgdown":
		s.cursor = min(max(0, len(list)-1), s.cursor+10)
	case "tab", "shift+tab":
		s.allServers = !s.allServers
		a.runSearch()
		return nil
	case "ctrl+s":
		s.newest = !s.newest
		a.runSearch()
		return nil
	case "enter":
		if strings.TrimSpace(s.input.Value()) != s.query {
			a.runSearch()
			return nil
		}
		if s.cursor < len(list) {
			h := list[s.cursor]
			a.search = nil
			return a.jumpToMention(inboxItem{serverID: h.serverID, channelID: h.msg.ChannelID, msg: h.msg})
		}
		return nil
	default:
		before := s.input.Value()
		var cmd tea.Cmd
		s.input, cmd = s.input.Update(msg)
		if s.input.Value() == before {
			return cmd
		}
		s.gen++
		gen := s.gen
		return tea.Batch(cmd, tea.Tick(searchDelay, func(time.Time) tea.Msg { return searchDebounceMsg{gen} }))
	}
	// Near the end of what's loaded: the next page from each server with more.
	if s.cursor >= len(list)-5 {
		for _, id := range s.order {
			if srv := s.servers[id]; srv.more && !srv.loading {
				a.sendSearch(id, len(srv.hits))
			}
		}
	}
	return nil
}

// searchPaused runs the search once typing has paused.
func (a *App) searchPaused(m searchDebounceMsg) {
	if s := a.search; s != nil && m.gen == s.gen && strings.TrimSpace(s.input.Value()) != s.query {
		a.runSearch()
	}
}

// ── Drawing ─────────────────────────────────────────────────────────────────

// snippetText is a snippet on one line, with control characters (other
// than the match markers) dropped, and its matches styled with hot.
func snippetText(snippet string, hot lipgloss.Style, plain bool) string {
	snippet = strings.Join(strings.Fields(snippet), " ")
	var b, word strings.Builder
	in := false
	for _, r := range snippet {
		switch {
		case r == '\x02':
			in = true
		case r == '\x03':
			if plain {
				b.WriteString(word.String())
			} else {
				b.WriteString(hot.Render(word.String()))
			}
			word.Reset()
			in = false
		case unicode.IsControl(r):
		case in:
			word.WriteRune(r)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteString(word.String())
	return b.String()
}

// renderSearch draws the search box and its results over the chat.
func (a *App) renderSearch() string {
	s := a.search
	c := a.theme.Colors
	w := min(110, max(50, a.width-8))
	inner := w - 4
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment))
	hot := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Yellow)).Bold(true)
	title := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple)).Bold(true)
	sel := lipgloss.NewStyle().Background(lipgloss.Color(c.Selection)).Foreground(lipgloss.Color(c.Foreground))
	on := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Background)).Background(lipgloss.Color(c.Purple)).Bold(true)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Red))

	scope := "This server"
	if a.activeConn != nil {
		scope = serverName(a.activeConn)
	}
	scopes := []string{scope, "All servers"}
	pick := func(i int, opts []string) string {
		var parts []string
		for j, o := range opts {
			if j == i {
				parts = append(parts, on.Render(" "+o+" "))
			} else {
				parts = append(parts, dim.Render(" "+o+" "))
			}
		}
		return strings.Join(parts, "")
	}
	scopeIdx, sortIdx := 0, 0
	if s.allServers {
		scopeIdx = 1
	}
	if s.newest {
		sortIdx = 1
	}
	head := title.Render("Search")
	right := pick(scopeIdx, scopes) + dim.Render("  ·  ") + pick(sortIdx, []string{"Best match", "Newest"})
	var b strings.Builder
	b.WriteString(head + strings.Repeat(" ", max(2, inner-ansi.StringWidth(head)-ansi.StringWidth(right))) + right + "\n")
	s.input.Width = inner - 4
	b.WriteString(s.input.View() + "\n\n")

	list := a.searchList()
	loading := false
	var notes []string
	for _, id := range s.order {
		srv := s.servers[id]
		loading = loading || srv.loading
		if srv.err != "" {
			notes = append(notes, red.Render(srv.err))
		}
		if srv.old {
			notes = append(notes, dim.Render(srv.name+" is older than v0.1.9, so it can't search"))
		}
	}
	rows := max(3, a.height-18)
	if len(list) == 0 {
		switch {
		case s.query == "":
			b.WriteString(dim.Render("  Type to search. Filters: from:name  in:#channel  has:link  has:file  before:2026-11-05  after:2026-11-01") + "\n")
		case loading:
			b.WriteString(dim.Render("  Searching…") + "\n")
		case len(notes) == 0:
			b.WriteString(dim.Render("  Nothing found.") + "\n")
		}
	}
	start := min(max(0, s.cursor-rows/2), max(0, len(list)-rows))
	multi := len(s.order) > 1
	for i := start; i < start+rows && i < len(list); i++ {
		h := list[i]
		where := "#" + a.channelNameOn(h.serverID, h.msg.ChannelID)
		if multi {
			where = s.servers[h.serverID].name + " › " + where
		}
		if h.msg.ThreadID != nil {
			where += " 🧵"
		}
		where = ansi.Truncate(where, 30, "…")
		who := ansi.Truncate(h.msg.AuthorName, 14, "…")
		when := a.formatTimestamp(h.msg.CreatedAt)
		left := where + strings.Repeat(" ", max(1, 32-ansi.StringWidth(where))) + who + strings.Repeat(" ", max(1, 16-ansi.StringWidth(who)))
		snip := h.snippet
		if snip == "" {
			snip = h.msg.Content
		}
		room := max(8, inner-2-ansi.StringWidth(left)-ansi.StringWidth(when)-2)
		if i == s.cursor {
			text := ansi.Truncate(snippetText(snip, hot, true), room, "…")
			pad := max(1, inner-2-ansi.StringWidth(left)-ansi.StringWidth(text)-ansi.StringWidth(when))
			b.WriteString("▸ " + sel.Render(left+text+strings.Repeat(" ", pad)+when) + "\n")
		} else {
			text := ansi.Truncate(snippetText(snip, hot, false), room, "…")
			pad := max(1, inner-2-ansi.StringWidth(left)-ansi.StringWidth(text)-ansi.StringWidth(when))
			b.WriteString("  " + left + text + dim.Render(strings.Repeat(" ", pad)+when) + "\n")
		}
	}
	if loading && len(list) > 0 {
		notes = append(notes, dim.Render("loading more…"))
	}
	if len(notes) > 0 {
		b.WriteString("\n" + strings.Join(notes, dim.Render(" · ")) + "\n")
	}
	b.WriteString("\n" + dim.Render("↑↓ choose · Enter search / go to it · Tab this server or all · Ctrl+S order · Esc close"))
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color(c.Purple)).
		Padding(0, 1).
		Width(w - 2).
		Background(lipgloss.Color(c.Background)).
		Render(b.String())
}
