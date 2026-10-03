package client

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// The collection is what this computer has discovered of the login
// experience: each layer's options, banners, easter eggs and achievements,
// with the date each was first seen. It's shown on Settings > About and
// kept in ~/.concord/collection.json. Nothing is sent anywhere.
type Collection struct {
	Version      int                          `json:"version"`
	Launches     int                          `json:"launches"`
	Seen         map[string]map[string]string `json:"seen"`    // layer → option → date
	Banners      []int                        `json:"banners"` // sorted indexes into banners
	Eggs         map[string]string            `json:"eggs"`
	Achievements map[string]string            `json:"achievements"`
	Counters     map[string]int               `json:"counters"`

	// Launch days: the first, the last, and the run of consecutive days.
	FirstLaunch string `json:"first_launch,omitempty"`
	LastDay     string `json:"last_day,omitempty"`
	Streak      int    `json:"streak,omitempty"`
	BestStreak  int    `json:"best_streak,omitempty"`

	// VoiceSeen: people this client has seen in voice, for "first time in
	// voice" (user ID → date).
	VoiceSeen map[string]string `json:"voice_seen,omitempty"`

	// Cellar: every legendary seen, with the mood it came in (About).
	Cellar []CellarBottle `json:"cellar,omitempty"`
}

func newCollection() *Collection {
	return &Collection{
		Version:      1,
		Seen:         map[string]map[string]string{},
		Eggs:         map[string]string{},
		Achievements: map[string]string{},
		Counters:     map[string]int{},
	}
}

func collectionPath(cm *ConfigManager) string {
	if cm == nil || cm.configFilePath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cm.configFilePath), "collection.json")
}

// loadCollection reads the collection, or starts an empty one.
func loadCollection(path string) *Collection {
	c := newCollection()
	if path == "" {
		return c
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c
	}
	if json.Unmarshal(data, c) != nil {
		return newCollection()
	}
	if c.Seen == nil {
		c.Seen = map[string]map[string]string{}
	}
	if c.Eggs == nil {
		c.Eggs = map[string]string{}
	}
	if c.Achievements == nil {
		c.Achievements = map[string]string{}
	}
	if c.Counters == nil {
		c.Counters = map[string]int{}
	}
	return c
}

func (c *Collection) save(path string) {
	if path == "" {
		return
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if os.WriteFile(tmp, data, 0644) == nil {
		if os.Rename(tmp, path) != nil {
			os.Remove(tmp)
		}
	}
}

func today() string { return moodClock().Format("2006-01-02") }

// see records an option as seen; true when it's new.
func (c *Collection) see(layer moodLayer, id string) bool {
	m := c.Seen[string(layer)]
	if m == nil {
		m = map[string]string{}
		c.Seen[string(layer)] = m
	}
	if _, ok := m[id]; ok {
		return false
	}
	m[id] = today()
	return true
}

// seenCount is how many of a layer's options have been seen.
func (c *Collection) seenCount(layer moodLayer) int {
	l := findLayer(layer)
	if l == nil {
		return 0
	}
	n := 0
	for _, o := range l.options {
		if _, ok := c.Seen[string(layer)][o.id]; ok {
			n++
		}
	}
	return n
}

// seeBanner records a banner as seen; true when it's new.
func (c *Collection) seeBanner(idx int) bool {
	i := sort.SearchInts(c.Banners, idx)
	if i < len(c.Banners) && c.Banners[i] == idx {
		return false
	}
	c.Banners = append(c.Banners, 0)
	copy(c.Banners[i+1:], c.Banners[i:])
	c.Banners[i] = idx
	return true
}

func (c *Collection) seeEgg(id string) bool {
	if _, ok := c.Eggs[id]; ok {
		return false
	}
	c.Eggs[id] = today()
	return true
}

// coll is the app's collection, loaded on first use.
func (a *App) coll() *Collection {
	if a.collection == nil {
		a.collection = loadCollection(collectionPath(a.configMgr))
	}
	return a.collection
}

func (a *App) saveCollection() {
	a.coll().save(collectionPath(a.configMgr))
}

// discover adds a layer option to the collection, and checks the
// achievements that depend on it.
func (a *App) discover(layer moodLayer, id string) {
	c := a.coll()
	if !c.see(layer, id) {
		return
	}
	if o, ok := findOption(layer, id); ok && o.rarity == legendary {
		a.unlock("legendary")
	}
	if l := findLayer(layer); l != nil && c.seenCount(layer) == len(l.options) {
		switch layer {
		case layerLoading:
			a.unlock("all_loading")
		case layerLogo:
			a.unlock("all_logos")
		case layerAtmosphere:
			a.unlock("all_backgrounds")
		}
	}
	a.saveCollection()
}

// discoverBanner adds the banner on screen to the collection.
func (a *App) discoverBanner(idx int) {
	c := a.coll()
	if idx < 0 || idx >= len(banners) || !c.seeBanner(idx) {
		return
	}
	switch len(c.Banners) {
	case 50:
		a.unlock("banners_50")
	case 150:
		a.unlock("banners_150")
	case len(banners):
		a.unlock("banners_all")
	}
	a.saveCollection()
}

// findEgg records an easter egg; true when it's the first time.
func (a *App) findEgg(id string) bool {
	if !a.coll().seeEgg(id) {
		return false
	}
	a.unlock("egg_" + id)
	if len(a.coll().Eggs) == 5 {
		a.unlock("egg_hunter")
	}
	a.saveCollection()
	return true
}

// count bumps a counter and returns its new value.
func (a *App) count(name string) int {
	c := a.coll()
	c.Counters[name]++
	a.saveCollection()
	return c.Counters[name]
}

// launched records a launch.
func (a *App) launched() {
	c := a.coll()
	c.Launches++
	switch c.Launches {
	case 1:
		a.unlock("first_light")
	case 10:
		a.unlock("regular")
	case 100:
		a.unlock("vintner")
	case 500:
		a.unlock("launch_500")
	}
	if h := moodClock().Hour(); h >= 2 && h < 5 {
		a.unlock("night_owl")
	}
	c.updateStreak(moodClock())
	for _, s := range []struct {
		days int
		id   string
	}{{3, "streak_3"}, {7, "streak_7"}, {30, "streak_30"}} {
		if c.Streak >= s.days {
			a.unlock(s.id)
		}
	}
	a.saveCollection()
}

// CellarBottle is one legendary sighting: what it was, the mood code it
// came in (so it can be uncorked again), and when.
type CellarBottle struct {
	Layer  string `json:"layer"`
	Option string `json:"option"`
	Code   string `json:"code"`
	Date   string `json:"date"`
}

// cellarLegendaries adds this launch's legendaries to the cellar.
func (a *App) cellarLegendaries() {
	c := a.coll()
	added := false
	for _, l := range moodLayers {
		id := a.pick(l.id)
		o, ok := findOption(l.id, id)
		if !ok || o.rarity != legendary {
			continue
		}
		dup := false
		for _, b := range c.Cellar {
			dup = dup || (b.Code == a.mood.code() && b.Option == id)
		}
		if !dup {
			c.Cellar = append(c.Cellar, CellarBottle{Layer: string(l.id), Option: id, Code: a.mood.code(), Date: today()})
			added = true
		}
	}
	if added {
		a.saveCollection()
	}
}
