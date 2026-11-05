package client

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

func readMsg(seq int64, own bool, text string) *MessageDisplay {
	return &MessageDisplay{
		Message:    &models.Message{ID: uuid.New(), Content: text, CreatedAt: time.Now().Add(time.Duration(seq) * time.Minute), Seq: seq},
		AuthorName: map[bool]string{true: "me", false: "alice"}[own],
		IsOwn:      own,
		ShowHeader: true,
	}
}

// readsApp has a v0.1.9 server with #general (open) and #random.
func readsApp(t *testing.T) (a *App, sid, general, random uuid.UUID) {
	t.Helper()
	msgs := []*MessageDisplay{readMsg(1, false, "old one"), readMsg(2, true, "my reply"), readMsg(3, false, "first new"), readMsg(4, false, "second new")}
	a, general = newTestAppForRendering(60, msgs)
	random = uuid.New()
	sid = uuid.New()
	a.activeConn.ServerVersion = "0.1.9"
	a.unreadCounts = map[uuid.UUID]map[uuid.UUID]int{}
	a.mentionCounts = map[uuid.UUID]map[uuid.UUID]int{}
	a.mutedChannels = map[uuid.UUID]bool{}
	a.mutedServers = map[uuid.UUID]bool{}
	a.currentChannel = nil
	return a, sid, general, random
}

func TestSeedUnreadsAndDivider(t *testing.T) {
	a, sid, general, random := readsApp(t)
	sc := a.activeConn
	a.seedUnreads(sid, sc, []protocol.ChannelUnread{
		{ChannelID: general, ReadSeq: 2, Unread: 2},
		{ChannelID: random, ReadSeq: 9, Unread: 4, Mentions: 1},
	})
	if a.unreadCounts[sid][general] != 2 || a.unreadCounts[sid][random] != 4 || a.mentionCounts[sid][random] != 1 {
		t.Fatalf("seeded %v / %v", a.unreadCounts[sid], a.mentionCounts[sid])
	}

	// Opening #general places the divider above "first new" (after seq 2,
	// not my own message), and it stays across a re-open of the same channel.
	a.currentChannel = &models.Channel{ID: general}
	a.openedChannel(sid, sc, general)
	if a.divider == nil || a.divider.afterSeq != 2 {
		t.Fatalf("divider %+v", a.divider)
	}
	delete(a.unreadCounts[sid], general)
	a.updateChatContent()
	lines := strings.Split(ansi.Strip(renderAll(a)), "\n")
	newAt, firstAt := -1, -1
	for i, l := range lines {
		if strings.Contains(l, "─ new ─") && newAt < 0 {
			newAt = i
		}
		if strings.Contains(l, "first new") && firstAt < 0 {
			firstAt = i
		}
	}
	if newAt < 0 || firstAt <= newAt || strings.Contains(strings.Join(lines[:newAt], "\n"), "first new") {
		t.Fatalf("divider at %d, first new at %d:\n%s", newAt, firstAt, strings.Join(lines, "\n"))
	}
	if a.divider.line != newAt {
		t.Errorf("recorded line %d, drawn at %d", a.divider.line, newAt)
	}
	a.openedChannel(sid, sc, general) // a reconnect re-opens it
	if a.divider == nil || a.divider.afterSeq != 2 {
		t.Error("the divider went on a re-open of the same channel")
	}

	// Alt+U scrolls there (in a chat too short to show it all).
	a.chatViewport.Height = 3
	a.chatViewport.GotoBottom()
	a.jumpToUnread()
	if a.chatViewport.YOffset != max(0, newAt-1) {
		t.Errorf("Alt+U: offset %d, want %d", a.chatViewport.YOffset, max(0, newAt-1))
	}

	// Leaving for #random: no divider there yet (it has unreads, so one is placed for it).
	a.currentChannel = &models.Channel{ID: random}
	a.openedChannel(sid, sc, random)
	if a.divider == nil || a.divider.channelID != random {
		t.Errorf("divider %+v after switching", a.divider)
	}
	a.currentChannel = &models.Channel{ID: general}
	a.openedChannel(sid, sc, general) // nothing unread now
	if a.divider != nil {
		t.Errorf("a divider with nothing unread: %+v", a.divider)
	}
	a.jumpToUnread()
	if a.statusMessage != "Nothing new here" {
		t.Errorf("status %q", a.statusMessage)
	}
}

// renderAll is the chat's whole content, not just the visible part.
func renderAll(a *App) string {
	h := a.chatViewport.Height
	a.chatViewport.Height = 1000
	a.chatViewport.GotoTop()
	v := a.chatViewport.View()
	a.chatViewport.Height = h
	return v
}

func TestReadElsewhereAndOldServers(t *testing.T) {
	a, sid, general, random := readsApp(t)
	sc := a.activeConn
	a.unreadCounts[sid] = map[uuid.UUID]int{random: 3}
	a.mentionCounts[sid] = map[uuid.UUID]int{random: 1}
	a.channelReadElsewhere(sid, sc, protocol.ChannelReadPayload{ChannelID: random, ReadSeq: 40})
	if a.unreadCounts[sid][random] != 0 || a.mentionCounts[sid][random] != 0 || sc.ReadSeq[random] != 40 {
		t.Errorf("after CHANNEL_READ: %v %v %v", a.unreadCounts[sid], a.mentionCounts[sid], sc.ReadSeq)
	}
	a.channelReadElsewhere(sid, sc, protocol.ChannelReadPayload{ChannelID: random, ReadSeq: 10})
	if sc.ReadSeq[random] != 40 {
		t.Error("a read position moved back")
	}

	// The open channel's count isn't seeded (it's being read).
	a.currentChannel = &models.Channel{ID: general}
	a.seedUnreads(sid, sc, []protocol.ChannelUnread{{ChannelID: general, ReadSeq: 1, Unread: 5}})
	if a.unreadCounts[sid][general] != 0 {
		t.Error("the open channel got an unread count")
	}

	// A server before v0.1.9: no divider, no reads sent, no throttle timer.
	sc.ServerVersion = "0.1.8"
	a.unreadCounts[sid][general] = 2
	a.divider = nil
	a.openedChannel(sid, sc, general)
	if a.divider != nil || a.markRead(sid, sc, general) != nil {
		t.Error("an old server got read positions")
	}
}

// Reads while messages keep arriving go out at most every readEvery.
func TestMarkReadIsThrottled(t *testing.T) {
	a, sid, general, _ := readsApp(t)
	sc := a.activeConn
	a.readSentAt = map[uuid.UUID]time.Time{general: time.Now()} // just sent
	cmd := a.markRead(sid, sc, general)
	if cmd == nil || !a.readPending[general] {
		t.Fatal("a read right after another wasn't held back")
	}
	if a.markRead(sid, sc, general) != nil {
		t.Error("a second timer for the same channel")
	}
	a.readSentAt[general] = time.Now().Add(-3 * readEvery)
	delete(a.readPending, general)
	if a.markRead(sid, sc, general) != nil {
		t.Error("a read after a quiet spell was held back")
	}
}

// Message times show in this computer's zone, whatever zone the server is in.
func TestTimesAreLocal(t *testing.T) {
	prev := time.Local
	time.Local = time.FixedZone("HERE", -6*3600)
	defer func() { time.Local = prev }()
	a := &App{}
	at := time.Date(2026, 11, 5, 3, 30, 0, 0, time.UTC) // 21:30 the day before, here
	if got := a.formatTimestamp(at); got != "11/04/26 21:30" {
		t.Errorf("formatTimestamp = %q", got)
	}
	if d := localDay(at); d.Day() != 4 || d.Hour() != 0 {
		t.Errorf("localDay = %v", d)
	}
}
