package server

// Message search (OpSearch → SEARCH_RESULTS): the query is parsed here, so
// every client searches the same way, and run on the database's full-text
// index (database/search.go) over the channels the searcher may view.

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// searchPage is the most hits one SEARCH_RESULTS carries.
const searchPage = 25

// parsedSearch is a query split into words and filters.
type parsedSearch struct {
	terms   []string // FTS5 terms, each already quoted (and maybe ending in *)
	from    []string
	in      []string
	hasLink bool
	hasFile bool
	before  string
	after   string
}

// parseSearch splits what was typed: "a phrase", word, prefix*, and
// from:/in:/has:/before:/after: filters. An unknown has: is an error.
func parseSearch(raw string) (parsedSearch, error) {
	var p parsedSearch
	for _, tok := range searchTokens(raw) {
		if tok.phrase {
			if w := strings.TrimSpace(tok.text); w != "" {
				p.terms = append(p.terms, ftsQuote(w))
			}
			continue
		}
		key, val, ok := strings.Cut(tok.text, ":")
		if ok && val != "" {
			switch strings.ToLower(key) {
			case "from":
				p.from = append(p.from, strings.ToLower(strings.TrimPrefix(val, "@")))
				continue
			case "in":
				p.in = append(p.in, strings.ToLower(strings.TrimPrefix(val, "#")))
				continue
			case "has":
				switch strings.ToLower(val) {
				case "link", "links", "url":
					p.hasLink = true
				case "file", "files", "attachment":
					p.hasFile = true
				default:
					return p, fmt.Errorf("has:%s isn't a filter (has:link or has:file)", val)
				}
				continue
			case "before":
				p.before = val
				continue
			case "after":
				p.after = val
				continue
			}
		}
		prefix := strings.HasSuffix(tok.text, "*")
		w := strings.TrimFunc(tok.text, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if w == "" {
			continue
		}
		q := ftsQuote(w)
		if prefix {
			q += "*"
		}
		p.terms = append(p.terms, q)
	}
	return p, nil
}

type searchToken struct {
	text   string
	phrase bool
}

// searchTokens splits on spaces, keeping "quoted phrases" whole.
func searchTokens(raw string) []searchToken {
	var out []searchToken
	for raw = strings.TrimSpace(raw); raw != ""; raw = strings.TrimSpace(raw) {
		if raw[0] == '"' {
			end := strings.IndexByte(raw[1:], '"')
			if end < 0 {
				out = append(out, searchToken{text: raw[1:], phrase: true})
				break
			}
			out = append(out, searchToken{text: raw[1 : end+1], phrase: true})
			raw = raw[end+2:]
			continue
		}
		i := strings.IndexFunc(raw, unicode.IsSpace)
		if i < 0 {
			i = len(raw)
		}
		out = append(out, searchToken{text: raw[:i]})
		raw = raw[i:]
	}
	return out
}

// ftsQuote makes text one FTS5 string: nothing in it is query syntax.
func ftsQuote(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// HandleSearch answers a search in one server.
func (h *Handlers) HandleSearch(c *Client, msg *protocol.Message) {
	var req protocol.SearchRequest
	if err := json.Unmarshal(msg.Data, &req); err != nil {
		c.sendError(protocol.ErrorCodeInvalidPayload, "Invalid request format")
		return
	}
	if _, err := h.db.GetServerMember(req.ServerID, c.UserID); err != nil {
		c.sendError(protocol.ErrorCodeForbidden, "Not a member of this server")
		return
	}
	out := protocol.SearchResultsPayload{ServerID: req.ServerID, Query: req.Query, Newest: req.Newest, Offset: req.Offset, Items: []protocol.SearchHit{}}
	reply := func() { _ = h.dispatchTo(c, protocol.EventSearchResults, out) }

	q, err := h.searchQuery(c.UserID, req)
	if err != nil {
		out.Error = err.Error()
		reply()
		return
	}
	hits, err := h.db.SearchMessages(q)
	if err != nil {
		MsgLog.Error("Search failed", "server_id", req.ServerID, "query", req.Query, "error", err)
		out.Error = "The search didn't work: try fewer or simpler words"
		reply()
		return
	}
	if len(hits) > q.Limit {
		hits, out.HasMore = hits[:q.Limit], true
	}
	names := h.newDisplayNames(req.ServerID)
	for _, hit := range hits {
		if d := names.display(hit.Message); d != nil {
			out.Items = append(out.Items, protocol.SearchHit{Message: d, Snippet: hit.Snippet, Score: hit.Score})
		}
	}
	reply()
}

// searchQuery turns a request into a database search: the channels the
// user may view (narrowed by in:), the authors from:, and dates as
// positions.
func (h *Handlers) searchQuery(userID uuid.UUID, req protocol.SearchRequest) (database.SearchQuery, error) {
	limit := req.Limit
	if limit <= 0 || limit > searchPage {
		limit = searchPage
	}
	q := database.SearchQuery{UserID: userID, Newest: req.Newest, Offset: max(0, req.Offset), Limit: limit + 1}
	p, err := parseSearch(req.Query)
	if err != nil {
		return q, err
	}
	if len(p.terms) == 0 && len(p.from) == 0 && len(p.in) == 0 && !p.hasLink && !p.hasFile && p.before == "" && p.after == "" {
		return q, fmt.Errorf("Type something to search for")
	}
	q.Match = strings.Join(p.terms, " ")
	q.HasLink, q.HasFile = p.hasLink, p.hasFile

	channels, err := h.db.GetServerChannels(req.ServerID)
	if err != nil {
		return q, fmt.Errorf("Couldn't read this server's channels")
	}
	found := map[string]bool{} // in: names that matched a channel the user may view
	for _, n := range p.in {
		found[n] = false
	}
	for _, ch := range channels {
		if ch.Type == models.ChannelTypeCategory || ch.Type == models.ChannelTypeDM || ch.Type == models.ChannelTypeGroupDM {
			continue
		}
		name := strings.ToLower(ch.Name)
		if _, named := found[name]; len(p.in) > 0 && !named {
			continue
		}
		if h.hasChannelPermission(userID, ch, models.PermissionViewChannels) != nil {
			continue
		}
		found[name] = true
		q.Channels = append(q.Channels, ch.ID)
	}
	for _, n := range p.in {
		if !found[n] {
			return q, fmt.Errorf("There's no channel called #%s here", n)
		}
	}

	if len(p.from) > 0 {
		ids, err := h.db.UserIDsByName(req.ServerID, p.from)
		if err != nil {
			return q, fmt.Errorf("Couldn't look up who that is")
		}
		if len(ids) == 0 {
			return q, fmt.Errorf("Nobody here is called %s", strings.Join(p.from, " or "))
		}
		q.Authors = ids
	}

	zone := time.FixedZone("searcher", req.UTCOffset)
	for _, d := range []struct {
		val   string
		into  *int64
		after bool
	}{{p.after, &q.AfterSeq, true}, {p.before, &q.BeforeSeq, false}} {
		if d.val == "" {
			continue
		}
		day, err := time.ParseInLocation("2006-01-02", d.val, zone)
		if err != nil {
			return q, fmt.Errorf("Dates are written 2026-11-05 (%s)", d.val)
		}
		if d.after {
			day = day.AddDate(0, 0, 1) // after a day: from the next one
		}
		seq, err := h.db.SeqAt(day)
		if err != nil {
			return q, fmt.Errorf("Couldn't read the dates")
		}
		*d.into = seq
	}
	return q, nil
}
