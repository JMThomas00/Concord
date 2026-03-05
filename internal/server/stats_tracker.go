package server

import (
	"sync"
	"time"
)

// StatsTracker tracks server statistics and message rates
type StatsTracker struct {
	startTime      time.Time
	messageCount   int64
	messageSamples []int64 // Messages in last N seconds
	sampleInterval time.Duration
	mu             sync.RWMutex
	currentSample  int64 // Messages in current second

	// Activity tracking
	lastMessageTime    time.Time
	lastConnectionTime time.Time
	totalEvents        int
}

// NewStatsTracker creates a new stats tracker
func NewStatsTracker() *StatsTracker {
	st := &StatsTracker{
		startTime:      time.Now(),
		messageSamples: make([]int64, 60), // 60 seconds of samples
		sampleInterval: time.Second,
	}

	// Start background ticker to rotate samples
	go st.tickLoop()

	return st
}

// tickLoop runs in background to rotate message samples every second
func (s *StatsTracker) tickLoop() {
	ticker := time.NewTicker(s.sampleInterval)
	defer ticker.Stop()

	for range ticker.C {
		s.Tick()
	}
}

// RecordMessage records a single message being sent
func (s *StatsTracker) RecordMessage() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.messageCount++
	s.currentSample++
	s.lastMessageTime = time.Now()
	s.totalEvents++
}

// RecordConnection records a new client connection
func (s *StatsTracker) RecordConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.lastConnectionTime = time.Now()
	s.totalEvents++
}

// Tick rotates the sample window (called every second)
func (s *StatsTracker) Tick() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Rotate samples (shift left, add current sample at end)
	copy(s.messageSamples, s.messageSamples[1:])
	s.messageSamples[len(s.messageSamples)-1] = s.currentSample

	// Reset current sample counter
	s.currentSample = 0
}

// GetMessageRate returns the average message rate over the last 60 seconds
func (s *StatsTracker) GetMessageRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Calculate average over samples
	total := int64(0)
	count := 0
	for _, sample := range s.messageSamples {
		if sample > 0 {
			total += sample
			count++
		}
	}

	if count == 0 {
		return 0.0
	}

	return float64(total) / float64(count)
}

// GetPeakRate returns the peak message rate in the last 60 seconds
func (s *StatsTracker) GetPeakRate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	max := int64(0)
	for _, sample := range s.messageSamples {
		if sample > max {
			max = sample
		}
	}

	return float64(max)
}

// GetTotalMessages returns the total message count since server start
func (s *StatsTracker) GetTotalMessages() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.messageCount
}

// GetUptime returns the server uptime duration
func (s *StatsTracker) GetUptime() time.Duration {
	return time.Since(s.startTime)
}

// GetLastMessageTime returns the time of the last message
func (s *StatsTracker) GetLastMessageTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastMessageTime
}

// GetLastConnectionTime returns the time of the last connection
func (s *StatsTracker) GetLastConnectionTime() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.lastConnectionTime
}

// GetTotalEvents returns the total number of events (messages + connections)
func (s *StatsTracker) GetTotalEvents() int {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.totalEvents
}
