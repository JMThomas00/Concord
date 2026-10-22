package table

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/JMThomas00/Concord/sdk/pane"
	"github.com/JMThomas00/Concord/sdk/wire"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/colorprofile"
	"github.com/google/uuid"
)

// A game with an Arcade, run in a terminal (RunLocal), goes through the
// same front door as in Concord: the Kit runs as it does there, with one
// viewer -- the person at this terminal -- in a room of its own, and the
// terminal stands in for Concord. Playing the computer uses the arcade
// table, and games and records (unlockables, the hall of fame) are kept in
// the user's config folder. Two people at one keyboard and network games
// hand off to the plain local table (local.go) and come back to the menu.

// standalone is the terminal program around the Kit.
type standalone struct {
	k       *Kit
	viewer  uuid.UUID
	channel uuid.UUID
	name    string
	options map[string]string // RunLocal's, for the plain local table

	plain *localModel // two players here, or a network game; nil in the arcade
	next  tea.Cmd     // the plain table's first command, once it's opened

	profile       colorprofile.Profile
	width, height int
	quit          bool
}

// postMsg runs work handed to the event loop (Kit.enqueue).
type postMsg func()

// localID is this computer's player, the same every time so their record
// is theirs.
var localID = uuid.NewSHA1(uuid.NameSpaceURL, []byte("concord-table:local-player"))

func newStandalone(rules Rules, options map[string]string, dir string, post func(func())) *standalone {
	k := New(rules)
	k.local, k.dir = post, dir
	s := &standalone{
		k: k, viewer: localID, name: playerName(), options: options,
		channel: uuid.NewSHA1(uuid.NameSpaceURL, []byte("concord-table:"+rules.Name)),
		profile: colorprofile.TrueColor, width: arcadeW, height: arcadeH,
	}
	k.host.OnQuit = func(*pane.Viewer) { s.quit = true }
	k.handOff = s.openPlain
	r := k.room(s.channel)
	r.settings = copyMap(options) // every game starts from RunLocal's options
	k.host.Enter(nil, wire.PluginPaneEnterPayload{
		ChannelID: s.channel, ViewerID: s.viewer, ViewerName: s.name, ViewerDisplayName: s.name,
		Width: s.width, Height: s.height,
	})
	return s
}

// runStandalone plays an arcade game in this terminal.
func runStandalone(rules Rules, options map[string]string) error {
	var p *tea.Program
	ready := make(chan struct{})
	s := newStandalone(rules, options, localDir(rules.Name), func(fn func()) {
		<-ready // (the ticker starts with the title screen, before the program)
		p.Send(postMsg(fn))
	})
	s.profile = colorprofile.Detect(os.Stdout, os.Environ())
	if canPlaySounds() {
		s.k.speaker = newSpeaker(s.k.dir)
	}
	p = tea.NewProgram(s, tea.WithAltScreen())
	close(ready)
	_, err := p.Run()
	if s.plain != nil {
		s.plain.leaveNet()
	}
	return err
}

// localDir is where a standalone game keeps its games and records: the
// user's config folder ("concord-games/tic-tac-toe"), or the working
// folder if there's none.
func localDir(game string) string {
	base, err := os.UserConfigDir()
	if err != nil {
		return "."
	}
	slug := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(game), "-"), "-")
	if slug == "" {
		slug = "game"
	}
	return filepath.Join(base, "concord-games", slug)
}

func (s *standalone) Init() tea.Cmd { return nil }

func (s *standalone) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if s.plain != nil {
		return s, s.plainUpdate(msg)
	}
	switch msg := msg.(type) {
	case postMsg:
		msg()
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
		s.k.host.Resize(nil, wire.PluginPaneResizePayload{ChannelID: s.channel, ViewerID: s.viewer, Width: s.width, Height: s.height})
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return s, tea.Quit
		}
		if cmd, done := s.key(msg); done {
			return s, cmd
		}
		s.k.host.Send(s.viewer, msg)
		s.k.flush()
	}
	if s.quit {
		return s, tea.Quit
	}
	return s, s.takeNext()
}

// key handles what Concord would before a key reaches the pane: Esc, Tab
// and Shift+Tab only go to the game while it claims them (unclaimed, Esc
// leaves -- here, the program), and Enter on the small front door opens
// the plain local table rather than the kit's plain layout, which is made
// for a channel full of people.
func (s *standalone) key(msg tea.KeyMsg) (tea.Cmd, bool) {
	vm := s.k.viewers[s.viewer]
	if vm == nil {
		return tea.Quit, true
	}
	switch key := msg.String(); key {
	case "esc", "tab", "shift+tab":
		claimed := map[string]string{"esc": wire.PaneKeyEsc, "tab": wire.PaneKeyTab, "shift+tab": wire.PaneKeyShiftTab}[key]
		for _, c := range vm.ClaimedKeys() {
			if c == claimed {
				return nil, false
			}
		}
		if key == "esc" {
			return tea.Quit, true
		}
		return nil, true
	case "enter", " ":
		if vm.screen >= screenTitle && !vm.big() {
			s.openPlain(func(*localModel) {}) // its own start menu: every way to play
			return s.takeNext(), true
		}
	}
	return nil, false
}

func (s *standalone) takeNext() tea.Cmd {
	cmd := s.next
	s.next = nil
	return cmd
}

// openPlain hands the terminal to the plain local table; set starts it (a
// game, or a network game being set up), or leaves it on its start menu.
// Leaving it -- its menu's Back, Quit on the start menu -- comes back here.
func (s *standalone) openPlain(set func(p *localModel)) {
	p := newLocal(s.k.rules, s.options)
	p.me = s.name
	p.back = func() { s.plain = nil }
	p.sound = func(move string) {
		k := s.k
		if k.speaker == nil || k.rules.Sound == nil || k.record(s.viewer).Muted {
			return
		}
		if sound := k.rules.Sound(p.t.game, move); sound != "" {
			k.speaker.play(sound)
		}
	}
	p.width, p.height = s.width, s.height
	set(p)
	s.plain, s.next, p.pending = p, p.pending, nil
}

// plainUpdate runs the plain local table, and comes back to the arcade
// when it's left.
func (s *standalone) plainUpdate(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case postMsg: // the arcade's ticker, winding down
		msg()
		return nil
	case tea.WindowSizeMsg: // the arcade needs it too, for later
		s.width, s.height = msg.Width, msg.Height
		s.k.host.Resize(nil, wire.PluginPaneResizePayload{ChannelID: s.channel, ViewerID: s.viewer, Width: s.width, Height: s.height})
	}
	_, cmd := s.plain.Update(msg)
	if s.plain == nil { // back to the arcade
		if vm := s.k.viewers[s.viewer]; vm != nil && vm.screen >= screenTitle && vm.big() {
			vm.goTo(screenMenu)
		}
		return nil
	}
	return cmd
}

func (s *standalone) View() string {
	if s.plain != nil {
		return s.plain.View()
	}
	vm := s.k.viewers[s.viewer]
	if vm == nil {
		return ""
	}
	frame := pane.Fit(vm.View(), s.width, s.height)
	if s.profile != colorprofile.TrueColor {
		var buf bytes.Buffer
		w := &colorprofile.Writer{Forward: &buf, Profile: s.profile}
		_, _ = w.WriteString(frame)
		frame = buf.String()
	}
	return frame
}

// standaloneItems are the arcade menu's ways to play another person
// without Concord: at this keyboard, or over the network (two-player
// games).
func (m *viewerModel) standaloneItems() []arcadeItem {
	k := m.k
	label := "2 PLAYERS"
	if len(k.rules.SeatNames) > 2 {
		label = "PASS AND PLAY"
	}
	items := []arcadeItem{{
		label: label,
		desc:  func() (string, string) { return "TAKE TURNS AT THIS KEYBOARD", "dim" },
		enter: func() {
			m.withSetup(label, true, func(o map[string]string) {
				opts := withOptions(m.room().settings, o)
				seats := k.seatsFor(opts, []Player{{Name: k.rules.SeatNames[0]}}, true, 2)
				for i := range seats {
					if seats[i].Empty() {
						seats[i] = Player{Name: k.rules.SeatNames[i]}
					}
				}
				m.handOff(func(p *localModel) {
					p.options = opts
					p.pending = p.start(seats, -1)
				})
			})
		},
	}}
	if len(k.rules.SeatNames) == 2 {
		items = append(items, arcadeItem{
			label: "NETWORK GAME",
			desc: func() (string, string) {
				if m.join {
					return "◂ JOIN ▸  A GAME ON ANOTHER COMPUTER", "fg"
				}
				return "◂ HOST ▸  A GAME FOR ANOTHER COMPUTER", "fg"
			},
			enter: func() {
				if m.join {
					m.handOff(func(p *localModel) { p.net = &netState{joining: true} })
					return
				}
				m.withSetup("HOST A GAME", false, func(o map[string]string) {
					m.handOff(func(p *localModel) {
						p.options = withOptions(m.room().settings, o)
						p.net = &netState{hosting: true, port: DefaultPort}
						p.pending = hostCmd(DefaultPort)
					})
				})
			},
			turn: func(int) { m.join = !m.join },
		})
	}
	return items
}

// handOff asks the terminal program to open the plain local table (set
// starts it) after the current key.
func (m *viewerModel) handOff(set func(p *localModel)) {
	if m.k.handOff != nil {
		m.k.handOff(set)
	}
}
