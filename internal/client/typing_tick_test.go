package client

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

// The typing animation ticks only while someone is typing: each tick
// redraws the screen, and ticking forever kept a CPU core busy on an idle
// client (2026-10-04).
func TestTypingTickerStopsWhenNobodyTypes(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)
	a.view = ViewMain
	a.typingTicking = true
	a.Update(typingTickMsg(time.Now()))
	if a.typingTicking {
		t.Fatal("still ticking with nobody typing")
	}
	if a.startTypingTick() == nil || !a.typingTicking {
		t.Fatal("didn't start for someone typing")
	}
	if a.startTypingTick() != nil {
		t.Fatal("started a second ticker")
	}
	a.typingExpiry = map[uuid.UUID]time.Time{uuid.New(): time.Now().Add(5 * time.Second)}
	a.Update(typingTickMsg(time.Now()))
	if !a.typingTicking {
		t.Fatal("stopped while someone is still typing")
	}
}
