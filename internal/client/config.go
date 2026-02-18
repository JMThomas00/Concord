package client

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ServersConfig represents the top-level configuration structure for ~/.concord/servers.json
type ServersConfig struct {
	Version            int                   `json:"version"`
	Servers            []*ClientServerInfo   `json:"servers"`
	DefaultPreferences *DefaultPreferences   `json:"default_preferences,omitempty"`
}

// DefaultPreferences stores default user preferences for new server registrations
type DefaultPreferences struct {
	Username              string `json:"username,omitempty"`
	Email                 string `json:"email,omitempty"`
	AutoConnectOnStartup  bool   `json:"auto_connect_on_startup"`
}

// LocalIdentity stores the user's single identity used across all servers
type LocalIdentity struct {
	Alias    string `json:"alias"`
	Email    string `json:"email"`
	Password string `json:"password"` // plaintext in v0.1; will be encrypted in a future version
}

// AppConfig represents UI preferences stored in ~/.concord/config.json
type AppConfig struct {
	Version  int            `json:"version"`
	UI       UIConfig       `json:"ui"`
	Identity *LocalIdentity `json:"identity,omitempty"`
}

// UIConfig holds UI-related preferences
type UIConfig struct {
	Theme               string                       `json:"theme"`
	ShowMembersList     bool                         `json:"show_members_list"`
	CollapsedCategories map[string]map[string]bool   `json:"collapsed_categories,omitempty"` // serverID -> categoryID -> collapsed
	MutedChannels       []string                     `json:"muted_channels,omitempty"`       // channel UUIDs
}

// ConfigManager handles loading and saving configuration files
type ConfigManager struct {
	serversFilePath string
	configFilePath  string
	mu              sync.RWMutex
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() (*ConfigManager, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get user home directory: %w", err)
	}

	concordDir := filepath.Join(homeDir, ".concord")

	// Create ~/.concord directory if it doesn't exist
	if err := os.MkdirAll(concordDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create .concord directory: %w", err)
	}

	return &ConfigManager{
		serversFilePath: filepath.Join(concordDir, "servers.json"),
		configFilePath:  filepath.Join(concordDir, "config.json"),
	}, nil
}

// LoadServers loads the server list from ~/.concord/servers.json
func (cm *ConfigManager) LoadServers() (*ServersConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(cm.serversFilePath); os.IsNotExist(err) {
		// Return empty config if file doesn't exist
		return &ServersConfig{
			Version: 1,
			Servers: []*ClientServerInfo{},
			DefaultPreferences: &DefaultPreferences{
				Username:             "",
				Email:                "",
				AutoConnectOnStartup: false,
			},
		}, nil
	}

	// Read file
	data, err := os.ReadFile(cm.serversFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read servers config: %w", err)
	}

	// Parse JSON
	var config ServersConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse servers config: %w", err)
	}

	return &config, nil
}

// SaveServers saves the server list to ~/.concord/servers.json
func (cm *ConfigManager) SaveServers(config *ServersConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal servers config: %w", err)
	}

	// Write to temp file first (atomic write)
	tempFile := cm.serversFilePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write servers config: %w", err)
	}

	// Rename temp file to actual file (atomic operation)
	if err := os.Rename(tempFile, cm.serversFilePath); err != nil {
		os.Remove(tempFile) // Clean up temp file on error
		return fmt.Errorf("failed to save servers config: %w", err)
	}

	return nil
}

// AddServer adds a new server to the configuration
func (cm *ConfigManager) AddServer(info *ClientServerInfo) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Check for duplicate address:port
	for _, existing := range config.Servers {
		if existing.Address == info.Address && existing.Port == info.Port {
			return fmt.Errorf("server %s:%d already exists", info.Address, info.Port)
		}
	}

	// Set added timestamp
	info.AddedAt = time.Now()

	// Add to list
	config.Servers = append(config.Servers, info)

	// Save
	return cm.SaveServers(config)
}

// UpdateServerCredentials updates saved credentials for a server
func (cm *ConfigManager) UpdateServerCredentials(serverID uuid.UUID, creds *SavedCredentials) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Find server
	for _, server := range config.Servers {
		if server.ID == serverID {
			server.SavedCredentials = creds
			return cm.SaveServers(config)
		}
	}

	return fmt.Errorf("server %s not found", serverID)
}

// UpdateServerLastConnected updates the last connected timestamp for a server
func (cm *ConfigManager) UpdateServerLastConnected(serverID uuid.UUID) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Find server
	now := time.Now()
	for _, server := range config.Servers {
		if server.ID == serverID {
			server.LastConnected = &now
			return cm.SaveServers(config)
		}
	}

	return fmt.Errorf("server %s not found", serverID)
}

// UpdateServer updates an existing server's connection details
func (cm *ConfigManager) UpdateServer(info *ClientServerInfo) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	for i, server := range config.Servers {
		if server.ID == info.ID {
			config.Servers[i] = info
			return cm.SaveServers(config)
		}
	}

	return fmt.Errorf("server %s not found", info.ID)
}

// RemoveServer removes a server from the configuration
func (cm *ConfigManager) RemoveServer(serverID uuid.UUID) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	// Find and remove server
	for i, server := range config.Servers {
		if server.ID == serverID {
			config.Servers = append(config.Servers[:i], config.Servers[i+1:]...)
			return cm.SaveServers(config)
		}
	}

	return fmt.Errorf("server %s not found", serverID)
}

// LoadAppConfig loads UI preferences from ~/.concord/config.json
func (cm *ConfigManager) LoadAppConfig() (*AppConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	// Check if file exists
	if _, err := os.Stat(cm.configFilePath); os.IsNotExist(err) {
		// Return default config if file doesn't exist
		return &AppConfig{
			Version: 1,
			UI: UIConfig{
				Theme:               "dracula",
				ShowMembersList:     true,
				CollapsedCategories: make(map[string]map[string]bool),
			},
		}, nil
	}

	// Read file
	data, err := os.ReadFile(cm.configFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read app config: %w", err)
	}

	// Parse JSON
	var config AppConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse app config: %w", err)
	}

	// Ensure CollapsedCategories map is initialized
	if config.UI.CollapsedCategories == nil {
		config.UI.CollapsedCategories = make(map[string]map[string]bool)
	}

	return &config, nil
}

// SaveAppConfig saves UI preferences to ~/.concord/config.json
func (cm *ConfigManager) SaveAppConfig(config *AppConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Marshal to JSON with indentation
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal app config: %w", err)
	}

	// Write to temp file first (atomic write)
	tempFile := cm.configFilePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write app config: %w", err)
	}

	// Rename temp file to actual file (atomic operation)
	if err := os.Rename(tempFile, cm.configFilePath); err != nil {
		os.Remove(tempFile) // Clean up temp file on error
		return fmt.Errorf("failed to save app config: %w", err)
	}

	return nil
}

// UpdateDefaultPreferences updates the default user preferences
func (cm *ConfigManager) UpdateDefaultPreferences(prefs *DefaultPreferences) error {
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}

	config.DefaultPreferences = prefs
	return cm.SaveServers(config)
}

// GetClientServers returns the list of configured servers
func (cm *ConfigManager) GetClientServers() []*ClientServerInfo {
	config, err := cm.LoadServers()
	if err != nil {
		return []*ClientServerInfo{}
	}
	return config.Servers
}

// SaveIdentity saves the user's local identity to config.json
func (cm *ConfigManager) SaveIdentity(identity *LocalIdentity) error {
	config, err := cm.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load app config: %w", err)
	}
	config.Identity = identity
	return cm.SaveAppConfig(config)
}

// GetIdentity returns the stored local identity, or nil if not configured
func (cm *ConfigManager) GetIdentity() *LocalIdentity {
	config, err := cm.LoadAppConfig()
	if err != nil {
		return nil
	}
	return config.Identity
}

// SaveServerToken saves an auth token and userID for a server after successful auto-connect
func (cm *ConfigManager) SaveServerToken(serverID uuid.UUID, email, token string, userID uuid.UUID) error {
	creds := &SavedCredentials{
		Email:               email,
		Token:               token,
		AutoConnect:         true,
		RememberCredentials: true,
	}
	config, err := cm.LoadServers()
	if err != nil {
		return fmt.Errorf("failed to load servers: %w", err)
	}
	for _, server := range config.Servers {
		if server.ID == serverID {
			server.SavedCredentials = creds
			server.UserID = userID
			return cm.SaveServers(config)
		}
	}
	return fmt.Errorf("server %s not found", serverID)
}
