package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/protocol"
)

// Connection represents a WebSocket connection to the server
type Connection struct {
	// WebSocket connection
	conn *websocket.Conn
	
	// Server address
	serverAddr string
	
	// Message handlers
	onMessage     func(*protocol.Message)
	onConnect     func()
	onDisconnect  func()
	onError       func(error)
	
	// State
	connected    bool
	authenticated bool
	sessionID    string
	lastSeq      int64
	
	// Channels
	send chan *protocol.Message
	done chan struct{}
	
	// Heartbeat
	heartbeatInterval time.Duration
	heartbeatTicker   *time.Ticker
	
	// Mutex for thread safety
	mu sync.RWMutex
}

// NewConnection creates a new connection instance
func NewConnection(serverAddr string) *Connection {
	return &Connection{
		serverAddr: serverAddr,
		send:       make(chan *protocol.Message, 256),
		done:       make(chan struct{}),
	}
}

// SetHandlers sets the message handlers
func (c *Connection) SetHandlers(onMessage func(*protocol.Message), onConnect func(), onDisconnect func(), onError func(error)) {
	c.onMessage = onMessage
	c.onConnect = onConnect
	c.onDisconnect = onDisconnect
	c.onError = onError
}

// Connect establishes a WebSocket connection
func (c *Connection) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if c.connected {
		return fmt.Errorf("already connected")
	}
	
	// Parse server address
	u, err := url.Parse(c.serverAddr)
	if err != nil {
		return fmt.Errorf("invalid server address: %w", err)
	}
	
	// Ensure WebSocket scheme
	if u.Scheme == "http" {
		u.Scheme = "ws"
	} else if u.Scheme == "https" {
		u.Scheme = "wss"
	}
	u.Path = "/ws"

	// Configure dialer with timeout
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		Proxy:            http.ProxyFromEnvironment,
	}

	// Connect with timeout
	conn, _, err := dialer.Dial(u.String(), nil)
	if err != nil {
		return fmt.Errorf("failed to connect: %w", err)
	}
	
	c.conn = conn
	c.connected = true
	c.done = make(chan struct{})
	
	// Start read/write pumps
	go c.readPump()
	go c.writePump()
	
	if c.onConnect != nil {
		c.onConnect()
	}
	
	return nil
}

// Disconnect closes the connection
func (c *Connection) Disconnect() {
	c.mu.Lock()
	defer c.mu.Unlock()
	
	if !c.connected {
		return
	}
	
	c.connected = false
	c.authenticated = false
	
	// Stop heartbeat
	if c.heartbeatTicker != nil {
		c.heartbeatTicker.Stop()
	}
	
	// Signal done
	close(c.done)
	
	// Close WebSocket
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage, 
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		c.conn.Close()
	}
	
	if c.onDisconnect != nil {
		c.onDisconnect()
	}
}

// IsConnected returns the connection state
func (c *Connection) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsAuthenticated returns the authentication state
func (c *Connection) IsAuthenticated() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authenticated
}

// Identify sends the IDENTIFY message to authenticate
func (c *Connection) Identify(token string) error {
	payload := &protocol.IdentifyPayload{
		Token: token,
		Properties: protocol.ConnectionProperties{
			OS:      "terminal",
			Browser: "concord-tui",
			Device:  "desktop",
		},
	}
	
	msg, err := protocol.NewMessage(protocol.OpIdentify, payload)
	if err != nil {
		return err
	}
	
	return c.Send(msg)
}

// SendMessage sends a chat message
func (c *Connection) SendMessage(channelID uuid.UUID, content string, replyTo *uuid.UUID) error {
	payload := &protocol.SendMessagePayload{
		ChannelID: channelID,
		Content:   content,
		ReplyToID: replyTo,
		Nonce:     uuid.New().String(),
	}
	
	msg, err := protocol.NewMessage(protocol.OpSendMessage, payload)
	if err != nil {
		return err
	}
	
	return c.Send(msg)
}

// SendTyping sends a typing indicator
func (c *Connection) SendTyping(channelID uuid.UUID) error {
	payload := &protocol.TypingStartPayload{
		ChannelID: channelID,
	}
	
	msg, err := protocol.NewMessage(protocol.OpTypingStart, payload)
	if err != nil {
		return err
	}
	
	return c.Send(msg)
}

// UpdatePresence updates the user's presence
func (c *Connection) UpdatePresence(status models.UserStatus, statusText string) error {
	payload := &protocol.PresenceUpdatePayload{
		Status:     status,
		StatusText: statusText,
	}
	
	msg, err := protocol.NewMessage(protocol.OpPresenceUpdate, payload)
	if err != nil {
		return err
	}
	
	return c.Send(msg)
}

// RequestServerData requests data for a server
func (c *Connection) RequestServerData(serverID uuid.UUID) error {
	payload := map[string]interface{}{
		"server_id": serverID,
	}
	
	msg, err := protocol.NewMessage(protocol.OpRequestGuild, payload)
	if err != nil {
		return err
	}
	
	return c.Send(msg)
}

// Send queues a message to be sent
func (c *Connection) Send(msg *protocol.Message) error {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()
	
	if !connected {
		return fmt.Errorf("not connected")
	}
	
	select {
	case c.send <- msg:
		return nil
	default:
		return fmt.Errorf("send buffer full")
	}
}

// readPump reads messages from the WebSocket
func (c *Connection) readPump() {
	defer func() {
		c.Disconnect()
	}()
	
	c.conn.SetReadLimit(512 * 1024) // 512KB
	c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	
	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
				if c.onError != nil {
					c.onError(err)
				}
			}
			return
		}
		
		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Printf("Failed to parse message: %v", err)
			continue
		}
		
		c.handleMessage(&msg)
	}
}

// writePump writes messages to the WebSocket
func (c *Connection) writePump() {
	ticker := time.NewTicker(54 * time.Second) // Ping interval
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	
	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			
			data, err := json.Marshal(msg)
			if err != nil {
				log.Printf("Failed to marshal message: %v", err)
				continue
			}
			
			if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
				log.Printf("Failed to write message: %v", err)
				return
			}
			
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
			
		case <-c.done:
			return
		}
	}
}

// handleMessage processes incoming messages
func (c *Connection) handleMessage(msg *protocol.Message) {
	// Update sequence number
	if msg.Seq != nil {
		c.mu.Lock()
		c.lastSeq = *msg.Seq
		c.mu.Unlock()
	}
	
	switch msg.Op {
	case protocol.OpHello:
		c.handleHello(msg)
		
	case protocol.OpHeartbeatAck:
		// Heartbeat acknowledged, connection is healthy
		
	case protocol.OpReady:
		c.handleReady(msg)
		
	case protocol.OpInvalidSession:
		c.handleInvalidSession(msg)
		
	case protocol.OpReconnect:
		// Server requested reconnection
		go func() {
			c.Disconnect()
			time.Sleep(time.Second)
			c.Connect()
		}()
		
	case protocol.OpDispatch:
		// Forward to message handler
		if c.onMessage != nil {
			c.onMessage(msg)
		}
	}
}

// handleHello processes the HELLO message
func (c *Connection) handleHello(msg *protocol.Message) {
	var payload protocol.HelloPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Printf("Failed to parse hello payload: %v", err)
		return
	}
	
	// Set heartbeat interval
	c.heartbeatInterval = time.Duration(payload.HeartbeatInterval) * time.Millisecond
	
	// Start heartbeat
	c.startHeartbeat()
}

// handleReady processes the READY message
func (c *Connection) handleReady(msg *protocol.Message) {
	var payload protocol.ReadyPayload
	if err := json.Unmarshal(msg.Data, &payload); err != nil {
		log.Printf("Failed to parse ready payload: %v", err)
		return
	}
	
	c.mu.Lock()
	c.authenticated = true
	c.sessionID = payload.SessionID
	c.mu.Unlock()
	
	// Forward to message handler
	if c.onMessage != nil {
		c.onMessage(msg)
	}
}

// handleInvalidSession processes authentication failure
func (c *Connection) handleInvalidSession(msg *protocol.Message) {
	c.mu.Lock()
	c.authenticated = false
	c.mu.Unlock()
	
	if c.onError != nil {
		c.onError(fmt.Errorf("invalid session"))
	}
}

// startHeartbeat begins the heartbeat loop
func (c *Connection) startHeartbeat() {
	if c.heartbeatTicker != nil {
		c.heartbeatTicker.Stop()
	}
	
	c.heartbeatTicker = time.NewTicker(c.heartbeatInterval)
	
	go func() {
		for {
			select {
			case <-c.heartbeatTicker.C:
				c.sendHeartbeat()
			case <-c.done:
				return
			}
		}
	}()
}

// sendHeartbeat sends a heartbeat message
func (c *Connection) sendHeartbeat() {
	c.mu.RLock()
	seq := c.lastSeq
	c.mu.RUnlock()
	
	payload := &protocol.HeartbeatPayload{
		LastSequence: &seq,
	}
	
	msg, err := protocol.NewMessage(protocol.OpHeartbeat, payload)
	if err != nil {
		return
	}
	
	c.Send(msg)
}

// GetLastSequence returns the last received sequence number
func (c *Connection) GetLastSequence() int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastSeq
}

// LoginResponse represents the response from the login endpoint
type LoginResponse struct {
	User  *models.User `json:"user"`
	Token string       `json:"token"`
}

// Login authenticates with the server using email and password
func (c *Connection) Login(email, password string) (*models.User, string, error) {
	// Parse server address to get HTTP URL
	u, err := url.Parse(c.serverAddr)
	if err != nil {
		return nil, "", fmt.Errorf("invalid server address: %w", err)
	}

	// Convert to HTTP scheme
	if u.Scheme == "ws" {
		u.Scheme = "http"
	} else if u.Scheme == "wss" {
		u.Scheme = "https"
	}
	u.Path = "/api/login"

	// Create request body
	reqBody := map[string]string{
		"email":    email,
		"password": password,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Post(u.String(), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("login failed: %s", string(body))
	}

	// Parse response
	var loginResp LoginResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %w", err)
	}

	return loginResp.User, loginResp.Token, nil
}

// Register creates a new account on the server
func (c *Connection) Register(username, email, password string) (*models.User, string, error) {
	// Parse server address to get HTTP URL
	u, err := url.Parse(c.serverAddr)
	if err != nil {
		return nil, "", fmt.Errorf("invalid server address: %w", err)
	}

	// Convert to HTTP scheme
	if u.Scheme == "ws" {
		u.Scheme = "http"
	} else if u.Scheme == "wss" {
		u.Scheme = "https"
	}
	u.Path = "/api/register"

	// Create request body
	reqBody := map[string]string{
		"username": username,
		"email":    email,
		"password": password,
	}
	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, "", fmt.Errorf("failed to marshal request: %w", err)
	}

	// Make HTTP request with timeout
	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Post(u.String(), "application/json", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, "", fmt.Errorf("failed to connect to server: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read response: %w", err)
	}

	// Check status code
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("registration failed: %s", string(body))
	}

	// Parse response
	var loginResp LoginResponse
	if err := json.Unmarshal(body, &loginResp); err != nil {
		return nil, "", fmt.Errorf("failed to parse response: %w", err)
	}

	return loginResp.User, loginResp.Token, nil
}
