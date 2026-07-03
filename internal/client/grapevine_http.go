package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// GrapevineHTTPClient makes calls to a Grapevine hub's REST API.
type GrapevineHTTPClient struct {
	baseURL    string
	httpClient *http.Client
}

func newGrapevineHTTPClient(baseURL string) *GrapevineHTTPClient {
	return &GrapevineHTTPClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// ListServers fetches the public server listing. category and query may be empty.
func (g *GrapevineHTTPClient) ListServers(category, query string) ([]HubServerEntry, error) {
	u, err := url.Parse(g.baseURL + "/v1/servers")
	if err != nil {
		return nil, fmt.Errorf("invalid hub URL: %w", err)
	}
	q := u.Query()
	if category != "" {
		q.Set("category", category)
	}
	if query != "" {
		q.Set("q", query)
	}
	u.RawQuery = q.Encode()

	resp, err := g.httpClient.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("hub unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}

	var servers []HubServerEntry
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	return servers, nil
}

// ListHubs fetches peer hub listings.
func (g *GrapevineHTTPClient) ListHubs() ([]HubEntry, error) {
	resp, err := g.httpClient.Get(g.baseURL + "/v1/hubs")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var hubs []HubEntry
	if err := json.NewDecoder(resp.Body).Decode(&hubs); err != nil {
		return nil, err
	}
	return hubs, nil
}

// CheckHealth verifies the URL is a Grapevine hub and returns its name.
func (g *GrapevineHTTPClient) CheckHealth() (string, error) {
	resp, err := g.httpClient.Get(g.baseURL + "/v1/health")
	if err != nil {
		return "", fmt.Errorf("unreachable: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var body struct {
		HubName string `json:"hub_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("not a Grapevine hub")
	}
	return body.HubName, nil
}

// RequestJoin sends a join request and returns the server's connection details.
func (g *GrapevineHTTPClient) RequestJoin(serverID string) (*HubJoinResponse, error) {
	resp, err := g.httpClient.Post(
		fmt.Sprintf("%s/v1/join/%s", g.baseURL, serverID),
		"application/json", nil)
	if err != nil {
		return nil, fmt.Errorf("hub unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, fmt.Errorf("rate limited — please wait a moment")
	}
	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("server is offline")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hub returned HTTP %d", resp.StatusCode)
	}

	var jr HubJoinResponse
	if err := json.NewDecoder(resp.Body).Decode(&jr); err != nil {
		return nil, fmt.Errorf("invalid response: %w", err)
	}
	return &jr, nil
}

// RedeemJoinToken redeems a hub-issued join token directly with the Concord
// server. The server only accepts tokens the hub signaled to it moments
// earlier, which verifies the address we got from the hub is genuine.
func RedeemJoinToken(host string, port int, token string) error {
	payload, _ := json.Marshal(map[string]string{"join_token": token})
	httpClient := &http.Client{Timeout: 10 * time.Second}
	resp, err := httpClient.Post(
		fmt.Sprintf("http://%s:%d/v1/grapevine/join", host, port),
		"application/json", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("server unreachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("join token rejected — refresh the listing and try again")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// ── Response types ─────────────────────────────────────────────────────────────

// HubServerEntry is the client-side view of a server from the hub listing.
type HubServerEntry struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	MemberCount int      `json:"member_count"`
	OnlineCount int      `json:"online_count"`
	MaxMembers  int      `json:"max_members"`
	IsOnline    bool     `json:"is_online"`
	LastSeen    string   `json:"last_seen"`
	FromHub     string   `json:"from_hub"`
}

// HubEntry is the client-side view of a federated peer hub.
type HubEntry struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// HubJoinResponse contains the connection details returned by a hub join request.
type HubJoinResponse struct {
	ServerID    string `json:"server_id"`
	DisplayName string `json:"display_name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	JoinToken   string `json:"join_token"`
}
