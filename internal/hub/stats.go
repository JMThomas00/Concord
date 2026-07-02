package hub

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stats holds live counters and a log ring buffer, consumed by the dashboard.
type Stats struct {
	StartTime time.Time

	Registrations    atomic.Int64
	Deregistrations  atomic.Int64
	Heartbeats       atomic.Int64
	JoinsServed      atomic.Int64
	JoinsRateLimited atomic.Int64
	FederationSyncs  atomic.Int64

	logMu sync.Mutex
	logs  []string
}

const maxLogLines = 500

func newStats() *Stats {
	return &Stats{StartTime: time.Now()}
}

// Write implements io.Writer so the standard logger's output can feed the
// dashboard's log pane (via log.SetOutput).
func (s *Stats) Write(p []byte) (int, error) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	for _, line := range strings.Split(strings.TrimRight(string(p), "\n"), "\n") {
		if line != "" {
			s.logs = append(s.logs, line)
		}
	}
	if len(s.logs) > maxLogLines {
		s.logs = s.logs[len(s.logs)-maxLogLines:]
	}
	return len(p), nil
}

// LogTail returns a copy of the last n log lines.
func (s *Stats) LogTail(n int) []string {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	start := 0
	if len(s.logs) > n {
		start = len(s.logs) - n
	}
	return append([]string(nil), s.logs[start:]...)
}

// Uptime returns time elapsed since the hub started.
func (s *Stats) Uptime() time.Duration {
	return time.Since(s.StartTime)
}
