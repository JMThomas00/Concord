package client

import (
	"fmt"
	"net"
	"net/http"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

// PingResult contains the result of a server ping
type PingResult struct {
	Success    bool
	Latency    time.Duration
	Error      string
	Timestamp  time.Time
	InProgress bool
}

// ServerPingResultMsg is sent when a ping completes
type ServerPingResultMsg struct {
	ServerID uuid.UUID
	Result   *PingResult
}

// PingServer attempts to ping a server and check its health
func PingServer(address string, port int, useTLS bool, timeout time.Duration) *PingResult {
	start := time.Now()

	addr := fmt.Sprintf("%s:%d", address, port)
	conn, err := net.DialTimeout("tcp", addr, timeout)

	if err != nil {
		return &PingResult{
			Success:   false,
			Error:     err.Error(),
			Timestamp: time.Now(),
		}
	}
	defer conn.Close()

	latency := time.Since(start)

	// Try HTTP health check
	protocol := "http"
	if useTLS {
		protocol = "https"
	}

	healthURL := fmt.Sprintf("%s://%s/api/health", protocol, addr)
	client := &http.Client{Timeout: timeout}

	resp, err := client.Get(healthURL)
	if err != nil {
		// TCP worked but HTTP failed - still consider it a success
		return &PingResult{
			Success:   true,
			Latency:   latency,
			Timestamp: time.Now(),
		}
	}
	defer resp.Body.Close()

	return &PingResult{
		Success:   resp.StatusCode == 200,
		Latency:   latency,
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
