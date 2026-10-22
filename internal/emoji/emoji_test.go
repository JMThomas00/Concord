package emoji

import (
	"strings"
	"testing"
)

func TestTheTableLoads(t *testing.T) {
	list := All()
	if len(list) < 1800 {
		t.Fatalf("only %d emoji", len(list))
	}
	if list[0].Char != "😀" || GroupNames[list[0].Group] != "Smileys & Emotion" {
		t.Fatalf("first emoji: %+v", list[0])
	}
	seen := map[string]bool{}
	for _, e := range list {
		if e.Char == "" || e.Name == "" || e.Group < 0 || e.Group >= len(GroupNames) {
			t.Fatalf("bad entry: %+v", e)
		}
		if seen[e.Char] {
			t.Fatalf("%s twice", e.Char)
		}
		seen[e.Char] = true
		if len(e.Tones) != 0 && len(e.Tones) != 5 {
			t.Fatalf("%s has %d tones", e.Char, len(e.Tones))
		}
	}
}

func TestSearchFindsTheObviousOneFirst(t *testing.T) {
	for query, want := range map[string]string{
		"thumbs":    "👍",
		"+1":        "👍",
		":grapes:":  "🍇",
		"grape":     "🍇",
		"party":     "🥳",
		"joy":       "😂",
		"heart":     "❤️",
		"tada":      "🎉",
		"fire":      "🔥",
		"thumbs up": "👍",
		"Thumbs UP": "👍",
		"us":        "🇺🇸",
		"rofl":      "🤣",
		"smile":     "😄",
	} {
		got := Search(query, 5)
		var chars []string
		for _, e := range got {
			chars = append(chars, e.Char)
		}
		if len(got) == 0 || got[0].Char != want {
			t.Errorf("Search(%q) = %s, want %s first", query, strings.Join(chars, " "), want)
		}
	}
	if got := Search("zzqx", 0); len(got) != 0 {
		t.Errorf("nonsense found %d", len(got))
	}
	if got := Search("   ", 0); got != nil {
		t.Errorf("an empty query found %d", len(got))
	}
	if got := Search("face", 10); len(got) != 10 {
		t.Errorf("limit: got %d", len(got))
	}
}

func TestShortcodesTonesAndLookup(t *testing.T) {
	if e, ok := Shortcode(":+1:"); !ok || e.Char != "👍" {
		t.Fatalf(":+1: = %+v", e)
	}
	thumbs, _ := Shortcode("thumbsup")
	if got := thumbs.WithTone(3); got != "👍🏽" {
		t.Fatalf("medium tone: %q", got)
	}
	if got := thumbs.WithTone(0); got != "👍" {
		t.Fatalf("no tone: %q", got)
	}
	grapes, _ := Shortcode("grapes")
	if got := grapes.WithTone(3); got != "🍇" {
		t.Fatalf("grapes have no tone: %q", got)
	}
	for _, s := range []string{"👍🏿", "❤", "❤️", "🍇"} {
		if _, ok := Lookup(s); !ok {
			t.Errorf("Lookup(%q) failed", s)
		}
	}
}
