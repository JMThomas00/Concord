package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/google/uuid"
)

// toastApp is the main window, 170x60, with n message toasts waiting.
func toastApp(t *testing.T, n int) *App {
	t.Helper()
	a := newLayoutTestApp(t, 170, 60)
	a.view = ViewMain
	for i := range n {
		a.toastMessage(uuid.New(), uuid.New(), "anothergh0st", "Sequoia", "general chat",
			fmt.Sprintf("message %d, long enough to need truncating at some point in a narrow column", i), i == 2)
	}
	return a
}

// Jordan, 2026-10-08: the stack reached two thirds of the way up the window
// with a blank row between cards. Now it's at most six, touching.
func TestAtMostSixToastsWithNoGaps(t *testing.T) {
	a := toastApp(t, 10)
	a.renderMainView()
	vis, waiting := a.visibleToasts(time.Now())
	if len(vis) != 6 || waiting != 4 {
		t.Fatalf("%d shown, %d waiting; want 6 and 4", len(vis), waiting)
	}
	for i := 1; i < len(vis); i++ {
		if gap := vis[i].toRow - vis[i-1].toRow; gap != 4 {
			t.Errorf("cards %d and %d are %v rows apart, want 4 (touching)", i-1, i, gap)
		}
	}
	a.height = 20 // a short window: never more than half its height
	if fit := a.toastFit(); fit*a.toastRows() > a.height/2 {
		t.Errorf("%d cards fill more than half of %d rows", fit, a.height)
	}
}

// The cards stay inside the side they're on, so they never cover the chat
// or the message box: the server and channel columns on the left, the
// members panel on the right, in either style.
func TestToastsStayInTheirSide(t *testing.T) {
	for _, side := range []string{ToastSideLeft, ToastSideRight} {
		for _, style := range []string{ToastStyleFull, ToastStyleCompact} {
			a := toastApp(t, 8)
			a.notifConfig.ToastSide, a.notifConfig.ToastStyle = side, style
			now := time.Now()
			a.paintToasts(a.renderMainView(), now) // places them and starts the slide
			frame := a.paintToasts(a.renderMainView(), now.Add(time.Second))
			if len(a.toastRects) == 0 {
				t.Fatalf("%q/%q: nothing drawn", side, style)
			}
			for _, r := range a.toastRects {
				if side == ToastSideLeft && r.col+r.cols > a.toastLeftW {
					t.Errorf("left/%q: card reaches column %d, the channels end at %d", style, r.col+r.cols, a.toastLeftW)
				}
				if side == ToastSideRight && r.col < a.width-a.toastRightW {
					t.Errorf("right/%q: card starts at column %d, the members panel at %d", style, r.col, a.width-a.toastRightW)
				}
				if want := a.toastRows(); r.rows != want {
					t.Errorf("%q/%q: a card is %d rows, want %d", side, style, r.rows, want)
				}
			}
			if style == ToastStyleCompact && !strings.Contains(ansi.Strip(frame), "Sequoia · anothergh0st") {
				t.Errorf("%q: a compact card doesn't say where and who:\n%s", side, ansi.Strip(frame))
			}
		}
	}
}

// With the members panel hidden there's no room on the right, so the cards
// go to the left rather than over the chat.
func TestRightSideToastsFallBackWhenMembersAreHidden(t *testing.T) {
	a := toastApp(t, 2)
	a.notifConfig.ToastSide = ToastSideRight
	a.renderMainView()
	a.toastRightW = 0
	if col, _, right := a.toastArea(); right || col != 0 {
		t.Fatalf("area starts at %d (right=%v), want the left side", col, right)
	}
}

// Jordan, 2026-10-08: after Ctrl+X the rest of the stack hung instead of
// sliding down (a click looked fine only because the mouse kept redrawing).
// The animation timer must see the move straight after the dismissal.
func TestCtrlXKeepsTheStackAnimating(t *testing.T) {
	a := toastApp(t, 3)
	a.renderMainView()
	now := time.Now()
	a.visibleToasts(now.Add(-time.Second)) // laid out a second ago, settled
	if a.toastsMoving(now) {
		t.Fatal("a settled stack shouldn't need the timer")
	}
	a.dismissBottomToast()
	if !a.toastsMoving(time.Now()) {
		t.Fatal("after Ctrl+X the stack's slide doesn't keep the timer running")
	}
}
