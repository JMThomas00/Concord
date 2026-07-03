package hub

import "time"

// RegisteredServer is the internal representation of a server registered with this hub.
type RegisteredServer struct {
	ID                 string
	Name               string
	Description        string
	Category           string
	Tags               []string
	Host               string
	Port               int
	RegistrationSecret string
	MemberCount        int
	OnlineCount        int
	MaxMembers         int
	IsOnline           bool
	LastHeartbeat      time.Time
	RegisteredAt       time.Time
	UpdatedAt          time.Time
}

// ServerListing is the public view of a registered server, safe to expose via the API.
// Host and Port are intentionally omitted; clients obtain connection details via a join token.
type ServerListing struct {
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
	FromHub     string   `json:"from_hub,omitempty"`
}

// PeerHub represents another hub instance that this hub federates with.
type PeerHub struct {
	ID           string
	Name         string
	URL          string
	IsActive     bool
	LastSynced   *time.Time
	RegisteredAt time.Time
}

// JoinToken is a single-use token that authorizes a client to connect to a specific server.
type JoinToken struct {
	Token     string
	ServerID  string
	CreatedAt time.Time
	ExpiresAt time.Time
	Used      bool
}

// RegisterRequest is the payload a Concord server sends to register itself with the hub.
type RegisterRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Category    string   `json:"category"`
	Tags        []string `json:"tags"`
	Host        string   `json:"host"`
	Port        int      `json:"port"`
	MaxMembers  int      `json:"max_members"`
}

// RegisterResponse is returned to the server after successful registration.
type RegisterResponse struct {
	ServerID           string `json:"server_id"`
	RegistrationSecret string `json:"registration_secret"`
}

// HeartbeatRequest is sent periodically by a registered server to report its current stats.
type HeartbeatRequest struct {
	MemberCount int `json:"member_count"`
	OnlineCount int `json:"online_count"`
}

// JoinResponse is returned to a client that requests a join token for a listed server.
type JoinResponse struct {
	ServerID    string `json:"server_id"`
	DisplayName string `json:"display_name"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	JoinToken   string `json:"join_token"`
}

// HubListing is the public representation of a peer hub, used in federation discovery.
type HubListing struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

// ErrorResponse is a helper struct for writing JSON error responses.
type ErrorResponse struct {
	Error string `json:"error"`
}

// ToListing returns the public-safe view of this server (host/port omitted).
func (s RegisteredServer) ToListing() ServerListing {
	return ServerListing{
		ID:          s.ID,
		Name:        s.Name,
		Description: s.Description,
		Category:    s.Category,
		Tags:        s.Tags,
		MemberCount: s.MemberCount,
		OnlineCount: s.OnlineCount,
		MaxMembers:  s.MaxMembers,
		IsOnline:    s.IsOnline,
		LastSeen:    relativeTime(s.LastHeartbeat, s.IsOnline),
	}
}
