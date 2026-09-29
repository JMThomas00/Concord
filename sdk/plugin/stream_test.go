package plugin_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/google/uuid"
)

// startConn runs a plugin against a fake server and returns its Conn.
func startConn(t *testing.T) (*plugintest.Server, *plugin.Conn) {
	t.Helper()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	ready := make(chan *plugin.Conn, 1)
	go plugin.Run(ctx, srv.Config(), plugin.Handler{OnReady: func(c *plugin.Conn, _ *wire.User) { ready <- c }})
	srv.WaitReady()
	select {
	case c := <-ready:
		return srv, c
	case <-time.After(5 * time.Second):
		t.Fatal("plugin never became ready")
		return nil, nil
	}
}

func TestPostMessageReturnsTheID(t *testing.T) {
	srv, c := startConn(t)
	ch := uuid.New()
	id, err := c.PostMessage(context.Background(), ch, "hello", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.EditMessage(ch, id, "hello again"); err != nil {
		t.Fatal(err)
	}
	if e := srv.NextEdit(); e.MessageID != id || e.Content != "hello again" || e.Stream != "" {
		t.Fatalf("edit = %+v", e)
	}
}

// The first piece appears at once; later ones wait for Interval, and
// Close sends whatever is left.
func TestStreamShowsTheFirstPieceAtOnce(t *testing.T) {
	srv, c := startConn(t)
	s := c.Stream(context.Background(), uuid.New(), nil)
	s.Interval = time.Hour
	s.Write("Hi ")
	s.Write("there!")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	posted := srv.Posted()
	if len(posted) == 1 {
		posted[0] = srv.WaitStreamDone(posted[0].ID)
	}
	if len(posted) != 1 || posted[0].Content != "Hi there!" || posted[0].Stream != wire.StreamDone || posted[0].Edits != 1 {
		t.Fatalf("posted %+v, want one message: the first piece, then one final edit", posted)
	}
}

// A reply that takes a while grows by edits marked as streaming, and the
// last one marks it done.
func TestStreamGrowsByEdits(t *testing.T) {
	srv, c := startConn(t)
	ch := uuid.New()
	question := uuid.New()
	s := c.Stream(context.Background(), ch, &question)
	s.Interval = 0
	for _, w := range []string{"Sure", ", here's ", "how."} {
		if err := s.Write(w); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	posted := srv.Posted()
	if len(posted) != 1 {
		t.Fatalf("posted %d messages", len(posted))
	}
	msg := srv.WaitStreamDone(posted[0].ID)
	if msg.Content != "Sure, here's how." || msg.Stream != wire.StreamDone || msg.ReplyToID == nil || *msg.ReplyToID != question {
		t.Fatalf("final message %+v", msg)
	}
	if first := <-chatOf(srv); first.Stream != wire.StreamWriting || first.Content != "Sure" {
		t.Fatalf("first post %+v, want the first piece marked writing", first)
	}
}

func chatOf(srv *plugintest.Server) chan wire.SendMessagePayload {
	out := make(chan wire.SendMessagePayload, 1)
	out <- srv.NextChat()
	return out
}

// A reply longer than one message continues in a new one; a code block
// cut in two is closed and reopened, and nothing exceeds the limit or
// splits a character.
func TestLongStreamSplitsAcrossMessages(t *testing.T) {
	srv, c := startConn(t)
	s := c.Stream(context.Background(), uuid.New(), nil)
	s.Interval = 0
	var want strings.Builder
	want.WriteString("Intro — ünïcödé.\n\n```go\n")
	for i := 0; i < 150; i++ {
		want.WriteString("fmt.Println(\"line\") // ✓ ok\n")
	}
	want.WriteString("```\n\nThat's all.")
	for _, r := range want.String() {
		if err := s.Write(string(r)); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	posted := srv.Posted()
	if len(posted) < 3 {
		t.Fatalf("a %d-byte reply fit in %d messages", want.Len(), len(posted))
	}
	var joined []string
	for i, p := range posted {
		p = srv.WaitStreamDone(p.ID)
		if strings.Count(p.Content, "```")%2 != 0 {
			t.Errorf("message %d leaves a code block open:\n%s", i, p.Content)
		}
		if i > 0 && p.ReplyToID != nil {
			t.Errorf("continuation %d replies to something", i)
		}
		joined = append(joined, p.Content)
	}
	// Undo the close/reopen at the seams: the text survives.
	all := strings.Join(joined, "\n")
	all = strings.ReplaceAll(all, "\n```\n```go\n", "\n")
	if strings.Count(all, "fmt.Println") != 150 || !strings.HasPrefix(all, "Intro") || !strings.HasSuffix(all, "That's all.") {
		t.Fatalf("text lost across the split:\n%s", all)
	}
}
