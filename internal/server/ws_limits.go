package server

import (
	"net"
	"sync"
	"time"

	"github.com/concord-chat/concord/internal/protocol"
)

// Limits on the WebSocket (security audit 2026-11-05, finding 7). Before
// them one account could send as fast as its connection allowed, and
// max_connections wasn't enforced.

// identifyWithin closes a connection that hasn't identified by then (a
// variable for tests).
var identifyWithin = 30 * time.Second

// defaultMaxPerIP is max_connections_per_ip when unset.
const defaultMaxPerIP = 20

// Per-connection rates: a burst, then a steady rate in operations a second.
// Plugins draw frames and get more.
var (
	personRate = opRate{burst: 200, perSec: 50}
	pluginRate = opRate{burst: 2000, perSec: 500}
)

// opCost is how much an operation takes from the bucket; anything not
// listed costs 1. Searches and history are the expensive ones.
var opCost = map[protocol.OpCode]float64{
	protocol.OpSearch:          10,
	protocol.OpRequestMentions: 5,
	protocol.OpRequestMessages: 3,
	protocol.OpSendMessage:     2,
	protocol.OpWhisper:         2,
	protocol.OpHeartbeat:       0,
	protocol.OpIdentify:        0,
}

type opRate struct{ burst, perSec float64 }

// opLimiter is one connection's token bucket. Only the connection's read
// loop uses it, so it needs no lock.
type opLimiter struct {
	tokens   float64
	last     time.Time
	warnedAt time.Time
}

// allow takes op's cost from the bucket, refilled at rate since the last
// call. It reports false when there isn't enough.
func (l *opLimiter) allow(op protocol.OpCode, rate opRate, now time.Time) bool {
	cost, ok := opCost[op]
	if !ok {
		cost = 1
	}
	if l.last.IsZero() {
		l.tokens = rate.burst
	} else {
		l.tokens += now.Sub(l.last).Seconds() * rate.perSec
		if l.tokens > rate.burst {
			l.tokens = rate.burst
		}
	}
	l.last = now
	if l.tokens < cost {
		return false
	}
	l.tokens -= cost
	return true
}

// connLimits counts open WebSocket connections, in all and per address.
type connLimits struct {
	mu       sync.Mutex
	total    int
	perIP    map[string]int
	max      int // 0: no limit
	maxPerIP int // 0: no limit
}

func newConnLimits(max, maxPerIP int) *connLimits {
	return &connLimits{perIP: map[string]int{}, max: max, maxPerIP: maxPerIP}
}

// acquire counts a new connection from ip, or reports false when it would
// go over a limit. Loopback addresses have no per-address limit: everyone on
// Tor arrives from 127.0.0.1, as does everyone behind a local proxy that
// doesn't pass the real address on.
func (l *connLimits) acquire(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.max > 0 && l.total >= l.max {
		return false
	}
	if l.maxPerIP > 0 && !isLoopback(ip) && l.perIP[ip] >= l.maxPerIP {
		return false
	}
	l.total++
	l.perIP[ip]++
	return true
}

func (l *connLimits) release(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.total > 0 {
		l.total--
	}
	if n := l.perIP[ip]; n <= 1 {
		delete(l.perIP, ip)
	} else {
		l.perIP[ip] = n - 1
	}
}

func isLoopback(ip string) bool {
	p := net.ParseIP(ip)
	return p != nil && p.IsLoopback()
}
