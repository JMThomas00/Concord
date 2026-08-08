package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pelletier/go-toml/v2"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
	"github.com/concord-chat/concord/internal/protocol"
	"github.com/concord-chat/concord/internal/server/dashboard"
	"golang.org/x/crypto/bcrypt"
)

// Config holds the server configuration
type Config struct {
	Host           string               `toml:"host"`
	Port           int                  `toml:"port"`
	ServerName     string               `toml:"server_name"`
	DatabasePath   string               `toml:"database_path"`
	MaxConnections int                  `toml:"max_connections"`
	Debug          bool                 `toml:"debug"`
	MessagePruning MessagePruningConfig `toml:"message_pruning"`
	TermsAccepted  bool                 `toml:"terms_accepted"` // Whether ToS has been accepted
	AdminEmail     string               `toml:"admin_email"`    // Admin email for auto-granting admin role
	Grapevine      GrapevineConfig      `toml:"grapevine"`
	PluginsDir     string               `toml:"plugins_dir"` // Folder scanned for plugin.toml subfolders at startup
}

// MessagePruningConfig configures automatic message pruning
type MessagePruningConfig struct {
	Enabled       bool `toml:"enabled"`        // Default: true
	IntervalHours int  `toml:"interval_hours"` // Default: 24 (daily)
}

// DefaultConfig returns the default server configuration
func DefaultConfig() *Config {
	return &Config{
		Host:           "0.0.0.0",
		Port:           8080,
		ServerName:     "Concord Server",
		DatabasePath:   "concord.db",
		MaxConnections: 1000,
		Debug:          false,
		MessagePruning: MessagePruningConfig{
			Enabled:       true,
			IntervalHours: 24,
		},
		PluginsDir: "Plugins",
	}
}

// Server represents the Concord server
type Server struct {
	config        *Config
	hub           *Hub
	handlers      *Handlers
	db            *database.DB
	upgrader      websocket.Upgrader
	httpServer    *http.Server

	// Dashboard support
	dashboardMode bool
	dashboard     *dashboard.Model
	stats         *StatsTracker

	// Grapevine discovery
	configPath      string
	grapevine       *GrapevineClient
	grapevineTokens *tokenStore

	// Plugin platform
	plugins *plugins.Manager
}

// New creates a new server instance
func New(config *Config) (*Server, error) {
	// Open database
	db, err := database.New(config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Ensure default server exists
	defaultServer, _, err := db.EnsureDefaultServer(config.ServerName)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ensure default server: %w", err)
	}
	DBLog.Info("Default server initialized", "server_id", defaultServer.ID, "name", defaultServer.Name)

	// Create hub
	hub := NewHub()

	// Create stats tracker
	stats := NewStatsTracker()

	// Plugin platform: connect host defaults to loopback since "0.0.0.0" is a
	// bind address, not something a locally-spawned plugin process can dial.
	connectHost := config.Host
	if connectHost == "" || connectHost == "0.0.0.0" {
		connectHost = "127.0.0.1"
	}
	wsURL := fmt.Sprintf("ws://%s:%d/ws", connectHost, config.Port)
	pluginManager := plugins.NewManager(db, wsURL, PluginLog)

	// Create handlers
	handlers := NewHandlers(db, hub, stats, pluginManager)

	// Register voice disconnect cleanup callback so the hub can trigger DB/broadcast
	// cleanup without importing the handlers package (avoids circular dependency).
	hub.SetVoiceLeaveCallback(func(userID, serverID, channelID uuid.UUID) {
		handlers.handleVoiceLeave(userID, serverID, channelID)
	})

	// Create server
	s := &Server{
		config:          config,
		hub:             hub,
		handlers:        handlers,
		db:              db,
		stats:           stats,
		grapevineTokens: newTokenStore(),
		plugins:         pluginManager,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}

	pluginsDir := config.PluginsDir
	if pluginsDir == "" {
		pluginsDir = "Plugins"
	}
	if err := pluginManager.LoadAll(pluginsDir); err != nil {
		DBLog.Warn("plugin platform failed to load", "error", err)
	}

	return s, nil
}

// SetConfigPath tells the server where its config file lives so Grapevine can
// write back server_id and registration_secret after first registration.
func (s *Server) SetConfigPath(path string) {
	s.configPath = path
}

// saveConfig re-marshals the entire Config and writes it back to disk.
func (s *Server) saveConfig() error {
	if s.configPath == "" {
		return fmt.Errorf("config path not set")
	}
	data, err := toml.Marshal(s.config)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	return os.WriteFile(s.configPath, data, 0644)
}

// startGrapevine registers Grapevine routes on mux and starts the background client.
func (s *Server) startGrapevine(mux *http.ServeMux) {
	mux.HandleFunc("/v1/grapevine/ping", s.handleGrapevinePing)
	mux.HandleFunc("/v1/grapevine/signal", s.handleGrapevineSignal)
	mux.HandleFunc("/v1/grapevine/join", s.handleGrapevineJoin)

	// Periodic cleanup of expired join tokens.
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			s.grapevineTokens.purgeExpired()
		}
	}()

	// Resolve public host/port for registration.
	cfg := &s.config.Grapevine
	publicHost := cfg.PublicHost
	if publicHost == "" {
		publicHost = s.config.Host
		if publicHost == "0.0.0.0" || publicHost == "" {
			publicHost = "localhost"
		}
	}
	publicPort := cfg.PublicPort
	if publicPort == 0 {
		publicPort = s.config.Port
	}

	s.grapevine = newGrapevineClient(
		cfg,
		s.config.ServerName,
		publicHost,
		publicPort,
		func() int { return s.db.GetTotalMemberCount() },
		func() int { return s.hub.ConnectedClientCount() },
		s.saveConfig,
	)
	s.grapevine.Start()
}

// Run starts the server
func (s *Server) Run() error {
	// If dashboard mode is enabled, use hybrid dashboard runner
	if s.dashboardMode {
		return s.runWithHybridDashboard()
	}

	// Normal mode: Start the hub
	go s.hub.Run()

	// Start cleanup tasks for expired timeouts and mutes
	go s.runCleanupTasks()

	// Start message pruning task
	if s.config.MessagePruning.Enabled {
		go s.runMessagePruningTask()
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/health", s.handleHealth)
	if s.config.Grapevine.Enabled {
		s.startGrapevine(mux)
	}

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Handle graceful shutdown
	go s.handleShutdown()

	Logger.Info("Concord server starting", "address", addr)
	Logger.Debug("WebSocket endpoint ready", "endpoint", "ws://"+addr+"/ws")
	Logger.Debug("API endpoint ready", "endpoint", "http://"+addr+"/api")

	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// handleShutdown handles graceful server shutdown
func (s *Server) handleShutdown() {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	Logger.Warn("Shutting down server...")

	// Deregister from Grapevine hub before closing
	if s.grapevine != nil {
		s.grapevine.Stop()
	}

	// Stop all supervised plugin processes before closing the database they
	// (and Concord) share.
	if s.plugins != nil {
		s.plugins.Shutdown()
	}

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		Logger.Error("HTTP server shutdown error", "error", err)
	}

	// Close database
	if err := s.db.Close(); err != nil {
		DBLog.Error("Database close error", "error", err)
	}

	Logger.Info("Server stopped")
}

// runCleanupTasks periodically cleans up expired timeouts and mutes
func (s *Server) runCleanupTasks() {
	ticker := time.NewTicker(1 * time.Minute) // Run every minute
	defer ticker.Stop()

	for range ticker.C {
		// Cleanup expired timeouts and send expiry notifications
		expiredTimeouts, err := s.db.CleanupExpiredTimeouts()
		if err != nil {
			DBLog.Error("Failed to cleanup expired timeouts", "error", err)
		}

		// For each expired timeout, send notification to the channel where it was issued
		for _, entry := range expiredTimeouts {
			user, err := s.db.GetUserByID(entry.UserID)
			if err != nil {
				continue
			}

			// Send funny timeout expiry message to the channel where the timeout was issued
			msg := getRandomMessage(timeoutExpiryMessages, user.Username)

			// Create system message
			systemMsg := &models.Message{
				ID:        uuid.New(),
				ChannelID: entry.ChannelID,
				AuthorID:  uuid.Nil,
				Content:   msg,
				Type:      models.MessageTypeSystem,
				CreatedAt: time.Now(),
			}

			if err := s.db.CreateMessage(systemMsg); err == nil {
				// Broadcast the system message
				payload := &protocol.SystemMessagePayload{
					ChannelID: entry.ChannelID,
					Content:   msg,
					Timestamp: time.Now(),
				}
				s.hub.BroadcastToChannel(entry.ChannelID, protocol.EventSystemMessage, payload, nil)
			}
		}

		// Cleanup expired mutes and auto-unmute users
		unmutedUsers, err := s.db.CleanupExpiredMutes()
		if err != nil {
			DBLog.Error("Failed to cleanup expired mutes", "error", err)
			continue
		}

		// For each unmuted user, update their mute state and send notification
		for _, entry := range unmutedUsers {
			// Update mute state in server_members table
			if err := s.db.SetMemberMuted(entry.ServerID, entry.UserID, false); err != nil {
				DBLog.Error("Failed to unmute user", "user_id", entry.UserID, "error", err)
				continue
			}

			// Send system message about auto-unmute to the channel where the mute was issued
			user, err := s.db.GetUserByID(entry.UserID)
			if err != nil {
				continue
			}

			msg := fmt.Sprintf("🔊 %s has been automatically unmuted (mute timer expired).", user.Username)

			// Create system message
			systemMsg := &models.Message{
				ID:        uuid.New(),
				ChannelID: entry.ChannelID,
				AuthorID:  uuid.Nil,
				Content:   msg,
				Type:      models.MessageTypeSystem,
				CreatedAt: time.Now(),
			}

			if err := s.db.CreateMessage(systemMsg); err == nil {
				// Broadcast the system message
				payload := &protocol.SystemMessagePayload{
					ChannelID: entry.ChannelID,
					Content:   msg,
					Timestamp: time.Now(),
				}
				s.hub.BroadcastToChannel(entry.ChannelID, protocol.EventSystemMessage, payload, nil)
			}

			// Broadcast member update
			s.handlers.broadcastMemberUpdate(entry.ServerID, entry.UserID)
		}
	}
}

// runMessagePruningTask periodically prunes old messages according to retention policies
func (s *Server) runMessagePruningTask() {
	interval := time.Duration(s.config.MessagePruning.IntervalHours) * time.Hour
	if interval == 0 {
		interval = 24 * time.Hour // Default to daily
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once on startup (after 5-minute delay to avoid startup churn)
	time.Sleep(5 * time.Minute)
	s.performAutomaticPruning()

	for range ticker.C {
		s.performAutomaticPruning()
	}
}

// performAutomaticPruning executes message pruning across all servers
func (s *Server) performAutomaticPruning() {
	Logger.Info("Starting automatic message pruning")
	start := time.Now()

	// Get all servers (in a real multi-server setup, this would get all servers)
	// For now, we'll just get the default server and process all its channels
	servers, err := s.db.GetAllServers()
	if err != nil {
		DBLog.Error("Failed to get servers for pruning", "error", err)
		return
	}

	totalDeleted := 0

	for _, server := range servers {
		results, err := s.db.PruneServerMessages(server.ID)
		if err != nil {
			DBLog.Error("Failed to prune server", "server_id", server.ID, "error", err)
			continue
		}

		// Record history for each channel that had deletions
		for channelID, stats := range results {
			history := &models.MessagePruneHistory{
				ID:              uuid.New(),
				ServerID:        server.ID,
				ChannelID:       &channelID,
				MessagesDeleted: stats.TotalDeleted,
				TimeBasedCount:  stats.TimeBasedDeleted,
				CountBasedCount: stats.CountBasedDeleted,
				TriggerType:     "automatic",
				TriggeredBy:     nil,
				ExecutedAt:      time.Now(),
				DurationMs:      stats.DurationMs,
			}
			if err := s.db.RecordPruneHistory(history); err != nil {
				DBLog.Error("Failed to record prune history", "error", err)
			}
			totalDeleted += stats.TotalDeleted
		}
	}

	if totalDeleted > 0 {
		Logger.Info("Automatic pruning completed", "messages_deleted", totalDeleted, "duration", time.Since(start))
	}
}

// handleWebSocket handles WebSocket upgrade requests
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		ClientLog.Error("WebSocket upgrade failed", "error", err, "remote_addr", r.RemoteAddr)
		return
	}

	client := NewClient(conn, s.hub, s.handlers)

	// Send hello message
	client.SendHello()

	// Start client pumps
	go client.WritePump()
	go client.ReadPump()
}

// handleRegister handles user registration
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate input
	if len(req.Username) < 2 || len(req.Username) > 32 {
		http.Error(w, "Username must be 2-32 characters", http.StatusBadRequest)
		return
	}

	if len(req.Password) < 8 {
		http.Error(w, "Password must be at least 8 characters", http.StatusBadRequest)
		return
	}

	// Hash password
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		AuthLog.Error("Failed to hash password", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create user
	user := models.NewUser(req.Username, req.Email)
	AuthLog.Info("Creating user", "user_id", user.ID, "username", user.Username, "email", user.Email, "discriminator", user.Discriminator)

	if err := s.db.CreateUser(user, string(passwordHash)); err != nil {
		// UNIQUE constraint here is expected: the client auto-connect flow
		// falls through to registration when login fails, but the account
		// may already exist. The client will retry with login on 409.
		AuthLog.Warn("Failed to create user (duplicate email/username — client will retry login)", "error", err, "username", req.Username, "email", req.Email)
		http.Error(w, "Failed to create user (email or username may already exist)", http.StatusConflict)
		return
	}

	AuthLog.Info("User created successfully", "user_id", user.ID, "username", user.Username, "discriminator", user.Discriminator)

	// Verify user was created by trying to retrieve it
	retrievedUser, err := s.db.GetUserByID(user.ID)
	if err != nil {
		AuthLog.Error("User was created but cannot be retrieved", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error: user created but not retrievable", http.StatusInternalServerError)
		return
	}
	AuthLog.Debug("User retrieval verified", "user_id", retrievedUser.ID, "username", retrievedUser.Username)

	// Add user to default server
	defaultServer, everyoneRole, err := s.db.EnsureDefaultServer(s.config.ServerName)
	if err != nil {
		DBLog.Error("Failed to get default server", "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create server member
	member := &models.ServerMember{
		UserID:    user.ID,
		ServerID:  defaultServer.ID,
		JoinedAt:  time.Now(),
		IsMuted:   false,
		IsDeafened: false,
	}
	if err := s.db.AddServerMember(member); err != nil {
		DBLog.Error("Failed to add user to default server", "user_id", user.ID, "server_id", defaultServer.ID, "error", err)
		// Don't fail registration, just log the error
	} else {
		DBLog.Info("User added to default server", "user_id", user.ID, "server_id", defaultServer.ID)
	}

	// Assign @everyone role
	if err := s.db.AddMemberRole(user.ID, defaultServer.ID, everyoneRole.ID); err != nil {
		DBLog.Error("Failed to assign @everyone role", "user_id", user.ID, "role_id", everyoneRole.ID, "error", err)
		// Don't fail registration, just log the error
	} else {
		DBLog.Info("User assigned @everyone role", "user_id", user.ID, "role_id", everyoneRole.ID)
	}

	// Auto-grant admin to the very first real user
	if count, err := s.db.CountRealUsers(); err == nil && count == 1 {
		if err := s.db.EnsureAdminRole(user.Email); err != nil {
			AuthLog.Error("Failed to auto-grant admin to first user", "email", user.Email, "error", err)
		} else {
			AuthLog.Info("First registrant granted Admin role", "email", user.Email)
		}
	}

	// Auto-grant admin to user matching configured admin email
	if s.config.AdminEmail != "" && strings.EqualFold(user.Email, s.config.AdminEmail) {
		if err := s.db.EnsureAdminRole(user.Email); err != nil {
			AuthLog.Error("Failed to grant admin role to configured admin email", "email", user.Email, "error", err)
		} else {
			AuthLog.Info("Admin role granted to user matching configured admin email", "email", user.Email)
		}
	}

	// Generate auth token
	token, err := s.handlers.CreateAuthToken(user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		AuthLog.Error("Failed to create auth token", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	AuthLog.Debug("Auth token created", "user_id", user.ID)

	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// handleLogin handles user login
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Look up user
	AuthLog.Info("Login attempt", "email", req.Email)
	user, passwordHash, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		AuthLog.Warn("Login failed - user not found", "email", req.Email, "error", err)
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}
	AuthLog.Debug("User found for login", "user_id", user.ID, "username", user.Username, "discriminator", user.Discriminator)

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Ensure user is a member of the default server
	defaultServer, everyoneRole, err := s.db.EnsureDefaultServer(s.config.ServerName)
	if err != nil {
		DBLog.Error("Failed to get default server", "error", err)
		// Don't fail login, continue
	} else {
		// Check if user is already a member
		_, err := s.db.GetServerMember(defaultServer.ID, user.ID)
		if err != nil {
			// User is not a member, add them
			member := &models.ServerMember{
				UserID:     user.ID,
				ServerID:   defaultServer.ID,
				JoinedAt:   time.Now(),
				IsMuted:    false,
				IsDeafened: false,
			}
			if err := s.db.AddServerMember(member); err != nil {
				DBLog.Error("Failed to add user to default server on login", "user_id", user.ID, "server_id", defaultServer.ID, "error", err)
			} else {
				DBLog.Info("User added to default server on login", "user_id", user.ID, "server_id", defaultServer.ID)

				// Assign @everyone role
				if err := s.db.AddMemberRole(user.ID, defaultServer.ID, everyoneRole.ID); err != nil {
					DBLog.Error("Failed to assign @everyone role on login", "user_id", user.ID, "role_id", everyoneRole.ID, "error", err)
				} else {
					DBLog.Info("User assigned @everyone role on login", "user_id", user.ID, "role_id", everyoneRole.ID)
				}
			}
		}
	}

	// Generate auth token
	token, err := s.handlers.CreateAuthToken(user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		AuthLog.Error("Failed to create auth token", "user_id", user.ID, "error", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Return response
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"user":  user,
		"token": token,
	})
}

// handleHealth returns server health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "ok",
		"time":   time.Now().UTC(),
	})
}

// SetDashboardMode enables or disables dashboard mode
func (s *Server) SetDashboardMode(enabled bool) {
	s.dashboardMode = enabled
	if enabled {
		s.dashboard = dashboard.NewModel()
	}
}

// LogDashboardEvent logs an event to the dashboard activity feed
func (s *Server) LogDashboardEvent(level, component, message string) {
	if s.dashboardMode && s.dashboard != nil {
		s.dashboard.AddActivityEvent(level, component, message)
	}
}

// runWithDashboard starts the server with an interactive TUI dashboard
func (s *Server) runWithDashboard() error {
	// Start the hub
	go s.hub.Run()

	// Start cleanup tasks
	go s.runCleanupTasks()

	// Start message pruning task
	if s.config.MessagePruning.Enabled {
		go s.runMessagePruningTask()
	}

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/health", s.handleHealth)
	if s.config.Grapevine.Enabled {
		s.startGrapevine(mux)
	}

	// Create HTTP server
	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start HTTP server in goroutine
	go func() {
		s.dashboard.AddActivityEvent("INFO", "SERVER", fmt.Sprintf("Server starting on %s", addr))
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.dashboard.AddActivityEvent("ERROR", "SERVER", fmt.Sprintf("HTTP server error: %v", err))
		}
	}()

	// Start dashboard update loop
	go s.updateDashboardLoop()

	// Run Bubble Tea program (blocks until quit)
	p := tea.NewProgram(s.dashboard, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		return fmt.Errorf("dashboard error: %w", err)
	}

	// Graceful shutdown after dashboard exits
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	return nil
}

// updateDashboardLoop periodically updates dashboard stats
func (s *Server) updateDashboardLoop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if s.dashboard == nil {
			return
		}

		// Update system stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		s.hub.mu.RLock()
		connectionCount := len(s.hub.clients)
		s.hub.mu.RUnlock()

		s.dashboard.UpdateSystemStats(
			s.stats.GetUptime(),
			m.Alloc/1024/1024, // Memory in MB
			runtime.NumGoroutine(),
			connectionCount,
			s.config.MaxConnections,
			s.stats.GetMessageRate(),
		)

		// Update client list
		clientInfos := s.getConnectedClientInfo()
		s.dashboard.UpdateClientList(clientInfos)

		// Update message stats
		totalMessages := s.stats.GetTotalMessages()
		channelCounts := s.getChannelMessageCounts()
		peakRate := s.stats.GetPeakRate()
		avgRate := s.stats.GetMessageRate()

		s.dashboard.UpdateMessageStats(
			int(totalMessages),
			channelCounts,
			peakRate,
			avgRate,
		)
	}
}

// getConnectedClientInfo returns information about connected clients
func (s *Server) getConnectedClientInfo() []*dashboard.ClientInfo {
	s.hub.mu.RLock()
	defer s.hub.mu.RUnlock()

	clients := make([]*dashboard.ClientInfo, 0, len(s.hub.clients))

	for _, client := range s.hub.clients {
		// Skip clients that haven't authenticated yet
		if client == nil || client.User == nil {
			continue
		}

		info := &dashboard.ClientInfo{
			Username:      client.User.Username,
			Discriminator: client.User.Discriminator,
			Status:        string(client.User.Status),
			Activity:      "Idle", // TODO: Detect typing activity
			LastSeen:      client.User.LastSeenAt,
		}

		clients = append(clients, info)
	}

	return clients
}

// getChannelMessageCounts returns message counts per channel (24h window)
func (s *Server) getChannelMessageCounts() map[string]int {
	counts := make(map[string]int)

	// Get all servers
	servers, err := s.db.GetAllServers()
	if err != nil {
		return counts
	}

	// For each server, get channels and count messages
	since := time.Now().Add(-24 * time.Hour)

	for _, srv := range servers {
		channels, err := s.db.GetServerChannels(srv.ID)
		if err != nil {
			continue
		}

		for _, ch := range channels {
			if ch.Type != 0 { // Only text channels
				continue
			}

			count, err := s.db.CountMessagesSince(ch.ID, since)
			if err != nil {
				continue
			}

			counts[ch.Name] = count
		}
	}

	return counts
}

// runWithHybridDashboard starts the server with hybrid inline dashboard
func (s *Server) runWithHybridDashboard() error {
	// Clear screen and move to home
	fmt.Print("\033[2J\033[H")

	// Print banner
	PrintBanner()

	// Calculate dashboard start line (banner is 15 lines + 1 blank line before dashboard)
	dashboardStartLine := 17

	// Create hybrid renderer
	renderer := dashboard.NewHybridRenderer()
	renderer.SetDashboardStartLine(dashboardStartLine)

	// Calculate scroll region start (after dashboard + blank lines + startup info)
	scrollRegionStart := dashboardStartLine + 7 + 2 + 14 // Dashboard (7) + blanks (2) + startup (14)
	renderer.SetScrollRegionStartLine(scrollRegionStart)

	// Print initial dashboard
	fmt.Println() // Blank line after banner
	fmt.Println(renderer.RenderInitial())
	fmt.Println() // Blank line after dashboard

	// Set up HTTP routes (needed for startup info)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/health", s.handleHealth)
	if s.config.Grapevine.Enabled {
		s.startGrapevine(mux)
	}

	addr := fmt.Sprintf("%s:%d", s.config.Host, s.config.Port)
	s.httpServer = &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Print startup info BEFORE setting scroll region (so it stays fixed)
	PrintStartupInfo(addr, s.config.DatabasePath)

	// Set scroll region (using pre-calculated scrollRegionStart value)
	fmt.Printf("\033[%d;r", scrollRegionStart) // Set scroll region from scrollRegionStart to bottom of screen

	// Move cursor to scroll region start for logs
	fmt.Printf("\033[%d;1H", scrollRegionStart)

	// Now start all the background services
	// Start the hub
	go s.hub.Run()

	// Start cleanup tasks
	go s.runCleanupTasks()

	// Start message pruning
	if s.config.MessagePruning.Enabled {
		go s.runMessagePruningTask()
	}

	// Start dashboard update loop
	go s.updateHybridDashboardLoop(renderer)

	// Handle graceful shutdown
	go s.handleShutdown()

	Logger.Info("Concord server starting", "address", addr)

	// Start HTTP server (logs will scroll below dashboard)
	if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
		return fmt.Errorf("server error: %w", err)
	}

	return nil
}

// updateHybridDashboardLoop periodically updates the hybrid dashboard
func (s *Server) updateHybridDashboardLoop(renderer *dashboard.HybridRenderer) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for range ticker.C {
		// Update system stats
		var m runtime.MemStats
		runtime.ReadMemStats(&m)

		s.hub.mu.RLock()
		connectionCount := len(s.hub.clients)
		s.hub.mu.RUnlock()

		renderer.UpdateSystemStats(
			s.stats.GetUptime(),
			m.Alloc/1024/1024,
			runtime.NumGoroutine(),
			connectionCount,
			s.config.MaxConnections,
			s.stats.GetMessageRate(),
		)

		// Update client list
		clientInfos := s.getConnectedClientInfo()
		renderer.UpdateClientList(clientInfos)

		// Update broadcast stats
		channelCounts := s.getChannelMessageCounts()
		renderer.UpdateBroadcastStats(channelCounts)

		// Update activity summary
		lastMsgTime := s.stats.GetLastMessageTime()
		lastConnTime := s.stats.GetLastConnectionTime()
		totalEvents := s.stats.GetTotalEvents()

		lastMsgStr := "Never"
		if !lastMsgTime.IsZero() {
			lastMsgStr = formatTimeAgo(time.Since(lastMsgTime))
		}

		lastConnStr := "Never"
		if !lastConnTime.IsZero() {
			lastConnStr = formatTimeAgo(time.Since(lastConnTime))
		}

		renderer.UpdateActivitySummary(lastMsgStr, lastConnStr, totalEvents)

		// Update dashboard in place
		renderer.UpdateInPlace()
	}
}

// formatTimeAgo formats a duration as a human-readable "time ago" string
func formatTimeAgo(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	days := int(d.Hours()) / 24
	return fmt.Sprintf("%dd", days)
}
