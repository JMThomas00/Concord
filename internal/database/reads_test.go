package database

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

type readsFixture struct {
	db         *DB
	srv        *models.Server
	ch         *models.Channel
	alice, bob *models.User
}

func newReadsFixture(t *testing.T) *readsFixture {
	t.Helper()
	db, err := New(filepath.Join(t.TempDir(), "concord.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	f := &readsFixture{db: db}
	f.alice = models.NewUser("alice", "alice@test.com")
	f.bob = models.NewUser("bob", "bob@test.com")
	for _, u := range []*models.User{f.alice, f.bob} {
		if err := db.CreateUser(u, "password123"); err != nil {
			t.Fatal(err)
		}
	}
	f.srv = models.NewServer("Reads", f.alice.ID)
	if err := db.CreateServer(f.srv); err != nil {
		t.Fatal(err)
	}
	f.ch = models.NewTextChannel(f.srv.ID, "general")
	if err := db.CreateChannel(f.ch); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *readsFixture) post(t *testing.T, ch *models.Channel, author *models.User, text string, edit func(*models.Message)) *models.Message {
	t.Helper()
	m := models.NewMessage(ch.ID, author.ID, text)
	if edit != nil {
		edit(m)
	}
	if err := f.db.CreateMessage(m); err != nil {
		t.Fatal(err)
	}
	if m.Seq == 0 {
		t.Fatal("no seq")
	}
	return m
}

func (f *readsFixture) unread(t *testing.T, u *models.User, ch *models.Channel) ChannelUnread {
	t.Helper()
	list, err := f.db.ServerUnreads(u.ID, f.srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, cu := range list {
		if cu.ChannelID == ch.ID {
			return cu
		}
	}
	t.Fatalf("no entry for #%s", ch.Name)
	return ChannelUnread{}
}

func TestUnreadCounting(t *testing.T) {
	f := newReadsFixture(t)
	before := f.post(t, f.ch, f.alice, "said before bob joined", nil)
	if err := f.db.AddServerMember(models.NewServerMember(f.bob.ID, f.srv.ID)); err != nil {
		t.Fatal(err)
	}
	if got := f.unread(t, f.bob, f.ch); got.Unread != 0 || got.ReadSeq < before.Seq {
		t.Fatalf("a new member starts read: %+v", got)
	}

	f.post(t, f.ch, f.alice, "hello", nil)
	f.post(t, f.ch, f.alice, "hey @bob", func(m *models.Message) { m.Mentions = []uuid.UUID{f.bob.ID} })
	f.post(t, f.ch, f.alice, "@everyone lunch", func(m *models.Message) { m.MentionEveryone = true })
	f.post(t, f.ch, f.bob, "bob's own", nil)
	root := f.post(t, f.ch, f.alice, "thread start", nil)
	f.post(t, f.ch, f.alice, "a reply", func(m *models.Message) { m.ThreadID = &root.ID })
	f.post(t, f.ch, f.alice, "psst bob", func(m *models.Message) { m.IsWhisper, m.RecipientID = true, &f.bob.ID })
	carol := uuid.New()
	f.post(t, f.ch, f.alice, "psst carol", func(m *models.Message) { m.IsWhisper, m.RecipientID = true, &carol })
	gone := f.post(t, f.ch, f.alice, "deleted", nil)
	if err := f.db.SoftDeleteMessage(gone.ID, f.alice.ID); err != nil {
		t.Fatal(err)
	}

	// hello, @bob, @everyone, thread start, psst bob: not his own, the reply,
	// carol's whisper or the deleted one.
	if got := f.unread(t, f.bob, f.ch); got.Unread != 5 || got.Mentions != 3 {
		t.Errorf("bob: %+v, want 5 unread, 3 mentions (@bob, @everyone, the whisper)", got)
	}

	seq, err := f.db.MarkChannelRead(f.bob.ID, f.ch.ID)
	if err != nil || seq < gone.Seq {
		t.Fatalf("MarkChannelRead = %d, %v", seq, err)
	}
	if got := f.unread(t, f.bob, f.ch); got.Unread != 0 || got.Mentions != 0 || got.ReadSeq != seq {
		t.Errorf("after reading: %+v", got)
	}
	f.post(t, f.ch, f.alice, "one more", nil)
	if got := f.unread(t, f.bob, f.ch); got.Unread != 1 {
		t.Errorf("after one more: %+v", got)
	}

	// A position never moves back: marking an empty channel read again
	// can't undo anything, and positions are per channel.
	other := models.NewTextChannel(f.srv.ID, "later")
	if err := f.db.CreateChannel(other); err != nil {
		t.Fatal(err)
	}
	f.post(t, other, f.alice, "in a channel made after bob joined", nil)
	if got := f.unread(t, f.bob, other); got.Unread != 1 {
		t.Errorf("a new channel counts everything: %+v", got)
	}
	if _, err := f.db.Exec(`UPDATE channel_reads SET read_seq = read_seq + 100 WHERE user_id = ? AND channel_id = ?`, f.bob.ID.String(), f.ch.ID.String()); err != nil {
		t.Fatal(err)
	}
	if seq2, _ := f.db.MarkChannelRead(f.bob.ID, f.ch.ID); seq2 <= seq {
		t.Errorf("a read position moved back: %d", seq2)
	}
}

// The migration numbers existing messages in arrival order and starts
// everyone with everything read; it doesn't renumber on the next start.
func TestReadPositionsMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "concord.db")
	db, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	f := &readsFixture{db: db}
	f.alice = models.NewUser("alice", "alice@test.com")
	db.CreateUser(f.alice, "password123")
	f.srv = models.NewServer("Old", f.alice.ID)
	db.CreateServer(f.srv)
	db.AddServerMember(models.NewServerMember(f.alice.ID, f.srv.ID))
	f.ch = models.NewTextChannel(f.srv.ID, "general")
	db.CreateChannel(f.ch)
	bob := models.NewUser("bob", "bob@test.com")
	db.CreateUser(bob, "password123")
	db.AddServerMember(models.NewServerMember(bob.ID, f.srv.ID))
	for _, text := range []string{"one", "two", "three"} {
		f.post(t, f.ch, f.alice, text, nil)
	}
	// As a server from before: no seq, no read positions.
	if _, err := db.Exec(`UPDATE messages SET seq = NULL; DELETE FROM channel_reads; DELETE FROM migrations_done WHERE name = '2026-11-04-everything-read'`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	db, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	f.db = db
	var seqs []int64
	rows, _ := db.Query(`SELECT seq FROM messages ORDER BY rowid`)
	for rows.Next() {
		var s int64
		rows.Scan(&s)
		seqs = append(seqs, s)
	}
	rows.Close()
	if len(seqs) != 3 || seqs[0] >= seqs[1] || seqs[1] >= seqs[2] {
		t.Fatalf("seqs %v", seqs)
	}
	if got := f.unread(t, bob, f.ch); got.Unread != 0 {
		t.Errorf("an existing member got a flood: %+v", got)
	}
	f.post(t, f.ch, f.alice, "four", nil)
	if got := f.unread(t, bob, f.ch); got.Unread != 1 {
		t.Errorf("after a new message: %+v", got)
	}
}

// History follows arrival order, whatever the stored times say: they're text
// in the server's own zone, so a server that moved zones sorts them wrongly.
func TestHistoryIsInArrivalOrder(t *testing.T) {
	f := newReadsFixture(t)
	var sent []*models.Message
	for _, text := range []string{"one", "two", "three"} {
		sent = append(sent, f.post(t, f.ch, f.alice, text, nil))
	}
	// A server that moved from UTC to Central: each was written later than
	// the one before, but as text "one" sorts last.
	for i, at := range []string{"2026-11-04 16:00:00 +0000 UTC", "2026-11-04 10:30:00 -0600 CST", "2026-11-04 10:45:00 -0600 CST"} {
		if _, err := f.db.Exec(`UPDATE messages SET created_at = ? WHERE id = ?`, at, sent[i].ID.String()); err != nil {
			t.Fatal(err)
		}
	}
	got, err := f.db.GetChannelMessages(f.ch.ID, 10, nil, f.alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, m := range got {
		order = append(order, m.Content)
	}
	if strings.Join(order, ",") != "one,two,three" {
		t.Errorf("history %v", order)
	}
	older, err := f.db.GetChannelMessages(f.ch.ID, 10, &sent[2].ID, f.alice.ID)
	if err != nil || len(older) != 2 || older[0].Content != "one" {
		t.Errorf("before three: %d messages, %v", len(older), err)
	}
}
