package client

import (
	"hash/fnv"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// A mood is one launch's look for the login stage: a pick from each
// decoration layer (loading screen, transitions, background, logo style,
// banner colouring...). The layout never changes; only these do, so there
// are always combinations nobody has seen yet. Each mood has a short code
// (#K7Q2M) that reproduces it: shown in Settings > About, where it can be
// locked or rerolled.

type moodLayer string

const (
	layerLoading    moodLayer = "loading"
	layerTransition moodLayer = "transition"
	layerAtmosphere moodLayer = "atmosphere"
	layerLogo       moodLayer = "logo"
	layerBanner     moodLayer = "banner_colour"
	layerLight      moodLayer = "light"
	layerAccent     moodLayer = "accent"
)

type rarity int

const (
	common rarity = iota
	rare
	legendary
)

func (r rarity) String() string {
	return [...]string{"common", "rare", "legendary"}[r]
}

// Odds per layer: a legendary option 1 in legendaryOdds, a rare one
// 1 in rareOdds, otherwise a common one.
const (
	legendaryOdds = 500
	rareOdds      = 10
)

type moodOption struct {
	id     string
	name   string
	rarity rarity
}

type layerInfo struct {
	id      moodLayer
	name    string // for Settings > About
	options []moodOption
}

// moodLayers is every layer and its options, in display order. Adding an
// option or a layer never changes what existing codes pick in other layers
// (each layer draws from its own stream), only possibly in its own.
var moodLayers = []layerInfo{
	{layerLoading, "Loading screens", []moodOption{
		{"matrix", "Grape rain", common},
		{"lava", "Lava lamp", common},
		{"bios", "640K grapes OK", common},
		{"teletext", "Teletext", common},
		{"crt", "CRT warm-up", common},
		{"calm", "Calm", common},
		{"bbs", "BBS dial-up", rare},
	}},
	{layerTransition, "Page transitions", []moodOption{
		{"slide", "Slide and settle", common},
		{"decode", "Decode", common},
		{"teletext", "Teletext rows", common},
		{"baud", "2400 baud", common},
		{"crt", "Channel change", rare},
		{"plasma", "Plasma wipe", rare},
	}},
	{layerAtmosphere, "Backgrounds", []moodOption{
		{"grapes", "Drifting grapes", common},
		{"stars", "Starfield", common},
		{"dots", "Dot grid", common},
		{"leaves", "Falling leaves", common},
		{"none", "Plain", common},
		{"mosaic", "Teletext mosaic", rare},
		{"plasma", "Plasma", rare},
		{"lava", "Lava strip", rare},
	}},
	{layerLogo, "Grape styles", []moodOption{
		{"shaded", "Shaded", common},
		{"pixel", "Pixel", common},
		{"braille", "Braille", common},
		{"dotmatrix", "Dot matrix", rare},
		{"wireframe", "Wireframe", legendary},
		{"golden", "Golden grapes", legendary},
	}},
	{layerBanner, "Banner colours", []moodOption{
		{"solid", "Solid", common},
		{"gradient", "Gradient", common},
		{"shimmer", "Shimmer", common},
		{"bbs", "BBS shaded", rare},
		{"green", "Green phosphor", rare},
		{"rainbow", "Rainbow", rare},
		{"amber", "Amber phosphor", legendary},
	}},
	{layerLight, "Grape lights", []moodOption{
		{"orbit", "Orbit", common},
		{"breathe", "Breathe", common},
		{"disco", "Disco", rare},
	}},
	{layerAccent, "Frame accents", []moodOption{
		{"none", "None", common},
		{"hairlines", "Hairlines", common},
		{"brackets", "HUD brackets", common},
	}},
}

func findLayer(id moodLayer) *layerInfo {
	for i := range moodLayers {
		if moodLayers[i].id == id {
			return &moodLayers[i]
		}
	}
	return nil
}

func findOption(layer moodLayer, id string) (moodOption, bool) {
	if l := findLayer(layer); l != nil {
		for _, o := range l.options {
			if o.id == id {
				return o, true
			}
		}
	}
	return moodOption{}, false
}

// moodCodeBits is how much of a seed a code carries: 5 characters of
// Crockford base32.
const moodCodeBits = 25

type mood struct {
	seed  uint32
	picks map[moodLayer]string
}

// moodFromSeed works out every layer's pick for a seed.
func moodFromSeed(seed uint32) mood {
	seed &= 1<<moodCodeBits - 1
	m := mood{seed: seed, picks: map[moodLayer]string{}}
	for _, l := range moodLayers {
		m.picks[l.id] = pickOption(seed, l)
	}
	return m
}

func pickOption(seed uint32, l layerInfo) string {
	h := fnv.New64a()
	h.Write([]byte(l.id))
	r := rand.New(rand.NewPCG(uint64(seed), h.Sum64()))
	tier := common
	switch {
	case r.IntN(legendaryOdds) == 0:
		tier = legendary
	case r.IntN(rareOdds) == 0:
		tier = rare
	}
	var choices []string
	for ; len(choices) == 0; tier-- {
		for _, o := range l.options {
			if o.rarity == tier {
				choices = append(choices, o.id)
			}
		}
		if tier == common {
			break
		}
	}
	return choices[r.IntN(len(choices))]
}

// randomMood is a fresh mood for this launch.
func randomMood() mood {
	return moodFromSeed(rand.Uint32())
}

const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// code is the mood's shareable code, "#" plus five characters.
func (m mood) code() string {
	var b [5]byte
	s := m.seed
	for i := 4; i >= 0; i-- {
		b[i] = crockford[s&31]
		s >>= 5
	}
	return "#" + string(b[:])
}

// parseMoodCode reads a code back, forgiving case, a missing "#", and the
// letters Crockford base32 treats as look-alikes (O, I, L).
func parseMoodCode(code string) (mood, bool) {
	code = strings.ToUpper(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(code), "#")))
	if len(code) != 5 {
		return mood{}, false
	}
	code = strings.NewReplacer("O", "0", "I", "1", "L", "1").Replace(code)
	var seed uint32
	for _, c := range code {
		i := strings.IndexRune(crockford, c)
		if i < 0 {
			return mood{}, false
		}
		seed = seed<<5 | uint32(i)
	}
	return moodFromSeed(seed), true
}

// surpriseLevel is Settings > Display > Surprise Me.
type surpriseLevel int

const (
	surpriseFull surpriseLevel = iota
	surpriseCalm               // no backgrounds, gentle transitions and loading
	surpriseOff                // none of it: the login stage stands still
)

var surpriseNames = []string{"", "calm", "off"}

func nextSurprise(cur string) string {
	for i, n := range surpriseNames {
		if n == cur {
			return surpriseNames[(i+1)%len(surpriseNames)]
		}
	}
	return surpriseNames[0]
}

func surpriseLabel(cur string) string {
	switch cur {
	case "calm":
		return "Calm: no backgrounds, gentle transitions"
	case "off":
		return "Off: the login screen stands still"
	}
	return "Full: a new look every launch, and the odd surprise"
}

func (a *App) surprise() surpriseLevel {
	if a.uiConfig == nil {
		return surpriseFull
	}
	lvl := surpriseFull
	switch a.uiConfig.Display.Surprise {
	case "calm":
		lvl = surpriseCalm
	case "off":
		lvl = surpriseOff
	}
	// Turning panel animations off means nothing moves anywhere.
	if a.uiConfig.Display.DisablePanelAnimations {
		lvl = surpriseOff
	}
	return lvl
}

// noColour reports a terminal (or NO_COLOR) that shows no colour at all,
// where backgrounds and colourings would only be noise.
func noColour() bool {
	return lipgloss.ColorProfile() == termenv.Ascii
}

// pick is what this launch shows for a layer, after the Surprise Me
// setting and the terminal have had their say.
func (a *App) pick(layer moodLayer) string {
	id := a.mood.picks[layer]
	lvl := a.surprise()
	if noColour() && lvl == surpriseFull {
		lvl = surpriseCalm
	}
	switch lvl {
	case surpriseCalm:
		switch layer {
		case layerAtmosphere:
			return "none"
		case layerTransition:
			return "slide"
		case layerLoading:
			return "calm"
		case layerLight:
			if id == "disco" {
				return "orbit"
			}
		}
	case surpriseOff:
		switch layer {
		case layerAtmosphere, layerTransition, layerLoading:
			return ""
		case layerLogo:
			return "shaded"
		case layerBanner:
			return "solid"
		case layerLight:
			return "orbit"
		case layerAccent:
			return "none"
		}
	}
	return id
}

// startMood chooses this launch's mood: the locked one if there is one.
func (a *App) startMood() {
	if a.uiConfig != nil && a.uiConfig.Display.MoodLock != "" {
		if m, ok := parseMoodCode(a.uiConfig.Display.MoodLock); ok {
			a.mood = m
			return
		}
	}
	a.mood = randomMood()
}

// rerollMood picks a new mood now (Settings > About), unlocking it.
func (a *App) rerollMood() {
	a.mood = randomMood()
	if a.uiConfig != nil && a.uiConfig.Display.MoodLock != "" {
		a.uiConfig.Display.MoodLock = ""
		a.saveDisplayConfig()
	}
	a.recordMood()
}

// toggleMoodLock keeps the current mood for every launch, or stops keeping it.
func (a *App) toggleMoodLock() {
	if a.uiConfig == nil {
		return
	}
	if a.uiConfig.Display.MoodLock != "" {
		a.uiConfig.Display.MoodLock = ""
	} else {
		a.uiConfig.Display.MoodLock = a.mood.code()
		a.unlock("homebody")
	}
	a.saveDisplayConfig()
}

// recordMood adds what the login stage shows in this mood to the
// collection. Loading screens and transitions are added when they play.
func (a *App) recordMood() {
	for _, l := range moodLayers {
		if l.id == layerLoading || l.id == layerTransition {
			continue // recorded when they play
		}
		if id := a.pick(l.id); id != "" {
			a.discover(l.id, id)
		}
	}
}

// moodClock is time.Now, replaceable in tests (the calendar and time-of-day
// touches read it).
var moodClock = time.Now
