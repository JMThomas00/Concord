package client

import (
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
	zone "github.com/lrstanley/bubblezone"
)

// newBenchChannelTree builds a channel tree at a scale comparable to a
// large real server (matches the shape of the server package's own
// BenchmarkChannelTreeBuild: 10 categories x 10 channels) so these
// benchmarks measure the render/mouse-support path at a realistic size,
// not the toy 1-category/3-channel fixture newLayoutTestApp uses for
// correctness tests.
func newBenchChannelTree(collapsed map[uuid.UUID]bool) *ChannelTree {
	var channels []*models.Channel
	for c := 0; c < 10; c++ {
		catID := uuid.New()
		channels = append(channels, &models.Channel{
			ID: catID, Name: fmt.Sprintf("Category %d", c), Type: models.ChannelTypeCategory, SortOrder: c,
		})
		for i := 0; i < 10; i++ {
			channels = append(channels, &models.Channel{
				ID: uuid.New(), Name: fmt.Sprintf("channel-%d-%d", c, i), Type: models.ChannelTypeText,
				CategoryID: catID, SortOrder: i, CreatedAt: time.Now(), UpdatedAt: time.Now(),
			})
		}
	}
	tree := BuildChannelTree(channels)
	tree.RebuildFlatList(collapsed)
	return tree
}

// newBenchApp mirrors newLayoutTestApp but swaps in the larger channel tree
// above -- factored out separately rather than changing newLayoutTestApp
// itself, since every existing correctness test relies on its exact
// 1-category/3-channel shape (e.g. asserting specific flat-list indices).
func newBenchApp(b *testing.B, width, height int) *App {
	b.Helper()
	a := newLayoutTestApp(b, width, height)
	a.channelTree = newBenchChannelTree(a.collapsedCategories)
	return a
}

// BenchmarkRenderMainView measures the full main-view render this sprint's
// mouse support runs on every frame -- server icons, a 110-channel tree,
// chat panel, member list, and status bar, each now zone.Mark-ing every
// row it draws (see mouse.go / views.go). This is the render path's actual
// steady-state cost: it runs on every Update() that triggers a re-render,
// not just on clicks.
func BenchmarkRenderMainView(b *testing.B) {
	a := newBenchApp(b, 160, 45)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.renderMainView()
	}
}

// BenchmarkZoneScanMainView isolates zone.Scan's own cost against a
// realistic rendered frame (stripping every zone.Mark marker and
// registering their bounds) -- this runs unconditionally on every render,
// whether or not a mouse event occurred, so its cost matters more broadly
// than just click handling.
func BenchmarkZoneScanMainView(b *testing.B) {
	a := newBenchApp(b, 160, 45)
	rendered := a.renderMainView()
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = zone.Scan(rendered)
	}
}

// BenchmarkResolveClickedChannelRow measures the per-click resolver cost
// added this sprint -- a linear scan across the flattened channel tree
// (110 rows) checking zoneInBounds per row. Run after a real Scan so the
// zones the resolver checks actually exist.
func BenchmarkResolveClickedChannelRow(b *testing.B) {
	a := newBenchApp(b, 160, 45)
	rendered := zone.Scan(a.renderMainView())
	// Let bubblezone's async worker settle once before timing (see the
	// documented Scan-then-Get race in settings_mouse_test.go) -- this
	// benchmark measures steady-state resolver cost, not that race.
	for i := 0; i < 200; i++ {
		if zone.Get("channel-row:"+a.channelTree.FlatList[0].Channel.ID.String()) != nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	_ = rendered

	// A click on the last row is the worst case for a linear scan.
	last := a.channelTree.FlatList[len(a.channelTree.FlatList)-1]
	z := zone.Get("channel-row:" + last.Channel.ID.String())
	if z == nil {
		b.Fatal("expected the last channel row's zone to be registered")
	}
	msg := clickAt(z.StartX, z.StartY)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		a.resolveClickedChannelRow(msg)
	}
}

// BenchmarkRenderSettingsView_Theme, _Notifications, and _Audio measure the
// Settings category render paths this sprint added zone-marking and the
// settingsSectionBuilder fixes to (see Concord - Mouse Support Plan's
// "Live Testing Findings — Second Pass" for the overflow bugs fixed there).
func BenchmarkRenderSettingsView_Theme(b *testing.B) {
	a := newLayoutTestApp(b, 160, 45)
	names := make([]string, 45)
	for i := range names {
		names[i] = fmt.Sprintf("theme-%d", i)
	}
	a.settingsState = &SettingsState{
		Categories:       []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "Help & Guide"},
		AvailableThemes:  names,
		SelectedCategory: settingsCatTheme,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.renderSettingsView()
	}
}

func BenchmarkRenderSettingsView_Notifications(b *testing.B) {
	a := newLayoutTestApp(b, 160, 45)
	a.uiConfig = &UIConfig{Notifications: NotificationConfig{MentionsOnly: true, MentionSound: "bell", MessageSound: "boop"}}
	a.settingsState = &SettingsState{
		Categories:       []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "Help & Guide"},
		SelectedCategory: settingsCatNotifications,
		FocusOnForm:      true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.renderSettingsView()
	}
}

func BenchmarkRenderSettingsView_Audio(b *testing.B) {
	a := newLayoutTestApp(b, 160, 45)
	a.uiConfig = &UIConfig{Audio: defaultAudioConfig(AudioConfig{})}
	a.settingsState = &SettingsState{
		Categories:       []string{"Theme", "Notifications", "Display", "Audio", "Manage Servers", "Help & Guide"},
		SelectedCategory: settingsCatAudio,
		FocusOnForm:      true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.renderSettingsView()
	}
}

// BenchmarkRenderServerManagementView_Channels measures Server Settings'
// Channels category (Area 4 of the mouse support plan) at a 110-channel
// scale, the same shape as BenchmarkRenderMainView above.
func BenchmarkRenderServerManagementView_Channels(b *testing.B) {
	a := newLayoutTestApp(b, 160, 45)
	tree := newBenchChannelTree(map[uuid.UUID]bool{})
	channelList := make([]*models.Channel, len(tree.FlatList))
	for i, node := range tree.FlatList {
		channelList[i] = node.Channel
	}
	a.serverManagementState = &ServerManagementState{
		Categories:       []string{"Channels", "Roles", "Members", "Messages", "Plugins"},
		SelectedCategory: 0,
		ChannelList:      channelList,
		FocusOnForm:      true,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = a.renderServerManagementView()
	}
}

// clickAt builds a left-button press MouseMsg at the given coordinates --
// a tiny helper so each resolver benchmark doesn't repeat the same struct
// literal.
func clickAt(x, y int) tea.MouseMsg {
	return tea.MouseMsg{Action: tea.MouseActionPress, Button: tea.MouseButtonLeft, X: x, Y: y}
}
