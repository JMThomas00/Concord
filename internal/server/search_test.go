package server

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func TestParseSearch(t *testing.T) {
	p, err := parseSearch(`domino* "the Café" from:@Juniper in:#Games has:link before:2026-11-05 after:2026-11-01 "unclosed`)
	if err != nil {
		t.Fatal(err)
	}
	want := parsedSearch{
		terms: []string{`"domino"*`, `"the Café"`, `"unclosed"`},
		from:  []string{"juniper"}, in: []string{"games"}, hasLink: true,
		before: "2026-11-05", after: "2026-11-01",
	}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("parsed %+v\nwant   %+v", p, want)
	}
	// Query syntax in what's typed is only text.
	p, _ = parseSearch(`NEAR(a b) OR -x "say ""hi"""`)
	for _, term := range p.terms {
		if !strings.HasPrefix(term, `"`) {
			t.Errorf("unquoted term %q", term)
		}
	}
	if _, err := parseSearch("has:pictures"); err == nil {
		t.Error("has:pictures accepted")
	}
}

func search(t *testing.T, c *testWSClient, req protocol.SearchRequest) protocol.SearchResultsPayload {
	t.Helper()
	c.send(protocol.OpSearch, req)
	m := c.readUntil(5*time.Second, func(m *protocol.Message) bool { return m.Type == protocol.EventSearchResults || (m.Op == protocol.OpDispatch && m.Type == "") })
	if m == nil || m.Type != protocol.EventSearchResults {
		t.Fatalf("no SEARCH_RESULTS: %+v", m)
	}
	var p protocol.SearchResultsPayload
	mustUnmarshal(t, m.Data, &p)
	return p
}

func TestSearchOverTheWire(t *testing.T) {
	srv, wsURL, users, tokens, ch, _ := reactionServer(t, models.NewEveryoneRole(uuid.Nil).Permissions)
	sid := ch.ServerID
	hidden := models.NewTextChannel(sid, "staff")
	if err := srv.db.CreateChannel(hidden); err != nil {
		t.Fatal(err)
	}
	roles, _ := srv.db.GetServerRoles(sid)
	for _, r := range roles {
		if r.Name == "everyone" || r.Name == "@everyone" {
			if err := srv.db.SetChannelPermissionOverwrite(hidden.ID, models.PermissionOverwrite{ID: r.ID, Type: "role", Deny: int64(models.PermissionViewChannels)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := srv.db.CreateMessage(models.NewMessage(hidden.ID, users[0].ID, "dominoes plans, staff only")); err != nil {
		t.Fatal(err)
	}

	alice := newTestWSClient(t, wsURL)
	alice.identify(tokens[1])
	post(t, alice, ch.ID, "dominoes tonight at eight", nil)
	post(t, alice, ch.ID, "anyone for chess", nil)
	bob := newTestWSClient(t, wsURL)
	bob.identify(tokens[2])
	post(t, bob, ch.ID, "dominoes? count me in", nil)

	got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "domino*"})
	if got.Error != "" || len(got.Items) != 2 {
		t.Fatalf("domino*: %+v", got)
	}
	for _, it := range got.Items {
		if it.Message.ChannelID == hidden.ID {
			t.Error("found a message in a channel bob can't view")
		}
		if !strings.Contains(it.Snippet, "\x02") || it.Message.Author == nil {
			t.Errorf("hit %+v", it)
		}
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "domino* from:" + users[1].Username}); len(got.Items) != 1 {
		t.Errorf("from alice: %+v", got.Items)
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "from:" + users[1].Username, Newest: true}); len(got.Items) != 2 || got.Items[0].Message.Content != "anyone for chess" {
		t.Errorf("filters only, newest first: %+v", got.Items)
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "domino* in:#staff"}); got.Error == "" {
		t.Error("in:#staff, a channel bob can't see, wasn't an error")
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "chess before:2000-01-01"}); len(got.Items) != 0 {
		t.Errorf("before 2000: %+v", got.Items)
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "chess after:2000-01-01"}); len(got.Items) != 1 {
		t.Errorf("after 2000: %+v", got.Items)
	}
	if got := search(t, bob, protocol.SearchRequest{ServerID: sid, Query: "from:nobody-at-all"}); got.Error == "" {
		t.Error("from:nobody wasn't an error")
	}
	// The owner sees the staff channel.
	owner := newTestWSClient(t, wsURL)
	owner.identify(tokens[0])
	if got := search(t, owner, protocol.SearchRequest{ServerID: sid, Query: "staff"}); len(got.Items) != 1 {
		t.Errorf("the owner's search: %+v", got)
	}
}
