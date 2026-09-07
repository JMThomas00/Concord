package client

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/themes"
)

// TestRenderMainViewMatchesReportedHeight is the regression test for a real
// bug found live 2026-09-06: several of the four main panels (server icons,
// channel list, chat, member list -- and their collapsed variants) built
// their bordered box with `lipgloss.NewStyle().Height(N).Border(...)`.
// lipgloss.Height(N) sets the *content* height; Border() then adds 2 more
// lines (top+bottom), so the rendered block was N+2 lines tall, not N.
// Every panel did this independently, so the whole screen rendered exactly
// 2 lines taller than a.height. Bubbletea's renderer silently drops lines
// from the TOP when content exceeds the terminal height (see
// standardRenderer.flush in bubbletea's own source), so everything Concord
// drew ended up shifted 2 rows up from where its own layout math (and, once
// mouse support was added, its zone-based click hit-testing) thought it
// was -- clicking a channel always selected the one two rows above the one
// actually clicked.
//
// This test renders the real renderMainView() and asserts it produces
// *exactly* a.height lines, for several plausible terminal sizes -- it
// would have failed with "52 lines, want 50" (etc.) against the pre-fix
// code.
func TestRenderMainViewMatchesReportedHeight(t *testing.T) {
	for _, height := range []int{24, 40, 50, 80} {
		t.Run(fmt.Sprintf("height=%d", height), func(t *testing.T) {
			a := newLayoutTestApp(t, 160, height)
			got := strings.Count(a.renderMainView(), "\n") + 1
			if got != height {
				t.Errorf("renderMainView() at a.height=%d produced %d lines, want %d (diff=%d)", height, got, height, got-height)
			}
		})
	}
}

// newLayoutTestApp builds a minimal but real App fixture sufficient to call
// renderMainView() without panicking -- mirrors the fixture-construction
// pattern already used for mouse-support testing (see mouse_test.go /
// commands_test.go), just factored out since this test also needs it.
func newLayoutTestApp(t *testing.T, width, height int) *App {
	t.Helper()

	a := &App{}
	a.theme = themes.GetDefaultTheme()
	a.width = width
	a.height = height
	a.collapsedCategories = map[uuid.UUID]bool{}
	a.serverListAnimWidth = 22
	a.membersAnimWidth = 30
	a.input = textarea.New()
	a.input.SetHeight(4) // matches NewApp()'s real setup (app.go) -- 2 borders + 4 content = 6-row input slot

	cfgMgr, err := NewConfigManager()
	if err != nil {
		t.Fatalf("NewConfigManager: %v", err)
	}
	a.configMgr = cfgMgr
	a.connMgr = NewConnectionManager(make(chan tea.Msg, 10))

	catID := uuid.New()
	channels := []*models.Channel{
		{ID: catID, Name: "GENERAL", Type: models.ChannelTypeCategory, SortOrder: 0},
	}
	for i, n := range []string{"general", "off-topic", "announcements"} {
		channels = append(channels, &models.Channel{
			ID: uuid.New(), Name: n, Type: models.ChannelTypeText, CategoryID: catID, SortOrder: i,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		})
	}
	tree := BuildChannelTree(channels)
	tree.RebuildFlatList(a.collapsedCategories)
	a.channelTree = tree

	return a
}
