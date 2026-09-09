package client

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// TestScheduleReconnectEmitsConnectionRetryingMsg is a regression test for
// a real gap: scheduleReconnect computed an attempt count and delay but
// never actually constructed ConnectionRetryingMsg, so the "Reconnecting
// (attempt N)..." status line -- fully coded and wired to a real switch
// case -- could never appear; a reconnecting user only ever saw one static
// message for the whole backoff sequence. This only exercises the
// immediately-resolving half of the returned tea.Batch (index 0) -- the
// other half genuinely sleeps for the real backoff delay and isn't safe to
// run in a unit test.
func TestScheduleReconnectEmitsConnectionRetryingMsg(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	sc.Token = "test-token" // scheduleReconnect requires a saved token to proceed meaningfully

	cmd := a.scheduleReconnect(serverID)
	if cmd == nil {
		t.Fatal("expected a non-nil command")
	}

	batchMsg, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected a tea.BatchMsg, got %T", cmd())
	}
	if len(batchMsg) != 2 {
		t.Fatalf("expected 2 batched commands (retrying notice + sleep-then-reconnect), got %d", len(batchMsg))
	}

	got := batchMsg[0]()
	retrying, ok := got.(ConnectionRetryingMsg)
	if !ok {
		t.Fatalf("expected the first batched command to produce ConnectionRetryingMsg, got %T", got)
	}
	if retrying.ServerID != serverID {
		t.Errorf("ServerID = %v, want %v", retrying.ServerID, serverID)
	}
	if retrying.AttemptCount != 1 {
		t.Errorf("AttemptCount = %d, want 1 on first call", retrying.AttemptCount)
	}
}

// TestScheduleReconnectIncrementsAttemptCount confirms each successive call
// reports a higher AttemptCount, matching sc.RetryCount's own progression --
// the display would otherwise be stuck showing "attempt 1" forever even as
// real retries pile up.
func TestScheduleReconnectIncrementsAttemptCount(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	sc.Token = "test-token"

	for want := 1; want <= 3; want++ {
		batchMsg := a.scheduleReconnect(serverID)().(tea.BatchMsg)
		got := batchMsg[0]().(ConnectionRetryingMsg)
		if got.AttemptCount != want {
			t.Errorf("call %d: AttemptCount = %d, want %d", want, got.AttemptCount, want)
		}
	}
}

// TestRetryConnectionNowFiresWhenErrorOrDisconnected covers the manual
// ctrl+r retry keybind's gating logic. It must fire during both states a
// pending automatic reconnect can leave a connection in while its backoff
// sleep is in flight: StateDisconnected (the first reconnect cycle, right
// after handleServerScopedMessage's DisconnectedMsg case) and StateError (a
// later cycle, after a failed attempt) -- gating on StateError alone would
// silently no-op ctrl+r during that first cycle, which is exactly when a
// user is most likely to reach for it. We don't invoke the returned
// command here (unlike scheduleReconnect's own test) since it dials a real
// connection immediately, with no sleep half to stop short at.
func TestRetryConnectionNowFiresWhenErrorOrDisconnected(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	sc.Token = "test-token"

	for _, state := range []ConnectionState{StateError, StateDisconnected} {
		sc.SetState(state)
		if cmd := a.retryConnectionNow(serverID); cmd == nil {
			t.Errorf("state %v: expected a non-nil retry command", state)
		}
	}
}

// TestRetryConnectionNowNoopWhenNotPending confirms ctrl+r is a harmless
// no-op outside the disconnected/error window -- most importantly while a
// connection attempt is already StateConnecting, where firing a second,
// concurrent connectServerAsync would be pure duplicate work.
func TestRetryConnectionNowNoopWhenNotPending(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	serverID := uuid.New()
	sc, err := a.connMgr.AddServer(&ClientServerInfo{ID: serverID})
	if err != nil {
		t.Fatalf("AddServer: %v", err)
	}
	sc.Token = "test-token"

	for _, state := range []ConnectionState{StateReady, StateConnecting, StateAuthenticating, StateConnected} {
		sc.SetState(state)
		if cmd := a.retryConnectionNow(serverID); cmd != nil {
			t.Errorf("state %v: expected a nil (no-op) command, got a non-nil one", state)
		}
	}
}

// TestRetryConnectionNowNoopForUnknownServer guards against a nil-pointer
// panic if ctrl+r is somehow pressed for a server ID the connection manager
// has no record of at all.
func TestRetryConnectionNowNoopForUnknownServer(t *testing.T) {
	a := newLayoutTestApp(t, 160, 45)

	if cmd := a.retryConnectionNow(uuid.New()); cmd != nil {
		t.Error("expected a nil command for an unregistered server ID")
	}
}
