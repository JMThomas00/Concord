package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/installer"
)

// press sends a key and runs what it sets off (Huh moves between fields
// and groups with commands), dropping anything slow like a cursor blink.
func press(m *model, k tea.KeyMsg) {
	_, cmd := m.Update(k)
	run(m, cmd, 0)
}

func run(m *model, cmd tea.Cmd, depth int) {
	if cmd == nil || depth > 20 {
		return
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		switch msg := msg.(type) {
		case nil, tickMsg:
		case tea.BatchMsg:
			for _, c := range msg {
				run(m, c, depth+1)
			}
		default:
			_, next := m.Update(msg)
			run(m, next, depth+1)
		}
	case <-time.After(60 * time.Millisecond):
	}
}

var (
	enter    = tea.KeyMsg{Type: tea.KeyEnter}
	shiftTab = tea.KeyMsg{Type: tea.KeyShiftTab}
)

func typeText(m *model, s string) {
	for _, r := range s {
		press(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

// The admin email is on screen when it's asked for, and shift+tab gets
// back out of it even with something invalid typed (both failed once).
func TestEmailIsVisibleAndBackAlwaysWorks(t *testing.T) {
	m := testModel(t)
	m.plan.Components = []string{installer.Server}
	m.plan.ServerPort = freePortT(t)
	m.Update(enter) // past the intro
	for i := 0; i < 4 && !strings.Contains(plain(m.View()), "make you its admin"); i++ {
		press(m, enter) // components, folder, name, port
	}
	v := plain(m.View())
	if !strings.Contains(v, "make you its admin") || !strings.Contains(v, "you@example.com") {
		t.Fatalf("the email field isn't on screen:\n%s", v)
	}
	typeText(m, "not an email")
	press(m, enter)
	if v := plain(m.View()); !strings.Contains(v, "doesn't look like an email") {
		t.Fatalf("a bad email went through:\n%s", v)
	}
	press(m, shiftTab)
	if v := plain(m.View()); !strings.Contains(v, "Which port should it listen on?") || strings.Contains(v, "make you its admin") {
		t.Fatalf("shift+tab didn't go back:\n%s", v)
	}
}

// The grapes stay where the welcome put them.
func TestGrapesStayPut(t *testing.T) {
	at := func(v string) (int, int) {
		for r, line := range strings.Split(plain(v), "\n") {
			if c := strings.Index(line, ";##:"); c >= 0 {
				return r, len([]rune(line[:c]))
			}
		}
		return -1, -1
	}
	m := testModel(t)
	m.stageAt = time.Now().Add(-3 * time.Second)
	r1, c1 := at(m.View())
	m.Update(enter)
	r2, c2 := at(m.View())
	m.toTerms()
	r3, c3 := at(m.View())
	if r1 < 0 || r1 != r2 || c1 != c2 || r1 != r3 || c1 != c3 {
		t.Fatalf("grapes at %d,%d (welcome), %d,%d (form), %d,%d (terms)", r1, c1, r2, c2, r3, c3)
	}
	if v := plain(m.View()); !strings.Contains(v, "╭") || !strings.Contains(v, "Accept") {
		t.Fatalf("terms without their box or buttons:\n%s", v)
	}
}

func freePortT(t *testing.T) string {
	for p := 18400; p < 18500; p++ {
		if s := strconv.Itoa(p); installer.PortFree(s) {
			return s
		}
	}
	t.Fatal("no free port")
	return ""
}
