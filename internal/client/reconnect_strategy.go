package client

import (
	"math"
	"time"
)

// ReconnectStrategy defines the reconnection behavior
type ReconnectStrategy struct {
	MaxRetries    int
	InitialDelay  time.Duration
	MaxDelay      time.Duration
	BackoffFactor float64
}

// DefaultReconnectStrategy returns the default reconnection strategy
func DefaultReconnectStrategy() *ReconnectStrategy {
	return &ReconnectStrategy{
		MaxRetries:    5,
		InitialDelay:  2 * time.Second,
		MaxDelay:      30 * time.Second,
		BackoffFactor: 2.0,
	}
}

// NextDelay calculates the delay for the next retry attempt
func (rs *ReconnectStrategy) NextDelay(attemptCount int) time.Duration {
	delay := float64(rs.InitialDelay) * math.Pow(rs.BackoffFactor, float64(attemptCount))
	if delay > float64(rs.MaxDelay) {
		return rs.MaxDelay
	}
	return time.Duration(delay)
}

// ShouldRetry determines if another retry attempt should be made
func (rs *ReconnectStrategy) ShouldRetry(attemptCount int) bool {
	return attemptCount < rs.MaxRetries
}
