package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

// Oldest first makes the stack a queue: the oldest at the bottom, dismissed
// first, and the newer ones waiting above.
func TestToastOrderOldestFirst(t *testing.T) {
	a := &App{uiConfig: &UIConfig{}, width: 120, height: 40}
	a.notifConfig.ToastOrder = ToastOrderOldest
	fit := a.toastFit()
	for i := 0; i < fit+2; i++ {
		a.toasts = append(a.toasts, &toast{title: fmt.Sprint(i)})
	}
	now := time.Now()
	vis, waiting := a.visibleToasts(now)
	if len(vis) != fit || waiting != 2 {
		t.Fatalf("visible %d, waiting %d", len(vis), waiting)
	}
	if a.currentToast(now).title != "0" || vis[0].title != fmt.Sprint(fit-1) {
		t.Fatalf("bottom %q, top %q", a.currentToast(now).title, vis[0].title)
	}
	a.dismissBottomToast()
	if a.currentToast(now).title != "1" {
		t.Fatalf("after Ctrl+X the bottom is %q", a.currentToast(now).title)
	}
}

// Each Notifications field's first line is where notifFieldLineStarts says,
// so the keyboard scrolls to it and the mouse finds it.
func TestNotifFieldLineStarts(t *testing.T) {
	a := newLayoutTestApp(t, 140, 200)
	a.settingsState = &SettingsState{}
	lines := strings.Split(ansi.Strip(a.renderNotificationsContent(120, 200)), "\n")
	header := -1
	for i, l := range lines {
		if strings.Contains(l, "Desktop Notifications") {
			header = i
			break
		}
	}
	if header < 0 {
		t.Fatal("no Desktop Notifications section")
	}
	for field, label := range map[int]string{8: "Message Toasts", 9: "Toasts From", 10: "Toast Order", 11: "Toast Side", 12: "Toast Style"} {
		found := -1
		for i, l := range lines {
			if strings.Contains(l, label) {
				found = i - header
				break
			}
		}
		if found != notifFieldLineStarts[field] {
			t.Errorf("%s is on line %d, notifFieldLineStarts says %d", label, found, notifFieldLineStarts[field])
		}
	}
}

// With an object on the right of the login stage, the banner picked for it
// keeps clear of it; one shuffled in with Ctrl+R may be any size.
func TestBannerKeepsClearOfTheMoon(t *testing.T) {
	a := newLoginTestApp(t, 150, 42, true)
	a.view = ViewLogin
	a.mood = moodFromSeed(12345)
	a.mood.picks[layerAtmosphere] = "moonrise"
	g, _ := a.currentLockup()
	clearOf := a.sceneClearance()
	wide := -1
	for i := range banners {
		if g.fits(i) && !clearOf(i) {
			wide = i
			break
		}
	}
	if wide < 0 {
		t.Skip("every fitting banner clears the moon at this size")
	}
	a.setBanner(wide)
	a.ensureBannerFits()
	if !a.sceneClearance()(a.bannerIndex) {
		t.Fatalf("banner %d still overlaps the moon", a.bannerIndex)
	}
	a.bannerShuffled = true
	a.setBanner(wide)
	a.ensureBannerFits()
	if a.bannerIndex != wide {
		t.Fatal("a shuffled banner was swapped for a smaller one")
	}
}
