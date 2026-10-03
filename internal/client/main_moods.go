package client

import (
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/google/uuid"
)

// The login stage's fun carried into the main window: grape art in empty
// channels, grape-flavoured typing, /grape /disco /mood /vintage
// /collection, little celebrations, quiet hours, and chat achievements.
// All of it is local to this client and off with Surprise Me off.

func (a *App) fullSurprise() bool { return a.surprise() == surpriseFull }

// --- empty channels ----------------------------------------------------------

var emptyQuips = []string{
	"Fresh vines. Plant the first word.",
	"A bare trellis. Say something!",
	"Quiet as a cellar. Say hello!",
	"No messages yet: the grapes are listening.",
	"Nothing pressed here yet.",
	"The first grape of the season is yours.",
	"Uncorked and waiting.",
	"Even vineyards start with one vine.",
}

var emptyBunch = []string{
	"    ,_",
	"   ( \\_",
	"  ● ● ●",
	"   ● ●",
	"    ●",
}

// emptyChannelArt is a little bunch of grapes and a quip for an empty
// channel, the same each time you visit that channel.
func (a *App) emptyChannelArt(channelID uuid.UUID, width int) string {
	c := a.theme.Colors
	n := 0
	for _, b := range channelID {
		n += int(b)
	}
	stem := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Green))
	grape := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Purple))
	var lines []string
	for i, l := range emptyBunch {
		if i < 2 {
			lines = append(lines, stem.Render(l))
		} else {
			lines = append(lines, grape.Render(l))
		}
	}
	art := lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(lipgloss.JoinVertical(lipgloss.Left, lines...))
	quip := lipgloss.NewStyle().Foreground(lipgloss.Color(c.Comment)).Italic(true).Width(width).Align(lipgloss.Center).
		Render(emptyQuips[n%len(emptyQuips)])
	return art + "\n\n" + quip
}

// --- typing ------------------------------------------------------------------

var grapeVerbs = []string{
	"is fermenting a reply", "is pressing some words", "is picking their words",
	"is stomping on the keys", "is letting a thought ripen", "is decanting a message",
}

// typingVerb is a grape verb for about a third of people, steady for the
// day, or "" for the plain "is typing".
func (a *App) typingVerb(uid uuid.UUID) string {
	if !a.fullSurprise() {
		return ""
	}
	day := moodClock().YearDay()
	h := 0
	for _, b := range uid {
		h = h*31 + int(b)
	}
	h = (h + day*7919) & 0x7fffffff
	if h%3 != 0 {
		return ""
	}
	return grapeVerbs[(h/3)%len(grapeVerbs)]
}

// --- commands ----------------------------------------------------------------

// handleGrapeCommand: /grape rains grapes down your own screen.
func (ch *CommandHandler) handleGrapeCommand() (string, error) {
	ch.app.startEgg("rain", 3500*time.Millisecond)
	ch.app.findEgg("slash_grape") // a hidden command: not in /help
	return "🍇", nil
}

// handleDiscoCommand: /disco throws a short party on your own screen.
func (ch *CommandHandler) handleDiscoCommand() (string, error) {
	ch.app.startEgg("party", 6*time.Second)
	ch.app.findEgg("slash_disco")
	return "🪩 " + discoQuips[rng.Intn(len(discoQuips))], nil
}

// handleMoodCommand: /mood shares today's mood in the channel; /mood #CODE
// adopts someone else's for your next launch.
func (ch *CommandHandler) handleMoodCommand(args []string) (string, error) {
	a := ch.app
	if len(args) > 0 {
		m, ok := parseMoodCode(args[0])
		if !ok {
			return "", fmt.Errorf("%q isn't a mood code (they look like #K7Q2M)", args[0])
		}
		if a.uiConfig != nil {
			a.uiConfig.Display.MoodLock = m.code()
			a.saveDisplayConfig()
		}
		name, _ := m.label(moodClock().Year())
		return "Next launch: " + name + " (locked; unlock on Settings > About)", nil
	}
	name, notes := a.mood.label(moodClock().Year())
	return "Shared your mood", a.postToChannel(fmt.Sprintf("🍷 My Concord mood: **%s**. %s Try it: `/mood %s`", name, notes, a.mood.code()))
}

// handleVintageCommand: /vintage describes your time on Concord as a
// tasting note (only you see it).
func (ch *CommandHandler) handleVintageCommand() (string, error) {
	a := ch.app
	c := a.coll()
	days := 1
	if t, err := time.ParseInLocation("2006-01-02", c.FirstLaunch, time.Local); err == nil {
		days = int(moodClock().Sub(t).Hours()/24) + 1
	}
	body := "light"
	switch m := c.Counters["messages"]; {
	case m > 5000:
		body = "full-bodied"
	case m > 1000:
		body = "bold"
	case m > 100:
		body = "medium-bodied"
	}
	voiceH := float64(c.Counters["voice_seconds"]) / 3600
	notes := []string{fmt.Sprintf("%s messages", commas(c.Counters["messages"]))}
	if voiceH >= .1 {
		notes = append(notes, fmt.Sprintf("%.1f hours of voice calls", voiceH))
	}
	if w := c.Counters["game_wins"]; w > 0 {
		notes = append(notes, fmt.Sprintf("%d games won", w))
	}
	notes = append(notes, fmt.Sprintf("%d launches", c.Launches))
	text := fmt.Sprintf("🍷 A %s vintage, %d days in the barrel. Notes of %s, with a lingering finish of %d achievements.",
		body, days, joinAnd(notes), len(c.Achievements))
	a.displayLocalSystemMessage(text)
	return "", nil
}

// handleCollectionCommand: /collection posts your collection to the channel.
func (ch *CommandHandler) handleCollectionCommand() (string, error) {
	a := ch.app
	c := a.coll()
	parts := []string{fmt.Sprintf("banners %d/%d", len(c.Banners), len(banners))}
	for _, l := range moodLayers {
		parts = append(parts, fmt.Sprintf("%s %d/%d", strings.ToLower(l.name), c.seenCount(l.id), len(l.options)))
	}
	rarest := ""
	for _, l := range moodLayers {
		for _, o := range l.options {
			if _, seen := c.Seen[string(l.id)][o.id]; seen && o.rarity == legendary {
				rarest = o.name + " (legendary)"
			}
		}
	}
	text := fmt.Sprintf("🍇 My Concord collection: %s · %d/%d achievements · %d easter eggs",
		strings.Join(parts, " · "), len(c.Achievements), len(achievements), len(c.Eggs))
	if rarest != "" {
		text += " · rarest find: " + rarest
	}
	return "Shared your collection", a.postToChannel(text)
}

// postToChannel sends a message to the channel on screen.
func (a *App) postToChannel(text string) error {
	if a.activeConn == nil || a.currentChannel == nil || a.currentClientServer == nil {
		return fmt.Errorf("open a channel first")
	}
	a.onMessageSent(a.currentClientServer.ID)
	return a.connMgr.SendMessage(a.currentClientServer.ID, a.currentChannel.ID, text, nil)
}

func joinAnd(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " and " + items[len(items)-1]
}

// --- celebrations and chat achievements ----------------------------------------

// onMessageSent counts a message you sent, for achievements and the odd
// celebration.
func (a *App) onMessageSent(serverID uuid.UUID) {
	c := a.coll()
	n := a.count("messages")
	if n == 1 {
		a.unlock("first_words")
	}
	switch n {
	case 100:
		a.unlock("msgs_100")
	case 1000:
		a.unlock("msgs_1000")
		a.celebrateHere()
	case 10000:
		a.unlock("msgs_10000")
		a.celebrateHere()
	}
	if h := moodClock().Hour(); h >= 2 && h < 5 {
		a.unlock("night_shift")
	}
	key := "server:" + serverID.String()
	if c.Counters[key] == 0 && a.fullSurprise() {
		name := "this server"
		if s := a.serverByID(serverID); s != nil {
			name = s.Name
		}
		a.toasts = append(a.toasts, &toast{label: "🎉 First words", title: "Your first message on " + name})
		a.celebrateHere()
	}
	a.count(key)
}

// celebrateHere throws confetti on your own screen.
func (a *App) celebrateHere() {
	if a.fullSurprise() {
		a.egg = &eggState{kind: "confetti", start: time.Now(), dur: 2500 * time.Millisecond, seed: rng.Uint64()}
	}
}

// onVoiceJoin notices someone joining voice for the first time this
// client has seen, once a baseline exists (so an upgrade doesn't greet
// everyone at once).
func (a *App) onVoiceJoin(userID uuid.UUID, name string, live bool) {
	c := a.coll()
	if c.VoiceSeen == nil {
		c.VoiceSeen = map[string]string{}
	}
	id := userID.String()
	if _, seen := c.VoiceSeen[id]; seen {
		return
	}
	baseline := len(c.VoiceSeen) > 0
	c.VoiceSeen[id] = today()
	a.saveCollection()
	if live && baseline && a.fullSurprise() && name != "" {
		a.toasts = append(a.toasts, &toast{label: "🎙 New voice", title: name + " is in voice for the first time"})
	}
}

// voiceTick adds time spent in voice (from the 30-second idle check).
func (a *App) voiceTick(d time.Duration) {
	if a.voiceEngine == nil {
		return
	}
	c := a.coll()
	c.Counters["voice_seconds"] += int(d.Seconds())
	s := c.Counters["voice_seconds"]
	for _, t := range []struct {
		secs int
		id   string
	}{{3600, "voice_1"}, {36000, "voice_10"}, {360000, "voice_100"}} {
		if s >= t.secs {
			a.unlock(t.id)
		}
	}
	a.saveCollection()
}

// channelBirthday wishes a channel happy birthday on the day it was made,
// once a day.
func (a *App) channelBirthday(ch *models.Channel) {
	if ch == nil || !a.fullSurprise() || ch.CreatedAt.IsZero() {
		return
	}
	now := moodClock()
	years := now.Year() - ch.CreatedAt.Year()
	if years < 1 || now.Month() != ch.CreatedAt.Month() || now.Day() != ch.CreatedAt.Day() {
		return
	}
	key := "birthday:" + ch.ID.String() + ":" + today()
	if a.coll().Counters[key] > 0 {
		return
	}
	a.count(key)
	a.toasts = append(a.toasts, &toast{label: "🎂 Happy birthday", title: fmt.Sprintf("#%s is %d year%s old today", ch.Name, years, plural(years))})
	a.celebrateHere()
}

// quietHours reports the small hours, when the status bar shows a moon.
func (a *App) quietHours() bool {
	h := moodClock().Hour()
	return h >= 1 && h < 5 && a.fullSurprise()
}

// --- the main window's own eggs --------------------------------------------------

// eggRain: grapes falling down the screen (/grape).
func eggRain(g *fxGrid, t float64, seed uint64) {
	for i := 0; i < g.w*g.h/60; i++ {
		x := int(cellHash(seed, i, 40, 1)*float64(g.w/2)) * 2
		y := t*(10+cellHash(seed, i, 40, 2)*14) - cellHash(seed, i, 40, 3)*float64(g.h)
		if y >= 0 && y < float64(g.h) {
			g.set(int(y), x, "🍇", "", 2)
		}
	}
}

// eggParty: the disco comes to the main window (/disco): coloured spots
// sweeping round, a mirror ball in the corner.
func eggParty(g *fxGrid, t float64, pal loadingPalette) {
	discoReflections(g, t, pal)
	ball := []string{" ▄██▄ ", "██████", " ▀██▀ "}
	cols := []string{pal.pink, pal.cyan, pal.yellow, "#ffffff"}
	for i, l := range ball {
		g.text(1+i, g.w-9, l, sgrFor(cols[(i+int(t*4))%len(cols)], "", true))
	}
}

// onGameResult celebrates a win the moment a game ends. The counting is
// the plugin's: its record (plugin_records.go) feeds the achievements.
func (a *App) onGameResult(p protocol.PluginGameResultPayload) {
	if p.Result == "win" {
		a.celebrateHere()
	}
}

// isSystemDisplay reports a system line in the chat (not a message anyone
// wrote), which message navigation steps over.
func isSystemDisplay(m *MessageDisplay) bool {
	return m != nil && (m.IsSystem || m.AuthorName == "System")
}

// lastSelectable is the newest message navigation can land on, or -1.
func lastSelectable(messages []*MessageDisplay) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if !isSystemDisplay(messages[i]) {
			return i
		}
	}
	return -1
}

// discoQuips: what /disco says in the status bar (only you see the party).
var discoQuips = []string{
	"Party of one. Only you can see this.",
	"It's a personal party. Nobody else was invited.",
	"Dance like nobody's watching. Nobody is.",
	"The VIP list has exactly one name on it.",
	"Your coworkers can't see this. Act natural.",
	"Private disco: no cover charge, no other guests.",
	"The DJ takes requests. The DJ is also you.",
	"Mirror ball rented for one. The grapes are dancing.",
	"Silent disco, extra silent edition.",
	"This party is invisible to everyone but you. Very exclusive.",
	"Someone had to start the party. It was you. Alone.",
	"Grapes on the dance floor, and only you can see them.",
}

// handlePartyCommand: /party (hidden): confetti bursts out of the middle
// of your screen.
func (ch *CommandHandler) handlePartyCommand() (string, error) {
	ch.app.startEgg("confetti_burst", 3500*time.Millisecond)
	ch.app.findEgg("slash_party")
	return "🎉 " + partyQuips[rng.Intn(len(partyQuips))], nil
}

var partyQuips = []string{
	"Is it your birthday?",
	"A very merry unbirthday to you!",
	"Confetti cleanup is your responsibility.",
	"Party! (Only you can see the confetti.)",
	"Somebody bring the grape juice.",
	"You've been to better parties. This one's yours, though.",
	"Celebrating... something. Probably.",
	"No reason. Just vibes.",
	"Hooray for whatever this is!",
	"Party poppers deployed. All of them.",
	"Happy Tuesday! (Or whatever day it is.)",
	"The confetti is free. The cleanup is not.",
}

// eggConfettiBurst: confetti thrown out of the middle of the screen,
// falling back down (/party).
func eggConfettiBurst(g *fxGrid, t float64, seed uint64, pal loadingPalette) {
	colours := []string{pal.pink, pal.purple, pal.cyan, pal.green, pal.yellow, "#ffb86c"}
	flecks := []string{"▪", "▫", "•", "◆", "▴", "✦", "*", "~"}
	cx, cy := float64(g.w)/2, float64(g.h)/2
	for i := 0; i < 160; i++ {
		ang := cellHash(seed, i, 41, 1) * 2 * math.Pi
		speed := 15 + cellHash(seed, i, 41, 2)*50
		x := cx + math.Cos(ang)*speed*t
		y := cy + math.Sin(ang)*speed*t*.45 + 9*t*t // and gravity
		g.set(int(y), int(x), flecks[i%len(flecks)], sgrFor(colours[i%len(colours)], "", true), 1)
	}
}
