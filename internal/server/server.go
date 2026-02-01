package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"golang.org/x/crypto/bcrypt"
)

// Config holds the server configuration
type Config struct {
	Host           string `toml:"host"`
	Port           int    `toml:"port"`
	DatabasePath   string `toml:"database_path"`
	MaxConnections int    `toml:"max_connections"`
	Debug          bool   `toml:"debug"`
}

// DefaultConfig returns the default server configuration
func DefaultConfig() *Config {
	return &Config{
		Host:           "0.0.0.0",
		Port:           8080,
		DatabasePath:   "concord.db",
		MaxConnections: 1000,
		Debug:          false,
	}
}

// Server represents the Concord server
type Server struct {
	config   *Config
	hub      *Hub
	handlers *Handlers
	db       *database.DB
	upgrader websocket.Upgrader
	httpServer *http.Server
}

// New creates a new server instance
func New(config *Config) (*Server, error) {
	// Open database
	db, err := database.New(config.DatabasePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Create hub
	hub := NewHub()

	// Create handlers
	handlers := NewHandlers(db, hub)

	// Create server
	s := &Server{
		config:   config,
		hub:      hub,
		handlers: handlers,
		db:       db,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(r *http.Request) bool {
				// In production, you should check the origin
				return true
			},
		},
	}

	return s, nil
}

// Run starts the server
func (s *Server) Run() error {
	// Start the hub
	go s.hub.Run()

	// Set up HTTP routes
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/api/register", s.handleRegister)
	mux.HandleFunc("/api/login", s.handleLogin)
	mux.HandleFunc("/api/health", s.handleHealth)

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

	log.Printf("Concord server starting on %s", addr)
	log.Printf("WebSocket endpoint: ws://%s/ws", addr)
	log.Printf("API endpoint: http://%s/api", addr)

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
	log.Println("Shutting down server...")

	// Create a deadline for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := s.httpServer.Shutdown(ctx); err != nil {
		log.Printf("HTTP server shutdown error: %v", err)
	}

	// Close database
	if err := s.db.Close(); err != nil {
		log.Printf("Database close error: %v", err)
	}

	log.Println("Server stopped")
}

// handleWebSocket handles WebSocket upgrade requests
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
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
		log.Printf("Failed to hash password: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// Create user
	user := models.NewUser(req.Username, req.Email)

	if err := s.db.CreateUser(user, string(passwordHash)); err != nil {
		log.Printf("Failed to create user: %v", err)
		http.Error(w, "Failed to create user (email or username may already exist)", http.StatusConflict)
		return
	}

	// Generate auth token
	token, err := s.handlers.CreateAuthToken(user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Failed to create auth token: %v", err)
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
	user, passwordHash, err := s.db.GetUserByEmail(req.Email)
	if err != nil {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Check password
	if err := bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(req.Password)); err != nil {
		http.Error(w, "Invalid email or password", http.StatusUnauthorized)
		return
	}

	// Generate auth token
	token, err := s.handlers.CreateAuthToken(user.ID, r.RemoteAddr, r.UserAgent())
	if err != nil {
		log.Printf("Failed to create auth token: %v", err)
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
