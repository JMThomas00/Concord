package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func plainCells(grid [][]bannerCell) string {
	var b strings.Builder
	for i, row := range grid {
		if i > 0 {
			b.WriteByte('\n')
		}
		for _, c := range row {
			b.WriteRune(c.r)
		}
	}
	return b.String()
}

// Every frame of every style keeps each line's exact length (hidden cells
// become spaces), so the centered login layout never shifts mid-animation,
// and the last frame is exactly the plain art -- across all real banners.
func TestBannerAnimFramesKeepShapeAndEndOnArt(t *testing.T) {
	for _, bn := range banners {
		art := trimBannerArt(bn.Art)
		lines := strings.Split(art, "\n")
		for style := range bannerAnimNames {
			for _, p := range []float64{0, 0.05, 0.3, 0.5, 0.8, 0.99} {
				grid := bannerAnimCells(art, style, p, 42, 3)
				if len(grid) != len(lines) {
					t.Fatalf("%s/%s p=%.2f: %d rows, want %d", bn.Name, bannerAnimNames[style], p, len(grid), len(lines))
				}
				for i, row := range grid {
					if len(row) != len([]rune(lines[i])) {
						t.Fatalf("%s/%s p=%.2f row %d: %d cells, want %d", bn.Name, bannerAnimNames[style], p, i, len(row), len([]rune(lines[i])))
					}
				}
			}
			if got := plainCells(bannerAnimCells(art, style, 1, 42, 3)); got != art {
				t.Fatalf("%s/%s: final frame differs from the art", bn.Name, bannerAnimNames[style])
			}
		}
	}
}

// Halfway through, each style should visibly differ from the finished logo.
func TestBannerAnimStylesAreVisibleMidway(t *testing.T) {
	art := trimBannerArt(banners[0].Art)
	for style, name := range bannerAnimNames {
		grid := bannerAnimCells(art, style, 0.5, 7, 5)
		counts := map[bannerCellKind]int{}
		for _, row := range grid {
			for _, c := range row {
				if c.r != ' ' || c.kind != cellHidden {
					counts[c.kind]++
				}
			}
		}
		if name == "decode" {
			if counts[cellNoise] == 0 {
				t.Errorf("%s: expected scrambled cells midway", name)
			}
			continue
		}
		if plainCells(grid) == art {
			t.Errorf("%s: midway frame is identical to the finished logo", name)
		}
		if counts[cellNormal] == 0 {
			t.Errorf("%s: nothing revealed midway", name)
		}
	}
}

func TestCtrlRShufflesBannerWithoutRepeatsAndAnimates(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.configMgr = nil // don't write the real config.json from a test
	a.view = ViewLogin
	a.bannerIndex = 0
	a.banner = banners[0]
	a.lastBannerAnimStyle = -1

	prevStyle := -1
	for i := 0; i < 200; i++ {
		prev := a.bannerIndex
		cmd := a.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
		if a.bannerIndex == prev {
			t.Fatalf("press %d: banner repeated (index %d)", i, prev)
		}
		if a.banner.Art != banners[a.bannerIndex].Art {
			t.Fatalf("press %d: banner/index out of sync", i)
		}
		if a.bannerAnim == nil || cmd == nil {
			t.Fatalf("press %d: expected an intro animation to start", i)
		}
		if a.bannerAnim.style == prevStyle {
			t.Fatalf("press %d: animation style %q repeated", i, bannerAnimNames[prevStyle])
		}
		prevStyle = a.bannerAnim.style
	}
}

func TestCtrlROnMainViewDoesNotShuffleBanner(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.configMgr = nil
	a.view = ViewMain
	a.bannerIndex = 3
	a.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
	if a.bannerIndex != 3 || a.bannerAnim != nil {
		t.Error("Ctrl+R outside login/register should keep its retry meaning, not shuffle the banner")
	}
}

func TestBannerAnimRunsToCompletionAndIgnoresStaleTicks(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.lastBannerAnimStyle = -1
	a.startBannerAnim()
	gen := a.bannerAnim.gen

	if a.handleBannerAnimTick(bannerAnimTickMsg{gen: gen - 1}) != nil || a.bannerAnim.frame != 0 {
		t.Fatal("a stale tick advanced the animation")
	}
	for i := 0; i < bannerAnimFrames-1; i++ {
		if a.handleBannerAnimTick(bannerAnimTickMsg{gen: gen}) == nil {
			t.Fatalf("animation stopped early at frame %d", i)
		}
	}
	if a.handleBannerAnimTick(bannerAnimTickMsg{gen: gen}) != nil || a.bannerAnim != nil {
		t.Fatal("animation should end after its last frame")
	}
	if got := ansi.Strip(a.renderBanner()); got != trimBannerArt(a.banner.Art) {
		t.Error("after the animation the banner should render as the plain art")
	}
}

func TestBannerAnimRespectsDisabledAnimations(t *testing.T) {
	a := newLoginTestApp(t, 160, 45, true)
	a.configMgr = nil
	a.view = ViewLogin
	a.uiConfig = &UIConfig{}
	a.uiConfig.Display.DisablePanelAnimations = true
	prev := a.bannerIndex
	a.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
	if a.bannerIndex == prev {
		t.Error("banner should still shuffle with animations disabled")
	}
	if a.bannerAnim != nil {
		t.Error("no intro animation should run with animations disabled")
	}
}

// With no previous banner, every banner (including index 0) must be reachable.
func TestGetRandomBannerCanPickFirstBannerWithNoHistory(t *testing.T) {
	for i := 0; i < 20000; i++ {
		if _, idx := GetRandomBanner(-1); idx == 0 {
			return
		}
	}
	t.Error("banner 0 never chosen with lastBannerIndex -1")
}
