package client

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// position returns the row and column where needle first appears in a
// rendered view, and the view's total line count.
func position(t *testing.T, view, needle string) (row, col, lines int) {
	t.Helper()
	all := strings.Split(ansi.Strip(view), "\n")
	for i, l := range all {
		if c := strings.Index(l, needle); c >= 0 {
			return i, len([]rune(l[:c])), len(all)
		}
	}
	t.Fatalf("%q not found in view", needle)
	return 0, 0, 0
}

// The form under the logo must not move when Ctrl+R swaps in a logo of a
// different height (they range from 1 to 21 lines) -- on a roomy terminal
// and on one too short for the full logo slot.
func TestLoginFormStaysPinnedAcrossEveryBanner(t *testing.T) {
	for _, height := range []int{55, 30} {
		a := newLoginTestApp(t, 160, height, true)
		wantRow, wantCol := -1, -1
		for i, b := range banners {
			a.banner = b
			view := a.renderLoginView()
			row, col, lines := position(t, view, "Welcome back")
			if lines != height {
				t.Fatalf("height %d, banner %q: view is %d lines", height, b.Name, lines)
			}
			if i == 0 {
				wantRow, wantCol = row, col
				continue
			}
			if row != wantRow || col != wantCol {
				t.Fatalf("height %d: banner %q put the form at row %d col %d, first banner at row %d col %d",
					height, b.Name, row, col, wantRow, wantCol)
			}
		}
	}
}

func TestLoginFormStaysPinnedMidAnimationAndWithAnError(t *testing.T) {
	a := newLoginTestApp(t, 160, 50, true)
	a.banner = banners[0]
	wantRow, wantCol, _ := position(t, a.renderLoginView(), "Welcome back")

	a.lastBannerAnimStyle = -1
	a.startBannerAnim()
	a.bannerAnim.frame = bannerAnimFrames / 2
	if row, col, _ := position(t, a.renderLoginView(), "Welcome back"); row != wantRow || col != wantCol {
		t.Errorf("mid-animation form at row %d col %d, want row %d col %d", row, col, wantRow, wantCol)
	}
	a.bannerAnim = nil

	a.loginError = "Invalid email or password, please double-check both fields and try again"
	if row, col, _ := position(t, a.renderLoginView(), "Welcome back"); row != wantRow || col != wantCol {
		t.Errorf("with an error the form moved to row %d col %d, want row %d col %d", row, col, wantRow, wantCol)
	}
}

// Layout A (logo lockup): the grapes stay put and every fitting banner
// starts on the same column and sits on the same baseline beside them.
func TestLogoLockupPinsGrapesAndBannerEdge(t *testing.T) {
	a := newLoginTestApp(t, 190, 48, true)
	a.view = ViewLogin
	g, _ := a.currentLockup()
	if !g.grapes {
		t.Fatal("expected grapes at 190x48")
	}

	grapeRow, grapeCol := -1, -1
	checked := 0
	for i, b := range banners {
		if !g.fits(i) {
			continue
		}
		a.banner = b
		lines := strings.Split(ansi.Strip(a.renderLoginView()), "\n")
		row, col, _ := position(t, strings.Join(lines, "\n"), ";##:")
		if grapeRow < 0 {
			grapeRow, grapeCol = row, col
		} else if row != grapeRow || col != grapeCol {
			t.Fatalf("banner %q moved the grapes to row %d col %d, want row %d col %d", b.Name, row, col, grapeRow, grapeCol)
		}

		// The grapes' bottom row is level with the password box's bottom.
		grapeBottom := grapeRow + grapeLogos[grapeLogoSize].rows - 1
		if !strings.Contains(lines[grapeBottom], "╰") {
			t.Fatalf("banner %q: grapes end on row %d, not level with the password box bottom:\n%q", b.Name, grapeBottom, lines[grapeBottom])
		}

		// Box starts right after the 35-column grapes (";##:" is 10 columns
		// in); its bottom row sits just above the gap before the form (the
		// form's first row is padding, then "Welcome back").
		boxStart := grapeCol - 10 + grapeLogoSize + grapeLockupGap + formTextIndent
		welcomeRow, welcomeCol, _ := position(t, strings.Join(lines, "\n"), "Welcome back")
		baseline := welcomeRow - 2 - bannerFormGap

		// Banner, form text, and shortcut hints share one left edge.
		if welcomeCol != boxStart {
			t.Fatalf("banner %q: form text starts at column %d, banner box at %d", b.Name, welcomeCol, boxStart)
		}
		if _, enterCol, _ := position(t, strings.Join(lines, "\n"), "Enter  Unlock"); enterCol != boxStart+1 {
			t.Fatalf("banner %q: hints' first chip text at column %d, want %d (chip edge on the shared left edge)", b.Name, enterCol, boxStart+1)
		}
		art := strings.Split(trimBannerArt(b.Art), "\n")
		last := len(art) - 1
		for last > 0 && strings.TrimSpace(art[last]) == "" {
			last--
		}
		want := []rune(art[last])
		got := []rune(lines[baseline-(len(art)-1-last)])
		if boxStart+len(want) > len(got) || string(got[boxStart:boxStart+len(want)]) != string(want) {
			t.Fatalf("banner %q's bottom line isn't at column %d on the baseline row:\n%q", b.Name, boxStart, string(got))
		}
		checked++
	}
	if checked < 300 {
		t.Errorf("only %d banners fit a 190-column screen; expected about 318", checked)
	}
}

func TestShuffleOnlyPicksBannersThatFit(t *testing.T) {
	a := newLoginTestApp(t, 120, 48, true)
	a.configMgr = nil
	a.view = ViewLogin
	g, _ := a.currentLockup()
	for i := 0; i < 300; i++ {
		a.handleKeyPress(tea.KeyMsg{Type: tea.KeyCtrlR})
		if !g.fits(a.bannerIndex) {
			d := bannerDims[a.bannerIndex]
			t.Fatalf("shuffle picked %q (%dx%d), which doesn't fit the %dx%d box", a.banner.Name, d[0], d[1], g.boxW, g.slot)
		}
	}
}

func TestResizeSwapsOutABannerThatNoLongerFits(t *testing.T) {
	a := newLoginTestApp(t, 190, 48, true)
	a.configMgr = nil
	a.view = ViewLogin
	widest := 0
	for i := range bannerDims {
		if bannerDims[i][0] > bannerDims[widest][0] {
			widest = i
		}
	}
	a.setBanner(widest)
	a.Update(tea.WindowSizeMsg{Width: 190, Height: 48})
	if g, _ := a.currentLockup(); !g.fits(a.bannerIndex) {
		t.Errorf("banner %q (%d wide) still showing in a %d-column box", a.banner.Name, bannerDims[a.bannerIndex][0], g.boxW)
	}
}

// On a narrower screen the grapes give way before the banner box would drop
// under bannerBoxMinWidth, so most banners stay available.
func TestGrapesGiveWayBeforeTheBannerBoxGetsTooNarrow(t *testing.T) {
	a := newLoginTestApp(t, 98, 48, true)
	a.view = ViewLogin
	g, _ := a.currentLockup()
	if g.grapes {
		t.Errorf("grapes shown with only a %d-column box", g.boxW)
	}
	if g.boxW < bannerBoxMinWidth {
		t.Errorf("box is %d columns; dropping the grapes should have kept it at least %d", g.boxW, bannerBoxMinWidth)
	}
}

func TestRegisterFormStaysPinnedAcrossBanners(t *testing.T) {
	a := newLoginTestApp(t, 160, 55, false)
	wantRow := -1
	for i, b := range banners {
		a.banner = b
		row, _, _ := position(t, a.renderRegisterView(), "Create a New Account")
		if i == 0 {
			wantRow = row
		} else if row != wantRow {
			t.Fatalf("banner %q moved the register form to row %d, want %d", b.Name, row, wantRow)
		}
	}
}
