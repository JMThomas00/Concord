package server

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

func isRecords(m *protocol.Message) bool { return m.Type == protocol.EventPluginRecords }

func readRecords(t *testing.T, c *testWSClient) protocol.PluginRecordsPayload {
	t.Helper()
	m := c.readUntil(5*time.Second, isRecords)
	var p protocol.PluginRecordsPayload
	if err := json.Unmarshal(m.Data, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

// A plugin's records reach their member (live, and again on sign-in), with
// undeclared achievements dropped and the first unlock date kept; the
// leaderboard ranks members and leaves out those who hide.
func TestPluginRecordsAndLeaderboard(t *testing.T) {
	r := newPaneTestRig(t)
	alice, ac := r.member(t, "alice")
	bob, _ := r.member(t, "bob")

	first := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	r.pluginEvent(t, protocol.PluginEventRecord, alice.ID, protocol.PluginRecord{
		UserID:   alice.ID,
		Stats:    []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "3", Num: 3}},
		Unlocked: []protocol.PluginUnlock{{ID: "first_win", At: first}, {ID: "made_up", At: first}},
	})
	got := readRecords(t, ac)
	if len(got.Records) != 1 || got.Records[0].PluginID != "HelloPlugin" {
		t.Fatalf("records %+v", got)
	}
	rec := got.Records[0].Record
	if len(rec.Unlocked) != 1 || rec.Unlocked[0].ID != "first_win" || len(rec.Stats) != 1 {
		t.Fatalf("record %+v", rec)
	}

	// Sent again later: the first date sticks.
	r.pluginEvent(t, protocol.PluginEventRecord, alice.ID, protocol.PluginRecord{
		UserID:   alice.ID,
		Stats:    []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "4", Num: 4}},
		Unlocked: []protocol.PluginUnlock{{ID: "first_win", At: time.Now()}},
	})
	if again := readRecords(t, ac); !again.Records[0].Record.Unlocked[0].At.Equal(first) {
		t.Fatalf("unlock date moved to %v", again.Records[0].Record.Unlocked[0].At)
	}

	r.pluginEvent(t, protocol.PluginEventRecord, bob.ID, protocol.PluginRecord{
		UserID: bob.ID, Stats: []protocol.PluginStat{{Key: "wins", Label: "Wins", Value: "9", Num: 9}}})
	time.Sleep(100 * time.Millisecond)

	board, ok := r.srv.handlers.leaderboard("HelloPlugin", alice.ID)
	if !ok || len(board.Entries) != 2 || board.Entries[0].Username != "bob" || board.Entries[1].Username != "alice" {
		t.Fatalf("leaderboard %+v", board)
	}
	if board.You == nil || board.You.Rank != 2 || board.Label != "Wins" {
		t.Fatalf("you %+v", board.You)
	}
	if err := r.srv.db.SetLeaderboardHidden(bob.ID, true); err != nil {
		t.Fatal(err)
	}
	if board, _ := r.srv.handlers.leaderboard("HelloPlugin", alice.ID); len(board.Entries) != 1 || board.You.Rank != 1 {
		t.Fatalf("bob still listed: %+v", board)
	}

	// Signing in again brings the records back.
	token, err := r.srv.handlers.CreateAuthToken(alice.ID, "127.0.0.1", "test")
	if err != nil {
		t.Fatal(err)
	}
	c2 := newTestWSClient(t, r.wsURL)
	c2.identify(token)
	if again := readRecords(t, c2); len(again.Records) != 1 {
		t.Fatalf("on sign-in: %+v", again)
	}

	if boards := r.srv.handlers.PluginBoardInfos(); len(boards) != 1 || len(boards[0].Achievements) != 2 || boards[0].LeaderboardStat != "wins" {
		t.Fatalf("boards %+v", boards)
	}
}
