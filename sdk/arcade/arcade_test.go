package arcade

import (
	"bytes"
	"math/rand/v2"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/JMThomas00/Concord/sdk/wire"
)

var sgr = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func TestCanvasNeverOverflows(t *testing.T) {
	for _, size := range [][2]int{{80, 24}, {43, 17}, {10, 3}} {
		c := New(size[0], size[1], NewPalette(nil))
		c.Logo("GRAPE RACE", -3, 1, 1, nil)
		c.Text(size[0]-2, 0, "🍇🍇 too long for the edge", "fg", "", true)
		c.Box(0, 0, size[0]+10, size[1]+5, "comment", "TITLE", "purple")
		c.Number(123, 3, size[0]-4, 3, "green", 2)
		lines := strings.Split(c.String(), "\n")
		if len(lines) != size[1] {
			t.Fatalf("%v: %d lines", size, len(lines))
		}
		for _, l := range lines {
			if w := ansi.StringWidth(l); w > size[0] {
				t.Fatalf("%v: line %d wide: %q", size, w, sgr.ReplaceAllString(l, ""))
			}
		}
	}
}

func TestHalfBlocks(t *testing.T) {
	c := New(4, 1, NewPalette(nil))
	c.Px(0, 0, "red")   // top only
	c.Px(1, 1, "red")   // bottom only
	c.Px(2, 0, "red")   // both, same
	c.Px(2, 1, "red")   //
	c.Px(3, 0, "red")   // both, different
	c.Px(3, 1, "green") //
	got := sgr.ReplaceAllString(c.String(), "")
	if got != "▀▄█▀" {
		t.Fatalf("got %q", got)
	}
}

func TestLogoAndDigitSizes(t *testing.T) {
	if LogoWidth("GRAPE RACE", 1) != 59 || LogoWidth("GO!", 2) != 34 || LogoHeight(1) != 4 {
		t.Fatal("logo sizes changed")
	}
	c := New(20, 4, NewPalette(nil))
	c.Number(7, 2, 0, 0, "green", 1)
	plain := sgr.ReplaceAllString(c.String(), "")
	if !strings.Contains(plain, "█") || DigitWidth(1) != 5 {
		t.Fatalf("digits:\n%s", plain)
	}
}

func TestTerminalDefaultTheme(t *testing.T) {
	// terminal-default gives ANSI numbers and empty strings, not hex
	pal := NewPalette(&wire.PaneTheme{Palette: map[string]string{
		"background": "", "foreground": "", "purple": "5", "red": "1", "comment": "8", "green": "2",
	}})
	if _, _, _, ok := pal.Get("shadow").RGB(); ok {
		t.Fatal("shadow shouldn't be mixed from ANSI colours")
	}
	c := New(10, 2, pal)
	c.Logo("GO", 0, 0, 1, nil)
	c.Text(0, 1, "x", "purple", "", false)
	if s := c.String(); !strings.Contains(s, "\x1b[0;35m") {
		t.Fatalf("ANSI purple not used: %q", s)
	}
}

func TestRewards(t *testing.T) {
	items := []Unlockable{
		{"kart", "GO-KART", Starter}, {"f1", "F1", Common}, {"drag", "DRAG", Common},
		{"monster", "MONSTER", Rare}, {"tub", "BATHTUB", Legendary},
	}
	rnd := rand.New(rand.NewPCG(1, 2))
	var r Rewards
	if !r.Owns("kart", items) || r.Owns("tub", items) || r.Count(items) != 1 {
		t.Fatal("starters are owned, the rest aren't")
	}
	if r.Deal(items, rnd) != nil {
		t.Fatal("no pass, no offer")
	}
	if got := r.Grant(9, items); got != 4 || r.Passes != 4 {
		t.Fatalf("granted %d: passes are capped at what's left to unlock", got)
	}
	offer := r.Deal(items, rnd)
	if len(offer) != 3 {
		t.Fatalf("offer %v", offer)
	}
	if again := r.Deal(items, rnd); strings.Join(again, ",") != strings.Join(offer, ",") {
		t.Fatal("the offer must stay the same until a pick")
	}
	if r.Pick("kart") {
		t.Fatal("picked something not on offer")
	}
	if !r.Pick(offer[1]) || r.Passes != 3 || !r.Owns(offer[1], items) || r.Offer != nil {
		t.Fatal("pick didn't spend a pass and unlock")
	}
	for r.Passes > 0 {
		r.Pick(r.Deal(items, rnd)[0])
	}
	if len(r.Locked(items)) != 0 || r.Grant(1, items) != 0 {
		t.Fatal("everything should be unlocked, and passes stop")
	}
}

func TestRewardWeights(t *testing.T) {
	var items []Unlockable
	for i := 0; i < 10; i++ {
		items = append(items, Unlockable{ID: "c" + itoa(i), Tier: Common}, Unlockable{ID: "l" + itoa(i), Tier: Legendary})
	}
	rnd := rand.New(rand.NewPCG(3, 4))
	common := 0
	for i := 0; i < 2000; i++ {
		r := Rewards{Passes: 1}
		if r.Deal(items, rnd)[0][0] == 'c' {
			common++
		}
	}
	// common is weighted 6 to legendary's 1
	if common < 1600 || common > 1830 {
		t.Fatalf("first card common %d of 2000 times", common)
	}
}

func TestSoundKit(t *testing.T) {
	k := SoundKit()
	for _, name := range []string{SoundBlip, SoundSelect, SoundBack, SoundCoin, SoundAmber, SoundGo, SoundRed, SoundMistake, SoundFinish, SoundRecord} {
		data := k[name]
		if len(data) < 1000 || !bytes.HasPrefix(data, []byte("RIFF")) || !bytes.Contains(data[:40], []byte("WAVEfmt ")) {
			t.Fatalf("%s isn't a WAV file (%d bytes)", name, len(data))
		}
		if len(data) > 64*1024 {
			t.Fatalf("%s is %d bytes; keep sounds short", name, len(data))
		}
	}
}
