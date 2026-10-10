package main

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"os/exec"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/concord-chat/concord/internal/grapes"
	"github.com/concord-chat/concord/internal/installer"
	"github.com/concord-chat/concord/internal/official"
)

type stage int

const (
	stIntro   stage = iota
	stForm          // the questions
	stTerms         // the server's terms, when installing one
	stReview        // everything at a glance: install, change or quit
	stInstall       // the checklist running
	stFailed        // a step failed: retry, change answers or quit
	stParty         // a moment of celebration
	stAfter         // next steps: join the official server, open Concord
	stConfirm       // type "uninstall" to remove everything
)

const (
	frameDur  = 33 * time.Millisecond
	introDur  = 3600 * time.Millisecond
	partyDur  = 2600 * time.Millisecond
	sideWidth = 41 // the grapes' column, with a margin
)

type tickMsg time.Time

func tick() tea.Cmd { return tea.Tick(frameDur, func(t time.Time) tea.Msg { return tickMsg(t) }) }

type model struct {
	w, h    int
	st      stage
	stageAt time.Time
	light   [3]float64
	plan    *installer.Plan

	form       *huh.Form
	notice     string // a line above the form (why we came back to it)
	back       bool   // the last key was shift+tab: answers aren't checked
	terms      viewport.Model
	termsOK    bool // the Accept button is focused
	review     *huh.Form
	reviewPick string
	after      *huh.Form
	join       bool
	openNow    bool
	addedNote  string

	// the install
	runner   *installer.Runner
	steps    []installer.Step
	cur      int
	frac     float64
	detail   string
	logs     []string
	events   chan tea.Msg
	cancel   context.CancelFunc
	failed   error
	failAt   int
	quipAt   time.Time
	quip     string
	confetti []confetto

	aborted bool
	done    bool // every step finished

	mode      string  // modeInstall, modeUpdate, modeConfigure or modeUninstall
	confirmed bool    // uninstall: already confirmed in Concord
	initCmd   tea.Cmd // what the first screen starts with
}

func newModel(plan *installer.Plan, intro bool) *model {
	m := &model{plan: plan, light: grapes.Dark, join: true, openNow: true, w: 100, h: 34, mode: modeInstall}
	if len(m.plan.Components) == 0 {
		m.plan.Components = []string{installer.Client}
	}
	m.st = stIntro
	if !intro {
		m.light = grapes.Light
		m.toForm("")
	}
	m.stageAt = time.Now()
	return m
}

func (m *model) Init() tea.Cmd {
	if m.initCmd != nil {
		return tea.Batch(tick(), m.initCmd)
	}
	if m.form != nil {
		return tea.Batch(tick(), m.form.Init())
	}
	return tick()
}

func (m *model) go_(st stage) {
	m.st = st
	m.stageAt = time.Now()
}

// --- updating -------------------------------------------------------------

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.terms.Width, m.terms.Height = m.contentWidth()-4, m.termsHeight()
	case tickMsg:
		m.animate(time.Time(msg))
		return m, tick()
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			if m.cancel != nil {
				m.cancel()
			}
			m.aborted = m.st != stParty && m.st != stAfter
			return m, tea.Quit
		}
	}
	switch m.st {
	case stIntro:
		if _, ok := msg.(tea.KeyMsg); ok || time.Since(m.stageAt) > introDur {
			return m, m.toForm("")
		}
	case stForm:
		return m.updateForm(msg)
	case stTerms:
		return m.updateTerms(msg)
	case stReview:
		return m.updateReview(msg)
	case stInstall:
		return m.updateInstall(msg)
	case stFailed:
		return m.updateFailed(msg)
	case stParty:
		if _, ok := msg.(tea.KeyMsg); ok || time.Since(m.stageAt) > partyDur {
			return m, m.toAfter()
		}
	case stAfter:
		return m.updateAfter(msg)
	case stConfirm:
		return m.updateConfirm(msg)
	}
	return m, nil
}

// animate moves the light: rising out of the dark at the start, then a
// slow orbit — quicker while installing, as if the grapes were busy.
func (m *model) animate(now time.Time) {
	speed := 1.0
	if m.st == stInstall {
		speed = 2.4
	}
	to := grapes.Orbit(float64(now.UnixMilli()) / 2600 * speed)
	if m.st == stIntro && time.Since(m.stageAt) < 600*time.Millisecond {
		to = grapes.Dark
	}
	m.light = grapes.Ease(m.light, to)
	if m.st == stInstall && now.Sub(m.quipAt) > 2600*time.Millisecond {
		m.quipAt = now
		m.quip = m.nextQuip()
	}
	if m.st == stParty {
		m.stepConfetti()
	}
}

// --- the questions ------------------------------------------------------------

func (m *model) toForm(notice string) tea.Cmd {
	m.notice = notice
	m.form = questions(m.plan, &m.back).form(m.contentWidth()).WithShowHelp(true)
	m.go_(stForm)
	return m.form.Init()
}

func (m *model) updateForm(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Going back skips the answers' checks until the next key (Huh checks
	// on the key, on leaving the field and on leaving the group).
	if k, ok := msg.(tea.KeyMsg); ok {
		m.back = k.String() == "shift+tab"
	}
	f, cmd := m.form.Update(msg)
	m.form = f.(*huh.Form)
	switch m.form.State {
	case huh.StateAborted:
		m.aborted = true
		return m, tea.Quit
	case huh.StateCompleted:
		m.plan.Tidy()
		if m.plan.Has(installer.Server) && !m.plan.Existing(installer.Server) && !m.plan.TermsAccepted {
			m.toTerms()
			return m, nil
		}
		return m, m.toReview()
	}
	return m, cmd
}

// --- the terms ------------------------------------------------------------------

func (m *model) toTerms() {
	m.terms = viewport.New(m.contentWidth()-4, m.termsHeight())
	m.terms.SetContent(renderTerms(m.contentWidth() - 6))
	m.termsOK = true
	m.go_(stTerms)
}

func (m *model) updateTerms(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "left", "right", "tab", "shift+tab", "h", "l":
			m.termsOK = !m.termsOK
			return m, nil
		case "a", "y":
			m.termsOK = true
			fallthrough
		case "enter":
			if m.termsOK {
				m.plan.TermsAccepted = true
				return m, m.toReview()
			}
			return m, m.toForm("A server needs its terms accepted. Untick Server to install without one.")
		case "n", "d", "esc":
			return m, m.toForm("A server needs its terms accepted. Untick Server to install without one.")
		}
	}
	var cmd tea.Cmd
	m.terms, cmd = m.terms.Update(msg)
	return m, cmd
}

// --- the review -------------------------------------------------------------------

func (m *model) toReview() tea.Cmd {
	m.reviewPick = "install"
	b := &formBuilder{}
	b.group(
		huh.NewSelect[string]().
			Title("Ready?").
			Options(
				huh.NewOption("Install 🍇", "install"),
				huh.NewOption("Change something", "change"),
				huh.NewOption("Quit without installing", "quit"),
			).
			Value(&m.reviewPick),
	)
	m.review = b.form(m.contentWidth()).WithShowHelp(false)
	m.go_(stReview)
	return m.review.Init()
}

func (m *model) updateReview(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.review.Update(msg)
	m.review = f.(*huh.Form)
	switch m.review.State {
	case huh.StateAborted:
		m.aborted = true
		return m, tea.Quit
	case huh.StateCompleted:
		switch m.reviewPick {
		case "install":
			return m, m.startInstall()
		case "change":
			return m, m.toForm("")
		}
		m.aborted = true
		return m, tea.Quit
	}
	return m, cmd
}

// --- installing -------------------------------------------------------------------

type (
	stepMsg     int
	progressMsg struct {
		frac   float64
		detail string
	}
	logMsg    string
	failMsg   struct{ err error }
	doneMsg   struct{}
	sudoMsg   struct{ reply chan error }
	sudoReply struct{}
)

func (m *model) startInstall() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	m.cancel = cancel
	m.events = make(chan tea.Msg, 64)
	m.failed, m.logs, m.cur, m.frac, m.detail = nil, nil, 0, 0, ""
	m.quip, m.quipAt = m.nextQuip(), time.Now()
	events := m.events
	r := &installer.Runner{
		Plan:     m.plan,
		Progress: func(f float64, d string) { events <- progressMsg{f, d} },
		Log:      func(l string) { events <- logMsg(l) },
		Sudo:     sudoAsker(events),
	}
	m.runner = r
	m.steps = r.Steps()
	if m.mode == modeUninstall {
		m.steps = r.UninstallSteps()
	}
	m.go_(stInstall)
	go func() {
		for i, s := range m.steps {
			events <- stepMsg(i)
			if err := s.Run(ctx); err != nil {
				events <- failMsg{err}
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
		events <- doneMsg{}
	}()
	return m.listen()
}

func (m *model) listen() tea.Cmd {
	ch := m.events
	return func() tea.Msg { return <-ch }
}

// sudoAsker lets the installer ask for your password the first time it
// needs it, handing the terminal to sudo for a moment.
func sudoAsker(events chan tea.Msg) func() error {
	ok := false
	return func() error {
		if ok || exec.Command("sudo", "-n", "true").Run() == nil {
			ok = true
			return nil
		}
		reply := make(chan error)
		events <- sudoMsg{reply}
		if err := <-reply; err != nil {
			return fmt.Errorf("sudo didn't accept the password, so that part couldn't be done")
		}
		ok = true
		return nil
	}
}

func (m *model) updateInstall(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case stepMsg:
		m.cur, m.frac, m.detail = int(msg), 0, ""
	case progressMsg:
		m.frac, m.detail = msg.frac, msg.detail
	case logMsg:
		m.logs = append(m.logs, string(msg))
	case sudoMsg:
		c := exec.Command("sudo", "-p", "\n  🍇 Concord needs your password to set things up (sudo): ", "-v")
		return m, tea.ExecProcess(c, func(err error) tea.Msg {
			msg.reply <- err
			return sudoReply{}
		})
	case failMsg:
		m.failed, m.failAt = msg.err, m.cur
		m.go_(stFailed)
		return m, nil
	case doneMsg:
		m.cur, m.done = len(m.steps), true
		if m.mode == modeUninstall {
			return m, tea.Quit
		}
		m.startConfetti()
		m.go_(stParty)
		return m, nil
	case sudoReply:
	default:
		return m, nil // not from the install: keep the one listener
	}
	return m, m.listen()
}

func (m *model) updateFailed(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "r", "enter":
			return m, m.startInstall()
		case "c", "b":
			m.failed = nil
			return m, m.toForm("Change what you need to, then install again.")
		case "q", "esc":
			return m, tea.Quit
		}
	}
	return m, nil
}

// --- afterwards ---------------------------------------------------------------------

func (m *model) clientReady() bool {
	return m.runner != nil && contains(m.runner.Result.Installed, installer.Client)
}

func (m *model) toAfter() tea.Cmd {
	if !m.clientReady() || m.mode != modeInstall {
		m.openNow = false
		return tea.Quit
	}
	b := &formBuilder{}
	b.group(
		huh.NewConfirm().
			Title("Join the official Concord server?").
			Description(fmt.Sprintf("%s: where the Concord community hangs out.\nNews, help, and people to play games with.\nIt'll be waiting in your server list.", official.ServerHost)).
			Affirmative("Yes, count me in").Negative("Not now").
			Value(&m.join),
		huh.NewConfirm().
			Title("Open Concord now?").
			Description("It'll ask who you are first: a name, your email and\na password. That one profile signs you in to every\nserver you join.").
			Affirmative("Let's go").Negative("Later").
			Value(&m.openNow),
	)
	m.after = b.form(m.contentWidth()).WithShowHelp(true)
	m.go_(stAfter)
	return m.after.Init()
}

func (m *model) updateAfter(msg tea.Msg) (tea.Model, tea.Cmd) {
	f, cmd := m.after.Update(msg)
	m.after = f.(*huh.Form)
	switch m.after.State {
	case huh.StateAborted:
		m.openNow = false
		return m, tea.Quit
	case huh.StateCompleted:
		m.addServers()
		return m, tea.Quit
	}
	return m, cmd
}

// addServers puts the official server (if wanted) and a server installed
// alongside into the client's list.
func (m *model) addServers() {
	var list []installer.ClientServer
	if m.join {
		list = append(list, installer.ClientServer{Name: official.ServerName, Address: official.ServerHost, Port: official.ServerPort, TLS: official.ServerTLS})
	}
	if contains(m.runner.Result.Installed, installer.Server) {
		list = append(list, installer.ClientServer{Name: m.plan.ServerName, Address: "localhost", Port: m.plan.ServerPortNum()})
	}
	if len(list) == 0 || m.plan.DryRun {
		return
	}
	if _, err := installer.AddClientServers(installer.ClientConfigDir(), list); err != nil {
		m.addedNote = "Couldn't add servers to your list: " + err.Error()
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// --- confetti -------------------------------------------------------------------------

type confetto struct {
	x, y, vx, vy float64
	ch           string
	col          string
}

func (m *model) startConfetti() {
	m.confetti = nil
	cols := []string{cPurple, cPink, cGreen, cOrange, cYellow, cCyan}
	glyphs := []string{"🍇", "✦", "•", "✶", "◆", "·", "🍇", "*"}
	for i := 0; i < 90; i++ {
		a := rand.Float64() * math.Pi
		v := 1.2 + rand.Float64()*2.2
		m.confetti = append(m.confetti, confetto{
			x: float64(m.w) / 2, y: float64(m.h) * .55,
			vx: math.Cos(a) * v * 2, vy: -math.Sin(a) * v,
			ch: glyphs[rand.Intn(len(glyphs))], col: cols[rand.Intn(len(cols))],
		})
	}
}

func (m *model) stepConfetti() {
	for i := range m.confetti {
		c := &m.confetti[i]
		c.x += c.vx
		c.y += c.vy
		c.vy += .09
		c.vx *= .97
	}
}
