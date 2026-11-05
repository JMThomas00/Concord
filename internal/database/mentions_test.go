package database

import (
	"testing"

	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
)

func TestMentionsInbox(t *testing.T) {
	f := newReadsFixture(t)
	if err := f.db.AddServerMember(models.NewServerMember(f.bob.ID, f.srv.ID)); err != nil {
		t.Fatal(err)
	}
	mine := f.post(t, f.ch, f.bob, "bob says hi", nil)
	f.post(t, f.ch, f.alice, "nothing for bob", nil)
	named := f.post(t, f.ch, f.alice, "hey @bob", func(m *models.Message) { m.Mentions = []uuid.UUID{f.bob.ID} })
	reply := f.post(t, f.ch, f.alice, "answering you", func(m *models.Message) { m.ReplyToID = &mine.ID })
	all := f.post(t, f.ch, f.alice, "@everyone lunch", func(m *models.Message) { m.MentionEveryone = true })
	whisper := f.post(t, f.ch, f.alice, "psst", func(m *models.Message) { m.IsWhisper, m.RecipientID = true, &f.bob.ID })
	carol := uuid.New()
	f.post(t, f.ch, f.alice, "psst carol @everyone", func(m *models.Message) { m.IsWhisper, m.RecipientID, m.MentionEveryone = true, &carol, true })
	inThread := f.post(t, f.ch, f.alice, "@bob in a thread", func(m *models.Message) { m.ThreadID = &named.ID; m.Mentions = []uuid.UUID{f.bob.ID} })
	f.db.FollowThread(f.bob.ID, named.ID, false) // mentioned: follows, unread
	gone := f.post(t, f.ch, f.alice, "@bob deleted", func(m *models.Message) { m.Mentions = []uuid.UUID{f.bob.ID} })
	f.db.SoftDeleteMessage(gone.ID, f.alice.ID)
	f.post(t, f.ch, f.bob, "@bob talking to myself", func(m *models.Message) { m.Mentions = []uuid.UUID{f.bob.ID} })

	got, err := f.db.MentionsFor(f.bob.ID, f.srv.ID, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		id   uuid.UUID
		kind string
	}{{inThread.ID, MentionDirect}, {whisper.ID, MentionWhisper}, {all.ID, MentionEveryone}, {reply.ID, MentionReply}, {named.ID, MentionDirect}}
	if len(got) != len(want) {
		t.Fatalf("%d mentions, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Message.ID != w.id || got[i].Kind != w.kind || !got[i].Unread {
			t.Errorf("#%d: %q %s unread=%v, want %q %s", i, got[i].Message.Content, got[i].Kind, got[i].Unread, w.kind, w.id)
		}
	}
	if got[0].Message.ThreadID == nil || *got[0].Message.ThreadID != named.ID {
		t.Error("the thread reply lost its thread")
	}

	// The reply and @everyone count toward the @ badge too.
	if u := f.unread(t, f.bob, f.ch); u.Mentions != 4 {
		t.Errorf("badge %d, want 4 (@bob, the reply, @everyone, the whisper)", u.Mentions)
	}

	// Reading the channel reads the top-level ones; the thread has its own.
	f.db.MarkChannelRead(f.bob.ID, f.ch.ID)
	got, _ = f.db.MentionsFor(f.bob.ID, f.srv.ID, 0, 50)
	if !got[0].Unread || got[1].Unread || got[4].Unread {
		t.Errorf("after reading the channel: %v %v %v", got[0].Unread, got[1].Unread, got[4].Unread)
	}
	f.db.MarkThreadRead(f.bob.ID, named.ID)
	if got, _ = f.db.MentionsFor(f.bob.ID, f.srv.ID, 0, 50); got[0].Unread {
		t.Error("the thread mention is still unread after reading the thread")
	}

	// Pages: newest first, then before the last one seen.
	page, _ := f.db.MentionsFor(f.bob.ID, f.srv.ID, 0, 2)
	next, _ := f.db.MentionsFor(f.bob.ID, f.srv.ID, page[1].Message.Seq, 2)
	if len(page) != 2 || len(next) != 2 || next[0].Message.ID != all.ID {
		t.Errorf("paging: %d then %d", len(page), len(next))
	}
}

func TestHistoryAround(t *testing.T) {
	f := newReadsFixture(t)
	var sent []*models.Message
	for i := 0; i < 60; i++ {
		sent = append(sent, f.post(t, f.ch, f.alice, "message", nil))
	}
	msgs, missing, err := f.db.GetChannelMessagesAround(f.ch.ID, sent[10].ID, 200, f.alice.ID)
	if err != nil || missing || len(msgs) != 60 || msgs[0].ID != sent[0].ID || msgs[59].ID != sent[59].ID {
		t.Fatalf("around 10: %d, missing %v, %v", len(msgs), missing, err)
	}
	msgs, _, _ = f.db.GetChannelMessagesAround(f.ch.ID, sent[40].ID, 200, f.alice.ID)
	if len(msgs) != 60-40+aroundBefore || msgs[aroundBefore].ID != sent[40].ID {
		t.Errorf("around 40: %d messages", len(msgs))
	}
	msgs, missing, _ = f.db.GetChannelMessagesAround(f.ch.ID, sent[30].ID, 30, f.alice.ID)
	if !missing || len(msgs) != 30 || msgs[aroundBefore].ID != sent[30].ID {
		t.Errorf("a small window: %d, missing %v", len(msgs), missing)
	}
	older, _ := f.db.GetChannelMessages(f.ch.ID, 5, &sent[30].ID, f.alice.ID)
	if len(older) != 5 || older[4].ID != sent[29].ID {
		t.Errorf("before 30: %d", len(older))
	}
}
