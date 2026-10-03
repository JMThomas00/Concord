package client

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ServersConfig represents the top-level configuration structure for ~/.concord/servers.json
type ServersConfig struct {
	Version            int                 `json:"version"`
	Servers            []*ClientServerInfo `json:"servers"`
	DefaultPreferences *DefaultPreferences `json:"default_preferences,omitempty"`
}

// DefaultPreferences stores default user preferences for new server registrations
type DefaultPreferences struct {
	Username             string `json:"username,omitempty"`
	Email                string `json:"email,omitempty"`
	AutoConnectOnStartup bool   `json:"auto_connect_on_startup"`
}

// LocalIdentity is one profile: who you are on every server you join.
// A computer can hold several (a shared or family computer); one is active.
type LocalIdentity struct {
	ID       string `json:"id,omitempty"` // stable key; set when first saved
	Alias    string `json:"alias"`
	Email    string `json:"email"`
	Password string `json:"password"` // plaintext in v0.1; will be encrypted in a future version
}

// AppConfig represents UI preferences stored in ~/.concord/config.json
type AppConfig struct {
	Version int      `json:"version"`
	UI      UIConfig `json:"ui"`
	// Identity is the active profile, kept for configs written before
	// profiles (and read by older clients). Identities holds them all.
	Identity       *LocalIdentity   `json:"identity,omitempty"`
	Identities     []*LocalIdentity `json:"identities,omitempty"`
	ActiveIdentity string           `json:"active_identity,omitempty"`
	TermsAccepted  bool             `json:"terms_accepted"` // Whether user has accepted Terms of Service
}

// profiles returns the config's profiles and the active one's ID, folding
// in a pre-profiles Identity.
func (c *AppConfig) profiles() ([]*LocalIdentity, string) {
	list := c.Identities
	active := c.ActiveIdentity
	if len(list) == 0 && c.Identity != nil {
		list = []*LocalIdentity{c.Identity}
	}
	for _, p := range list {
		if p.ID == "" {
			p.ID = uuid.NewString()
		}
	}
	if c.Identity != nil && active == "" {
		for _, p := range list {
			if strings.EqualFold(p.Email, c.Identity.Email) {
				active = p.ID
			}
		}
	}
	if active == "" && len(list) > 0 {
		active = list[0].ID
	}
	return list, active
}

// setProfiles stores the profiles and which one is active.
func (c *AppConfig) setProfiles(list []*LocalIdentity, active string) {
	c.Identities, c.ActiveIdentity, c.Identity = list, active, nil
	for _, p := range list {
		if p.ID == active {
			c.Identity = p
		}
	}
	if c.Identity == nil {
		c.ActiveIdentity = ""
	}
}

// UIConfig holds UI-related preferences
type UIConfig struct {
	Theme               string                     `json:"theme"`
	ShowMembersList     bool                       `json:"show_members_list"`
	CollapsedCategories map[string]map[string]bool `json:"collapsed_categories,omitempty"` // serverID -> categoryID -> collapsed
	MutedChannels       []string                   `json:"muted_channels,omitempty"`       // channel UUIDs
	MutedServers        []string                   `json:"muted_servers,omitempty"`        // client server UUIDs
	LastBannerIndex     int                        `json:"last_banner_index"`              // Index of last displayed banner
	Notifications       NotificationConfig         `json:"notifications"`
	Display             DisplayConfig              `json:"display"`
	Audio               AudioConfig                `json:"audio"`
	HubURLs             []string                   `json:"hub_urls,omitempty"` // Grapevine hub URLs; nil = use built-in default
}

// defaultAudioConfig fills in zero-value fields with sensible defaults.
func defaultAudioConfig(c AudioConfig) AudioConfig {
	if c.InputGain == 0 {
		c.InputGain = 1.0
	}
	if c.OutputVolume == 0 {
		c.OutputVolume = 1.0
	}
	if c.VADThreshold == 0 {
		c.VADThreshold = 0.08
	}
	if c.NoiseSuppressStrength == 0 {
		c.NoiseSuppressStrength = 0.5
	}
	if c.EchoCancellationStrength == 0 {
		c.EchoCancellationStrength = 0.5
	}
	if c.CodecPreset == "" {
		c.CodecPreset = "medium"
	}
	if c.PerUserVolumes == nil {
		c.PerUserVolumes = make(map[string]float64)
	}
	if c.PluginSoundVolume == 0 {
		c.PluginSoundVolume = 0.8
	}
	// Version 1 (2026-10-02): noise suppression became RNNoise. The old
	// gate's settings don't carry over, so it's switched on for everyone,
	// at a strength that removes most noise; people can turn it down.
	if c.ProcessingVersion < 1 {
		c.NoiseSuppress = true
		c.NoiseSuppressStrength = math.Max(c.NoiseSuppressStrength, 0.8)
		c.ProcessingVersion = 1
	}
	return c
}

// AudioConfig holds audio device and voice preferences
type AudioConfig struct {
	InputDevice              string             `json:"input_device"`               // "" = system default
	InputDeviceName          string             `json:"input_device_name"`          // friendly display name
	OutputDevice             string             `json:"output_device"`              // "" = system default
	OutputDeviceName         string             `json:"output_device_name"`         // friendly display name
	InputGain                float64            `json:"input_gain"`                 // 0.0–2.0, default 1.0
	OutputVolume             float64            `json:"output_volume"`              // 0.0–1.0, default 1.0
	VADEnabled               bool               `json:"vad_enabled"`                // Voice Activity Detection
	VADThreshold             float64            `json:"vad_threshold"`              // raw RMS gate, default 0.08 -- see vadThresholdMin/Max in audio_settings_view.go for the realistic range the UI exposes as "Sensitivity"
	NoiseSuppress            bool               `json:"noise_suppress"`             // Noise suppression (RNNoise, see VoiceEngine.sendFrame and internal/rnnoise)
	NoiseSuppressStrength    float64            `json:"noise_suppress_strength"`    // 0.0–1.0, default 0.8 -- the noise left is (1-strength)², see mixDenoised
	AutoLevelOff             bool               `json:"auto_level_off"`             // turn off automatic levelling (autoLevel, voice_dsp.go), on by default
	ProcessingVersion        int                `json:"processing_version"`         // which voice processing these settings were last migrated for
	EchoCancellation         bool               `json:"echo_cancellation"`          // Echo cancellation (adaptive NLMS filter, see VoiceEngine.aecFilt / voice_aec.go)
	EchoCancellationStrength float64            `json:"echo_cancellation_strength"` // 0.0–1.0, default 0.5 -- NLMS adaptation aggressiveness (see aecMuForStrength)
	CodecPreset              string             `json:"codec_preset"`               // "low" / "medium" / "high"
	PerUserVolumes           map[string]float64 `json:"per_user_volumes"`           // userID → 0.0–2.0

	// Plugin sounds (game moves, alerts): their own volume, and a mute.
	PluginSoundVolume float64 `json:"plugin_sound_volume"` // 0.0–1.0, default 0.8
	PluginSoundsMuted bool    `json:"plugin_sounds_muted"`
}

// NotificationConfig holds notification and sound alert preferences
type NotificationConfig struct {
	SoundsMuted   bool   `json:"sounds_muted"`    // Master mute for all notification sounds
	MentionsOnly  bool   `json:"mentions_only"`   // Only play sounds for @mention messages
	BellOnMention bool   `json:"bell_on_mention"` // Write terminal bell \a on every @mention
	MentionSound  string `json:"mention_sound"`   // Sound name for @mention alerts
	MessageSound  string `json:"message_sound"`   // Sound name for regular message alerts

	// Desktop (OS-native) popup notifications -- independent of the sound
	// settings above. Zero values ("") are deliberately the safe/off
	// defaults so upgrading an existing config.json never starts firing
	// surprise popups for users who never opted in.
	DesktopNotifyMode  string `json:"desktop_notify_mode"`  // "" / "off" (default), "mentions", "all"
	DesktopNotifyScope string `json:"desktop_notify_scope"` // "" / "all_servers" (default), "current_server"
}

// DisplayConfig holds display and appearance preferences
type DisplayConfig struct {
	TimestampFormat      string `json:"timestamp_format"`       // "12h" or "24h"; empty = "24h"
	TimestampStyle       string `json:"timestamp_style"`        // "absolute" or "relative"; empty = "absolute"
	MessageDensity       string `json:"message_density"`        // "compact", "normal", "spacious"; empty = "normal"
	ShowAvatars          bool   `json:"show_avatars"`           // show colored circle avatars in chat headers
	ShowDateSeps         bool   `json:"show_date_seps"`         // show date separator lines between days
	GroupingGapMins      int    `json:"grouping_gap_mins"`      // minutes before new header shown; 0 = default (5)
	ServerListCollapsed  bool   `json:"server_list_collapsed"`  // false = expanded (default), true = collapsed
	MembersListCollapsed bool   `json:"members_list_collapsed"` // false = expanded (default), true = collapsed

	// Members panel display options (false = show, true = hide — matches Go zero value = show by default)
	MembersHideVUMeter bool `json:"members_hide_vu_meter"` // hide the voice level bar row (superseded by VoiceLevelStyle; kept in step for older clients)
	// VoiceLevelStyle is how voice levels show in the members panel:
	// "bar", "slider", "wave", "ring" or "off" (voice_level.go). "" means
	// "bar", or "off" when MembersHideVUMeter is set.
	VoiceLevelStyle    string `json:"voice_level_style,omitempty"`
	MembersHideQuality bool   `json:"members_hide_quality"` // hide the connection quality bar

	// Animation options
	DisablePanelAnimations bool   `json:"disable_panel_animations"` // skip slide-in/out for settings and server panels
	TypingAnimation        string `json:"typing_animation"`         // "" = "braille"; see typingAnimNames for valid values

	// Surprise is how lively the login stage is (mood.go): "" (full),
	// "calm" or "off". MoodLock is a mood code to use on every launch
	// instead of a random one ("" = random).
	Surprise string `json:"surprise,omitempty"`
	MoodLock string `json:"mood_lock,omitempty"`
	// Disco is the login stage's disco party ("disco" typed on Settings >
	// About, until it's typed again).
	Disco bool `json:"disco,omitempty"`

	// Images says how plugin images are drawn: "" or "auto" (the best this
	// terminal supports), "kitty", "sixel", "iterm2", "blocks" or "off".
	Images string `json:"images,omitempty"`

	// PluginCode says whether plugins' client code may run: "" asks for
	// each plugin first (plugin_code_consent.go), "never" turns it off.
	PluginCode string `json:"plugin_code,omitempty"`
}

// ServerSoundOverride stores per-server sound settings, overriding global defaults.
type ServerSoundOverride struct {
	SoundsMuted  bool   `json:"sounds_muted"`  // Mute all sounds for this server
	MentionsOnly bool   `json:"mentions_only"` // Only mention sounds for this server
	MentionSound string `json:"mention_sound"` // "" means use global default
	MessageSound string `json:"message_sound"` // "" means use global default
}

// SharedFilesConfig represents ~/.concord/shared_files.json -- a local record
// of files this client has sent as peer-to-peer attachments, so it can keep
// serving them to downloaders after a restart (the server never stores the
// bytes, only this client remembers where the original file lives on disk).
type SharedFilesConfig struct {
	Version int                        `json:"version"`
	Files   map[string]SharedFileEntry `json:"files"` // key: attachment ID
}

// SharedFileEntry records where a shared attachment's source file lives locally.
type SharedFileEntry struct {
	LocalPath string `json:"local_path"`
	Filename  string `json:"filename"`
}

// ConfigManager handles loading and saving configuration files
type ConfigManager struct {
	serversFilePath     string
	configFilePath      string
	sharedFilesFilePath string
	mu                  sync.RWMutex
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
		serversFilePath:     filepath.Join(concordDir, "servers.json"),
		configFilePath:      filepath.Join(concordDir, "config.json"),
		sharedFilesFilePath: filepath.Join(concordDir, "shared_files.json"),
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

	// Ensure all servers have an Order field set (for backward compatibility)
	for i, srv := range config.Servers {
		if srv.Order == 0 && i > 0 {
			// Order not set, assign based on array position
			srv.Order = i
		}
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

// LoadSharedFiles loads the sent-attachment registry from ~/.concord/shared_files.json.
func (cm *ConfigManager) LoadSharedFiles() (*SharedFilesConfig, error) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()

	if _, err := os.Stat(cm.sharedFilesFilePath); os.IsNotExist(err) {
		return &SharedFilesConfig{Version: 1, Files: map[string]SharedFileEntry{}}, nil
	}

	data, err := os.ReadFile(cm.sharedFilesFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read shared files config: %w", err)
	}

	var config SharedFilesConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse shared files config: %w", err)
	}
	if config.Files == nil {
		config.Files = map[string]SharedFileEntry{}
	}

	return &config, nil
}

// SaveSharedFiles saves the sent-attachment registry to ~/.concord/shared_files.json.
func (cm *ConfigManager) SaveSharedFiles(config *SharedFilesConfig) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal shared files config: %w", err)
	}

	tempFile := cm.sharedFilesFilePath + ".tmp"
	if err := os.WriteFile(tempFile, data, 0644); err != nil {
		return fmt.Errorf("failed to write shared files config: %w", err)
	}

	if err := os.Rename(tempFile, cm.sharedFilesFilePath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to save shared files config: %w", err)
	}

	return nil
}

// RecordSharedFile registers a newly-shared attachment's local source path so
// this client can keep serving it to downloaders after a restart.
func (cm *ConfigManager) RecordSharedFile(attachmentID uuid.UUID, localPath, filename string) error {
	config, err := cm.LoadSharedFiles()
	if err != nil {
		return fmt.Errorf("failed to load shared files: %w", err)
	}
	config.Files[attachmentID.String()] = SharedFileEntry{LocalPath: localPath, Filename: filename}
	return cm.SaveSharedFiles(config)
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
				LastBannerIndex:     -1,
				ShowMembersList:     true,
				CollapsedCategories: make(map[string]map[string]bool),
				Display: DisplayConfig{
					TimestampFormat: "24h",
					TimestampStyle:  "absolute",
					MessageDensity:  "normal",
					ShowAvatars:     false,
					ShowDateSeps:    false,
					GroupingGapMins: 5,
				},
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

// profilesMu serializes read-modify-write of the profiles and of saved
// server sign-ins, which several connections can finish at once.
var profilesMu sync.Mutex

// SaveIdentity saves a profile (adding it, or replacing the one with the
// same ID) and makes it the active one.
func (cm *ConfigManager) SaveIdentity(identity *LocalIdentity) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	config, err := cm.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("failed to load app config: %w", err)
	}
	list, _ := config.profiles()
	if identity.ID == "" {
		identity.ID = uuid.NewString()
	}
	replaced := false
	for i, p := range list {
		if p.ID == identity.ID {
			list[i], replaced = identity, true
		}
	}
	if !replaced {
		list = append(list, identity)
	}
	config.setProfiles(list, identity.ID)
	return cm.SaveAppConfig(config)
}

// GetIdentity returns the active profile, or nil if there is none.
func (cm *ConfigManager) GetIdentity() *LocalIdentity {
	list, active := cm.Profiles()
	for _, p := range list {
		if p.ID == active {
			return p
		}
	}
	return nil
}

// Profiles returns every saved profile and the active one's ID. A config
// from before profiles is converted (and saved) the first time, so each
// profile's ID stays the same from then on.
func (cm *ConfigManager) Profiles() ([]*LocalIdentity, string) {
	config, err := cm.LoadAppConfig()
	if err != nil {
		return nil, ""
	}
	// profiles() fills in missing IDs, so decide whether to save first.
	convert := config.ActiveIdentity == "" || len(config.Identities) == 0 || needsIDs(config.Identities)
	list, active := config.profiles()
	if convert && len(list) > 0 {
		profilesMu.Lock()
		config.setProfiles(list, active)
		_ = cm.SaveAppConfig(config)
		profilesMu.Unlock()
	}
	return list, active
}

func needsIDs(list []*LocalIdentity) bool {
	for _, p := range list {
		if p.ID == "" {
			return true
		}
	}
	return false
}

// SetActiveProfile makes the profile with id the active one.
func (cm *ConfigManager) SetActiveProfile(id string) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	config, err := cm.LoadAppConfig()
	if err != nil {
		return err
	}
	list, _ := config.profiles()
	config.setProfiles(list, id)
	return cm.SaveAppConfig(config)
}

// ForgetProfile removes a profile from this computer, along with its saved
// server sign-ins. If it was active, the first remaining one (if any)
// becomes active; the new active ID is returned ("" when none are left).
func (cm *ConfigManager) ForgetProfile(id string) (string, error) {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	config, err := cm.LoadAppConfig()
	if err != nil {
		return "", err
	}
	list, active := config.profiles()
	var kept []*LocalIdentity
	var forgotten *LocalIdentity
	for _, p := range list {
		if p.ID == id {
			forgotten = p
		} else {
			kept = append(kept, p)
		}
	}
	if active == id {
		active = ""
		if len(kept) > 0 {
			active = kept[0].ID
		}
	}
	config.setProfiles(kept, active)
	if err := cm.SaveAppConfig(config); err != nil {
		return "", err
	}
	if servers, err := cm.LoadServers(); err == nil && forgotten != nil {
		changed := false
		for _, s := range servers.Servers {
			if s.SavedCredentials != nil && s.SavedCredentials.belongsTo(forgotten) {
				s.SavedCredentials, s.UserID, changed = nil, uuid.Nil, true
			}
		}
		if changed {
			_ = cm.SaveServers(servers)
		}
	}
	return active, nil
}

// SaveServerToken saves an auth token and userID for a server after
// successful auto-connect, for the profile that signed in.
func (cm *ConfigManager) SaveServerToken(serverID uuid.UUID, email, token string, userID uuid.UUID) error {
	return cm.SaveServerSignIn(serverID, "", email, token, userID)
}

// SaveServerSignIn records that profileID signed in to a server as email.
func (cm *ConfigManager) SaveServerSignIn(serverID uuid.UUID, profileID, email, token string, userID uuid.UUID) error {
	profilesMu.Lock()
	defer profilesMu.Unlock()
	creds := &SavedCredentials{
		ProfileID:           profileID,
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
