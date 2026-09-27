package client

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/protocol"
)

// pump feeds msg to Update and then runs returned commands the way Bubble
// Tea does (batches expanded, results fed back), for up to d.
func pump(t *testing.T, a *App, msg tea.Msg, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	queue := []tea.Msg{msg}
	for len(queue) > 0 && time.Now().Before(deadline) {
		m := queue[0]
		queue = queue[1:]
		_, cmd := a.Update(m)
		a.View()
		var run func(c tea.Cmd)
		run = func(c tea.Cmd) {
			if c == nil {
				return
			}
			done := make(chan tea.Msg, 1)
			go func() { done <- c() }()
			select {
			case out := <-done:
				if b, ok := out.(tea.BatchMsg); ok {
					for _, sub := range b {
						run(sub)
					}
					return
				}
				if out != nil {
					queue = append(queue, out)
				}
			case <-time.After(400 * time.Millisecond):
			}
		}
		run(cmd)
	}
}

func TestPaneEnterSentThroughRealUpdateLoop(t *testing.T) {
	a, conn, _ := paneTestApp(t)
	a.focus = FocusChannelList
	pump(t, a, tea.WindowSizeMsg{Width: 160, Height: 45}, 3*time.Second)
	for _, m := range sent(conn) {
		if m.Op == protocol.OpPluginPaneEnter {
			return
		}
	}
	t.Fatal("no Enter was ever sent through the real Update loop")
}
