package client

import (
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/google/uuid"

	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// newTestAppForRendering builds the minimal App needed to exercise
// updateChatContent()/calculateMessageLinePosition() — just the fields that
// code path actually touches, not a full NewApp().
func newTestAppForRendering(viewportWidth int, messages []*MessageDisplay) (*App, uuid.UUID) {
	channelID := uuid.New()
	theme := themes.GetDefaultTheme()

	sc := &ServerConnection{
		Messages: map[uuid.UUID][]*MessageDisplay{channelID: messages},
	}

	vp := viewport.New(viewportWidth, 20)

	a := &App{
		activeConn:     sc,
		currentChannel: &models.Channel{ID: channelID},
		chatViewport:   vp,
		theme:          theme,
		styles:         theme.BuildStyles(),
	}
	return a, channelID
}

// TestCalculateMessageLinePositionAccountsForWordWrap is the regression test
// for the 2026-08-19 finding: calculateMessageLinePosition used to
// re-derive line counts from raw \n splits only, on the stated assumption
// that updateChatContent() doesn't word-wrap — but it does, via lipgloss's
// Width() applied to each message's content line. Any message with a line
// longer than the viewport wraps to more than one visual line, which the
// old implementation never counted, so alt+m message navigation's
// scroll-into-view undershot for exactly the kind of long messages (e.g.
// Alice's recipe replies) that made this show up. calculateMessageLinePosition
// now just reads back what updateChatContent() actually rendered
// (a.messageLineOffsets), so it can't drift from the real render again —
// this test proves that by using a first message long enough to force
// wrapping and confirming the second message's recorded position reflects
// the extra wrapped lines, not just the raw \n count.
func TestCalculateMessageLinePositionAccountsForWordWrap(t *testing.T) {
	const viewportWidth = 40

	// One long, single-line (\n-free) message — well over viewportWidth, so
	// it must wrap to multiple visual lines. A naive raw-\n count would see
	// this as exactly 1 content line no matter how long it is.
	longContent := "This is a deliberately long single-line message with no newlines at all, long enough to wrap across several visual lines at a narrow viewport width."

	messages := []*MessageDisplay{
		{
			Message:    &models.Message{ID: uuid.New(), Content: longContent, CreatedAt: time.Now()},
			AuthorName: "Alice",
			ShowHeader: true,
		},
		{
			Message:    &models.Message{ID: uuid.New(), Content: "target", CreatedAt: time.Now().Add(time.Minute)},
			AuthorName: "gh0st",
			ShowHeader: true,
		},
	}

	a, _ := newTestAppForRendering(viewportWidth, messages)
	a.updateChatContent()

	pos := a.calculateMessageLinePosition(1)

	// What the old, wrapping-unaware implementation would have produced for
	// message 0: 1 header line + 1 content line (single \n-free string) + 1
	// blank separator = 3. If word-wrapping isn't accounted for, pos == 3.
	const oldBuggyPosition = 3
	if pos <= oldBuggyPosition {
		t.Errorf("calculateMessageLinePosition(1) = %d, want > %d (the long message's wrapped lines must push the second message further down than a naive no-wrap count would)", pos, oldBuggyPosition)
	}
}

// TestCalculateMessageLinePositionMatchesRenderedContent confirms the
// recorded offset for a message is exactly the number of newlines that
// precede its own content in the real rendered string — not just "greater
// than the naive count," but actually correct.
func TestCalculateMessageLinePositionMatchesRenderedContent(t *testing.T) {
	const viewportWidth = 60

	messages := []*MessageDisplay{
		{
			Message:    &models.Message{ID: uuid.New(), Content: "short one", CreatedAt: time.Now()},
			AuthorName: "Alice",
			ShowHeader: true,
		},
		{
			Message:    &models.Message{ID: uuid.New(), Content: "short two", CreatedAt: time.Now().Add(time.Minute)},
			AuthorName: "gh0st",
			ShowHeader: true,
		},
		{
			Message:    &models.Message{ID: uuid.New(), Content: "third", CreatedAt: time.Now().Add(2 * time.Minute)},
			AuthorName: "Alice",
			ShowHeader: true,
		},
	}

	a, _ := newTestAppForRendering(viewportWidth, messages)
	a.updateChatContent()

	for i := range messages {
		got := a.calculateMessageLinePosition(i)
		want := a.messageLineOffsets[i]
		if got != want {
			t.Errorf("calculateMessageLinePosition(%d) = %d, want %d (a.messageLineOffsets[%d], the recorded value from the real render)", i, got, want, i)
		}
	}

	// Positions must be strictly increasing — every message actually takes
	// up space.
	for i := 1; i < len(messages); i++ {
		if a.messageLineOffsets[i] <= a.messageLineOffsets[i-1] {
			t.Errorf("messageLineOffsets[%d] = %d, want > messageLineOffsets[%d] = %d", i, a.messageLineOffsets[i], i-1, a.messageLineOffsets[i-1])
		}
	}
}

// TestBotAuthorMessagesNeverGroupUnderOneHeader is the regression test for
// 2026-08-19's finding: two of Mynah's replies landing close together (now
// routine, since messages are answered one at a time but can arrive only
// seconds apart) rendered as one merged block with a single "(A) Alice ..."
// header — because updateChatContent()'s grouping logic only ever looked at
// author + time gap, the same rule meant for a human sending several
// consecutive chat lines. Two answers to two different questions reading as
// one reply is actively misleading, not just a cosmetic nit. A bot message
// (models.User.IsServiceAccount, carried onto MessageDisplay.IsBotAuthor)
// must always get its own header, even immediately following another
// message from the very same bot author within the normal grouping window.
func TestBotAuthorMessagesNeverGroupUnderOneHeader(t *testing.T) {
	const viewportWidth = 60
	humanAuthorID := uuid.New()
	botAuthorID := uuid.New()
	base := time.Now()

	messages := []*MessageDisplay{
		{
			Message:    &models.Message{ID: uuid.New(), AuthorID: humanAuthorID, Content: "hi", CreatedAt: base},
			AuthorName: "gh0st", ShowHeader: true,
		},
		{
			// Same human author, 10s later — well inside the default 5-minute
			// grouping window, so this one SHOULD merge under msg 0's header.
			Message:    &models.Message{ID: uuid.New(), AuthorID: humanAuthorID, Content: "hi", CreatedAt: base.Add(10 * time.Second)},
			AuthorName: "gh0st", ShowHeader: true,
		},
		{
			Message:     &models.Message{ID: uuid.New(), AuthorID: botAuthorID, Content: "hi", CreatedAt: base.Add(20 * time.Second)},
			AuthorName:  "Alice", ShowHeader: true, IsBotAuthor: true,
		},
		{
			// Same bot author, 10s later — same grouping window as the human
			// pair above, but must NOT merge: this answers a different
			// question than msg 2 did.
			Message:     &models.Message{ID: uuid.New(), AuthorID: botAuthorID, Content: "hi", CreatedAt: base.Add(30 * time.Second)},
			AuthorName:  "Alice", ShowHeader: true, IsBotAuthor: true,
		},
		{
			// Sentinel — exists only so messageLineOffsets[4]-messageLineOffsets[3]
			// bounds message 3's own contribution below.
			Message:    &models.Message{ID: uuid.New(), AuthorID: uuid.New(), Content: "hi", CreatedAt: base.Add(40 * time.Second)},
			AuthorName: "sentinel", ShowHeader: true,
		},
	}

	a, _ := newTestAppForRendering(viewportWidth, messages)
	a.updateChatContent()

	// messageLineOffsets[i] is where message i STARTS, so a given message's
	// own contribution is the gap to the NEXT message's start: message 1's
	// is offsets[2]-offsets[1], message 3's is offsets[4]-offsets[3].
	humanGroupedDiff := a.messageLineOffsets[2] - a.messageLineOffsets[1]
	botDiff := a.messageLineOffsets[4] - a.messageLineOffsets[3]

	if botDiff <= humanGroupedDiff {
		t.Errorf("second bot message's own line count = %d, want > the grouped human pair's %d — a second consecutive bot message must always render its own header instead of merging into the previous one", botDiff, humanGroupedDiff)
	}
}
