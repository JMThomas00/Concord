package client

import (
	"os"
	"strings"
	"sync"
	"time"
)

// CONCORD_SCENES forces picks, for trying the login stage's looks without
// rerolling moods until one turns up. Layers by name, plus the screensaver
// and how long until it starts:
//
//	CONCORD_SCENES="atmosphere=lightbulb,loading=flashbang,transition=powercut,saver=fireworks,saver_after=10s"
var sceneOverrides = sync.OnceValue(func() map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(os.Getenv("CONCORD_SCENES"), ",") {
		if k, v, ok := strings.Cut(strings.TrimSpace(kv), "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
})

// forcedPick is CONCORD_SCENES's pick for a layer, if it names a real one.
func forcedPick(layer moodLayer) (string, bool) {
	id, ok := sceneOverrides()[string(layer)]
	if !ok {
		return "", false
	}
	_, real := findOption(layer, id)
	return id, real
}

// forcedSaver is CONCORD_SCENES's screensaver, if it names a real one.
func forcedSaver() (string, bool) {
	kind := sceneOverrides()["saver"]
	for _, k := range saverKinds {
		if k == kind {
			return k, true
		}
	}
	return "", false
}

// idleBeforeSaver is how long the stage sits idle before a screensaver.
func idleBeforeSaver() time.Duration {
	if d, err := time.ParseDuration(sceneOverrides()["saver_after"]); err == nil && d > 0 {
		return d
	}
	return saverAfter
}
