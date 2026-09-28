package pane_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/plugin"
	"github.com/JMThomas00/Concord/sdk/plugintest"
	"github.com/JMThomas00/Concord/sdk/wire"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/google/uuid"
)

// moveMsg is the "shared state changed" message the test broadcasts.
type moveMsg struct{ move string }

// board is a small model: a text field for the viewer's move, the last
// move anyone made, and its size. Esc quits (hands keys back).
type board struct {
	name   string
	input  textinput.Model
	last   string
	width  int
	height int
}

func newBoard(v *pane.Viewer) tea.Model {
	ti := textinput.New()
	ti.Placeholder = "your move"
	return &board{name: v.DisplayName, input: ti}
}

func (b *board) Init() tea.Cmd { return b.input.Focus() }

func (b *board) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		b.width, b.height = msg.Width, msg.Height
	case moveMsg:
		b.last = msg.move
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return b, tea.Quit
		}
	}
	var cmd tea.Cmd
	b.input, cmd = b.input.Update(msg)
	return b, cmd
}

func (b *board) View() string {
	red := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("●")
	return fmt.Sprintf("%s %s last=%s size=%dx%d\n> %s", red, b.name, b.last, b.width, b.height, b.input.Value())
}

func start(t *testing.T, host *pane.Host) *plugintest.Server {
	t.Helper()
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go plugin.Run(ctx, srv.Config(), host.Handler())
	srv.WaitReady()
	return srv
}

func TestTypingIntoATextInputIsFast(t *testing.T) {
	srv := start(t, pane.NewHost(newBoard))
	alice := srv.Enter(uuid.New(), "alice", 60, 5)
	srv.FrameContaining(alice, "alice")

	began := time.Now()
	srv.Type(alice, "e2e4")
	srv.FrameContaining(alice, "> e2e4")
	// Cursor blinking would add ~530ms per keystroke if its command ran.
	if took := time.Since(began); took > time.Second {
		t.Fatalf("typing 4 keys took %v; cursor blink commands are being run", took)
	}
}

func TestBroadcastRerendersEveryViewerOfTheChannel(t *testing.T) {
	host := pane.NewHost(newBoard)
	h := host.Handler()
	// A custom event lets the test ask for a broadcast from the handler goroutine.
	h.OnEvent = func(c *plugin.Conn, e wire.PluginEventPayload) {
		var channel uuid.UUID
		if json.Unmarshal(e.Payload, &channel) == nil {
			host.Broadcast(channel, moveMsg{move: e.Kind})
		}
	}
	srv := plugintest.NewServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go plugin.Run(ctx, srv.Config(), h)
	srv.WaitReady()

	table, other := uuid.New(), uuid.New()
	alice := srv.Enter(table, "alice", 60, 5)
	bob := srv.Enter(table, "bob", 60, 5)
	carol := srv.Enter(other, "carol", 60, 5)
	for _, v := range []*plugintest.Viewer{alice, bob, carol} {
		srv.NextFrame(v)
	}
	srv.Event("Nf3", table)
	srv.FrameContaining(alice, "last=Nf3")
	srv.FrameContaining(bob, "last=Nf3")
	if strings.Contains(carol.LastFrame, "Nf3") {
		t.Fatal("a viewer of another channel was sent the move")
	}
}

func TestQuitHandsKeysBackAndResizeReflows(t *testing.T) {
	srv := start(t, pane.NewHost(newBoard))
	alice := srv.Enter(uuid.New(), "alice", 60, 5)
	srv.FrameContaining(alice, "size=60x5")
	srv.Resize(alice, 80, 10)
	srv.FrameContaining(alice, "size=80x10")
	srv.Key(alice, "esc")
	if e := srv.NextEvent(); e.Kind != wire.PluginEventLeavePane || e.ViewerID != alice.ID {
		t.Fatalf("tea.Quit should hand alice's keys back, got %+v", e)
	}
}

func TestFramesFitThePaneAndMatchTheViewersColors(t *testing.T) {
	srv := start(t, pane.NewHost(newBoard))
	narrow := srv.Enter(uuid.New(), "a-very-long-display-name", 10, 1)
	f := srv.NextFrame(narrow)
	if strings.Contains(f, "\n") {
		t.Fatalf("frame taller than the 1-line pane: %q", f)
	}
	// plugintest viewers report "ansi256": the true-color red is converted.
	if strings.Contains(f, "38;2;255;0;0") {
		t.Fatalf("true-color escape sent to a 256-color viewer: %q", f)
	}
	if !strings.Contains(f, "\x1b[") {
		t.Fatalf("colors were dropped entirely: %q", f)
	}
}

func TestKeyMsgRebuildsBubbleTeaKeys(t *testing.T) {
	for key, want := range map[string]tea.KeyMsg{
		"enter":    {Type: tea.KeyEnter},
		"up":       {Type: tea.KeyUp},
		"ctrl+c":   {Type: tea.KeyCtrlC},
		"alt+left": {Type: tea.KeyLeft, Alt: true},
		"a":        {Type: tea.KeyRunes, Runes: []rune("a")},
		"alt+x":    {Type: tea.KeyRunes, Runes: []rune("x"), Alt: true},
		" ":        {Type: tea.KeySpace, Runes: []rune(" ")},
		"esc":      {Type: tea.KeyEsc},
		"tab":      {Type: tea.KeyTab},
	} {
		got := pane.KeyMsg(wire.PluginPaneInputPayload{KeyString: key})
		if got.String() != want.String() || got.Type != want.Type || got.Alt != want.Alt {
			t.Errorf("KeyMsg(%q) = %#v (%q), want %#v (%q)", key, got, got.String(), want, want.String())
		}
	}
}

func TestFit(t *testing.T) {
	colored := "\x1b[31mhello world\x1b[0m"
	got := pane.Fit(colored+"\nline2\nline3\n", 5, 2)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "\x1b[31mhello") || strings.Contains(lines[0], "world") {
		t.Fatalf("Fit = %q", got)
	}
}

// bumpModel counts bumps; pressing "b" broadcasts one from inside its own
// Update -- which the Host must queue, not re-enter.
type bumpModel struct {
	bumps  int
	onBump func()
}

type bumpMsg struct{}

func (m *bumpModel) Init() tea.Cmd { return nil }
func (m *bumpModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case bumpMsg:
		m.bumps++
	case tea.KeyMsg:
		if msg.String() == "b" {
			m.onBump()
		}
	}
	return m, nil
}
func (m *bumpModel) View() string { return fmt.Sprintf("bumps=%d", m.bumps) }

func TestBroadcastFromInsideUpdateIsQueued(t *testing.T) {
	var host *pane.Host
	host = pane.NewHost(func(v *pane.Viewer) tea.Model {
		return &bumpModel{onBump: func() { host.Broadcast(v.ChannelID, bumpMsg{}) }}
	})
	srv := start(t, host)
	ch := uuid.New()
	a, b := srv.Enter(ch, "a", 20, 3), srv.Enter(ch, "b", 20, 3)
	srv.NextFrame(a)
	srv.NextFrame(b)
	srv.Key(a, "b")
	srv.FrameContaining(a, "bumps=1")
	srv.FrameContaining(b, "bumps=1")
	srv.Key(b, "b")
	srv.FrameContaining(a, "bumps=2")
	srv.FrameContaining(b, "bumps=2")
}
