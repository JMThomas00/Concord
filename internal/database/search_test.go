package database

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

func TestSearchMessages(t *testing.T) {
	f := newReadsFixture(t)
	other := models.NewTextChannel(f.srv.ID, "random")
	f.db.CreateChannel(other)
	chans := []uuid.UUID{f.ch.ID, other.ID}
	q := func(match string, edit func(*SearchQuery)) []string {
		t.Helper()
		sq := SearchQuery{UserID: f.bob.ID, Channels: chans, Match: match, Limit: 20}
		if edit != nil {
			edit(&sq)
		}
		hits, err := f.db.SearchMessages(sq)
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, h := range hits {
			out = append(out, h.Message.Content)
		}
		return out
	}
	f.post(t, f.ch, f.alice, "Anyone up for dominoes at the Café?", nil)
	edited := f.post(t, f.ch, f.alice, "chess tonight", nil)
	f.post(t, other, f.bob, "dominoes again, dominoes forever", nil)
	f.post(t, f.ch, f.alice, "see https://example.com/rules", nil)
	gone := f.post(t, f.ch, f.alice, "dominoes secret", nil)
	f.db.SoftDeleteMessage(gone.ID, f.alice.ID)
	carol := uuid.New()
	f.post(t, f.ch, f.alice, "dominoes whisper for carol", func(m *models.Message) { m.IsWhisper, m.RecipientID = true, &carol })
	f.post(t, f.ch, f.alice, "dominoes whisper for bob", func(m *models.Message) { m.IsWhisper, m.RecipientID = true, &f.bob.ID })

	got := q(`"domino"*`, nil)
	if len(got) != 3 || got[0] != "dominoes again, dominoes forever" {
		t.Errorf("best match first, no deleted or other people's whispers: %q", got)
	}
	if got := q(`"cafe"`, nil); len(got) != 1 {
		t.Errorf("accents: %q", got)
	}
	if got := q(`"domino"*`, func(s *SearchQuery) { s.Authors = []uuid.UUID{f.bob.ID} }); len(got) != 1 {
		t.Errorf("from bob: %q", got)
	}
	if got := q(`"domino"*`, func(s *SearchQuery) { s.Channels = []uuid.UUID{other.ID} }); len(got) != 1 {
		t.Errorf("in #random: %q", got)
	}
	if got := q("", func(s *SearchQuery) { s.HasLink = true }); len(got) != 1 || !strings.Contains(got[0], "https") {
		t.Errorf("has:link: %q", got)
	}
	if got := q(`"domino"*`, func(s *SearchQuery) { s.Newest = true }); got[0] != "dominoes whisper for bob" {
		t.Errorf("newest first: %q", got)
	}

	// Edits and deletes keep the index right.
	edited.Content = "checkers tonight"
	if err := f.db.UpdateMessage(edited); err != nil {
		t.Fatal(err)
	}
	if len(q(`"chess"`, nil)) != 0 || len(q(`"checkers"`, nil)) != 1 {
		t.Error("an edit didn't reach the index")
	}
	if _, err := f.db.Exec(`DELETE FROM messages WHERE id = ?`, edited.ID.String()); err != nil {
		t.Fatal(err)
	}
	if len(q(`"checkers"`, nil)) != 0 {
		t.Error("a pruned message is still found")
	}
	hits, _ := f.db.SearchMessages(SearchQuery{UserID: f.bob.ID, Channels: chans, Match: `"cafe"`, Limit: 5})
	if len(hits) != 1 || !strings.Contains(hits[0].Snippet, SnippetOpen+"Café"+SnippetClose) {
		t.Errorf("snippet %+v", hits)
	}
}

// Dates become positions by a search over Seq.
func TestSeqAt(t *testing.T) {
	f := newReadsFixture(t)
	base := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	var seqs []int64
	for i := 0; i < 5; i++ {
		m := f.post(t, f.ch, f.alice, "day", func(m *models.Message) { m.CreatedAt = base.AddDate(0, 0, i) })
		seqs = append(seqs, m.Seq)
	}
	for i, want := range seqs {
		if got, _ := f.db.SeqAt(base.AddDate(0, 0, i).Add(-time.Hour)); got != want {
			t.Errorf("day %d: %d, want %d", i, got, want)
		}
	}
	if got, _ := f.db.SeqAt(base.AddDate(0, 0, 9)); got != seqs[4]+1 {
		t.Errorf("after everything: %d", got)
	}
}

// A database from before search gets every message indexed once.
func TestSearchIndexMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, _ := New(path)
	f := &readsFixture{db: db}
	f.alice = models.NewUser("alice", "alice@test.com")
	db.CreateUser(f.alice, "password123")
	f.srv = models.NewServer("Old", f.alice.ID)
	db.CreateServer(f.srv)
	f.ch = models.NewTextChannel(f.srv.ID, "general")
	db.CreateChannel(f.ch)
	f.post(t, f.ch, f.alice, "an old message about grapes", nil)
	if _, err := db.Exec(`DROP TABLE messages_fts; DROP TRIGGER messages_fts_insert; DELETE FROM migrations_done WHERE name = '2026-11-05-search-index'`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, _ = New(path)
	defer db.Close()
	hits, err := db.SearchMessages(SearchQuery{UserID: f.alice.ID, Channels: []uuid.UUID{f.ch.ID}, Match: `"grapes"`, Limit: 5})
	if err != nil || len(hits) != 1 {
		t.Errorf("after the migration: %d, %v", len(hits), err)
	}
}
