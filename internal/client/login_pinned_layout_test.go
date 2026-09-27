package client

import (
	"strings"
	"testing"

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
