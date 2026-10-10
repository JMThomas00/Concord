package clienttest_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/JMThomas00/Concord/sdk/client"
	"github.com/JMThomas00/Concord/sdk/client/clienttest"
)

// counter is small client code: Enter counts locally and tells the server
// half; the best count is remembered on the viewer's computer.
func counter() client.Handler {
	n, best := 0, 0
	draw := func() { _ = client.Frame(fmt.Sprintf("count %d (best %d)", n, best)) }
	return client.Handler{
		OnStart: func(e client.Event) {
			if v, ok, _ := client.Get("best"); ok {
				fmt.Sscan(v, &best)
			}
			_ = client.ForwardKeys(false)
			draw()
		},
		OnKey: func(e client.Event) {
			if e.Key != "enter" {
				return
			}
			n++
			if n > best {
				best = n
				_ = client.Set("best", fmt.Sprint(best))
			}
			_ = client.Send(map[string]int{"count": n})
			_ = client.PlaySound("sounds/tick.wav", 0.5)
			_ = client.After("flash", 100*time.Millisecond)
			draw()
		},
		OnTimer: func(id string) { client.Log("timer %s", id) },
		OnServer: func(data json.RawMessage) {
			var m struct{ Reset bool }
			if json.Unmarshal(data, &m) == nil && m.Reset {
				n = 0
				draw()
			}
		},
	}
}

func TestCounter(t *testing.T) {
	h := clienttest.New(t, counter(), "pane", "storage", "server", "sound")
	h.Start(40, 10)
	if h.LastFrame() != "count 0 (best 0)" || h.ForwardingKeys() {
		t.Fatalf("start: %q forwarding=%v", h.LastFrame(), h.ForwardingKeys())
	}
	h.Key("enter")
	h.Key("enter")
	if h.LastFrame() != "count 2 (best 2)" || h.Storage()["best"] != "2" {
		t.Fatalf("after two: %q %v", h.LastFrame(), h.Storage())
	}
	if sent := h.Sent(); len(sent) != 2 || string(sent[1]) != `{"count":2}` {
		t.Fatalf("sent %s", sent)
	}
	if len(h.Sounds()) != 2 || h.Timers()["flash"] != 100*time.Millisecond {
		t.Fatalf("sounds %v timers %v", h.Sounds(), h.Timers())
	}
	if !h.Timer("flash") || h.Logs()[0] != "timer flash" {
		t.Fatalf("logs %v", h.Logs())
	}
	h.Server(map[string]bool{"reset": true})
	if h.LastFrame() != "count 0 (best 2)" {
		t.Fatalf("after reset: %q", h.LastFrame())
	}
}

// Without a capability, the call fails and nothing happens.
func TestCapabilitiesAreEnforced(t *testing.T) {
	var errs []error
	h := clienttest.New(t, client.Handler{OnStart: func(client.Event) {
		errs = append(errs, client.Frame("x"), client.Set("k", "v"), client.Send(1))
	}}, "pane")
	h.Start(10, 5)
	if errs[0] != nil || errs[1] != client.ErrNotAllowed || errs[2] != client.ErrNotAllowed {
		t.Fatalf("errors %v", errs)
	}
}

func TestClaimKeys(t *testing.T) {
	var bad error
	h := clienttest.New(t, client.Handler{
		OnStart: func(client.Event) { _ = client.ClaimKeys("esc") },
		OnKey: func(e client.Event) {
			_ = client.ClaimKeys()
			bad = client.ClaimKeys("enter")
		},
	}, "pane")
	h.Start(10, 5)
	if c := h.Claimed(); len(c) != 1 || c[0] != "esc" {
		t.Fatalf("claimed %v", c)
	}
	h.Key("x")
	if len(h.Claimed()) != 0 || bad != client.ErrBadRequest {
		t.Fatalf("claimed %v, claiming enter: %v", h.Claimed(), bad)
	}
}
