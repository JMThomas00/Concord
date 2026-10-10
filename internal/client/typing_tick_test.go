package client

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

// Typing with the messages panel focused starts a message instead of
// vanishing (the viewport only scrolls).
func TestTypingInTheMessagesStartsAMessage(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)
	a.view = ViewMain
	a.focus = FocusChat
	for _, r := range "hi" {
		a.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if a.focus != FocusInput || a.input.Value() != "hi" {
		t.Fatalf("focus %v, input %q", a.focus, a.input.Value())
	}
}
