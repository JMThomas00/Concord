package client

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// PingResult contains the result of a server ping
type PingResult struct {
	Success    bool
	Latency    time.Duration
	Error      string
	Attempts   int // Number of ping attempts made
	Timestamp  time.Time
	InProgress bool
}

// ServerPingResultMsg is sent when a ping completes
type ServerPingResultMsg struct {
	ServerID uuid.UUID
	Result   *PingResult
}

// PingServer attempts to ping a server and check its health
// Retries up to maxAttempts times on failure
func PingServer(address string, port int, useTLS bool, timeout time.Duration) *PingResult {
	maxAttempts := 3
	attemptTimeout := timeout / time.Duration(maxAttempts)

	addr := net.JoinHostPort(address, strconv.Itoa(port))
	protocol := "http"
	if useTLS {
		protocol = "https"
	}

	var lastError error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		start := time.Now()

		// Try TCP connection
		ctx, cancel := context.WithTimeout(context.Background(), attemptTimeout)
		if viaTor(addr) {
			ctx, cancel = context.WithTimeout(context.Background(), torWait+attemptTimeout)
		}
		conn, err := netDial(ctx, "tcp", addr)
		cancel()
		if err != nil {
			lastError = err
			if attempt < maxAttempts {
				time.Sleep(500 * time.Millisecond) // Brief pause between retries
			}
			continue
		}
		conn.Close()

		latency := time.Since(start)

		// Try HTTP health check
		healthURL := fmt.Sprintf("%s://%s/api/health", protocol, addr)
		client := httpClientFor(addr, attemptTimeout)

		resp, err := client.Get(healthURL)
		if err != nil {
			// TCP worked but HTTP failed - still consider it a success
			return &PingResult{
				Success:   true,
				Latency:   latency,
				Attempts:  attempt,
				Timestamp: time.Now(),
			}
		}
		defer resp.Body.Close()

		return &PingResult{
			Success:   resp.StatusCode == 200,
			Latency:   latency,
			Attempts:  attempt,
			Timestamp: time.Now(),
		}
	}

	// All attempts failed
	errorMsg := "connection refused"
	if lastError != nil {
		errorMsg = lastError.Error()
	}

	return &PingResult{
		Success:   false,
		Error:     errorMsg,
		Attempts:  maxAttempts,
		Timestamp: time.Now(),
	}
}

// PingServerCmd creates a bubbletea command to ping a server
func PingServerCmd(serverInfo *ClientServerInfo) tea.Cmd {
	return func() tea.Msg {
		result := PingServer(
			serverInfo.Address,
			serverInfo.Port,
			serverInfo.UseTLS,
			5*time.Second,
		)

		return ServerPingResultMsg{
			ServerID: serverInfo.ID,
			Result:   result,
		}
	}
}
