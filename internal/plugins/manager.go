package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	charmlog "github.com/charmbracelet/log"
	"github.com/google/uuid"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
)

// Manager discovers installed plugins, provisions their service-account
// identities, and supervises their processes for the lifetime of the server.
type Manager struct {
	db       *database.DB
	registry *Registry
	wsURL    string
	log      *charmlog.Logger

	supervisors map[string]*Supervisor // by plugin id
}

// NewManager creates a Manager. wsURL is the Concord WebSocket endpoint
// (e.g. "ws://127.0.0.1:8080/ws") handed to each plugin process so it knows
// where to connect back.
func NewManager(db *database.DB, wsURL string, log *charmlog.Logger) *Manager {
	return &Manager{
		db:          db,
		wsURL:       wsURL,
		log:         log,
		supervisors: make(map[string]*Supervisor),
		registry:    &Registry{},
	}
}

// Registry returns the discovered plugin/channel-kind registry.
func (m *Manager) Registry() *Registry {
	return m.registry
}

// LoadAll discovers plugins under pluginsDir, provisions any new ones
// (service-account user + token), and starts a Supervisor for each enabled
// plugin. Malformed plugin folders are logged and skipped, not fatal.
func (m *Manager) LoadAll(pluginsDir string) error {
	reg, errs := Discover(pluginsDir)
	m.registry = reg

	for folder, err := range errs {
		m.log.Warn("skipping invalid plugin", "folder", folder, "error", err)
	}

	for _, manifest := range reg.All() {
		installed, err := m.ensureInstalledPlugin(manifest)
		if err != nil {
			m.log.Error("failed to provision plugin", "plugin", manifest.Plugin.ID, "error", err)
			continue
		}

		if !installed.record.Enabled {
			m.log.Info("plugin discovered but disabled, not starting", "plugin", manifest.Plugin.ID)
			continue
		}

		if err := m.start(manifest, installed); err != nil {
			m.log.Error("failed to start plugin", "plugin", manifest.Plugin.ID, "error", err)
		}
	}

	return nil
}

// provisionedPlugin bundles a DB record with the plaintext token issued this
// run (only ever held in memory, passed to the child process's environment).
type provisionedPlugin struct {
	record      *models.InstalledPlugin
	plainToken  string
}

// ensureInstalledPlugin loads the plugin's persisted record, creating its
// service-account user + token on first discovery.
func (m *Manager) ensureInstalledPlugin(manifest *Manifest) (*provisionedPlugin, error) {
	pluginID := manifest.Plugin.ID

	existing, err := m.db.GetInstalledPlugin(pluginID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up installed plugin: %w", err)
	}

	if existing != nil {
		if existing.Name != manifest.Plugin.Name || existing.Version != manifest.Plugin.Version {
			if err := m.db.UpdateInstalledPluginMeta(pluginID, manifest.Plugin.Name, manifest.Plugin.Version, manifest.Dir); err != nil {
				return nil, fmt.Errorf("failed to update plugin metadata: %w", err)
			}
			existing.Name = manifest.Plugin.Name
			existing.Version = manifest.Plugin.Version
		}
		// Rotate the token on every (re)start: the plaintext only ever lives
		// in the child process's environment for that process's lifetime and
		// is never persisted, so there's nothing to reuse across restarts —
		// issuing a fresh one is simplest and avoids storing plaintext anywhere.
		plain, hash, err := IssueToken()
		if err != nil {
			return nil, err
		}
		if err := m.rotateToken(pluginID, hash); err != nil {
			return nil, err
		}
		existing.AuthTokenHash = hash
		return &provisionedPlugin{record: existing, plainToken: plain}, nil
	}

	// First discovery: create the service-account user + issue a token.
	user := models.NewUser(manifest.Plugin.Name, "")
	user.IsServiceAccount = true
	user.Status = models.StatusOnline

	placeholder := make([]byte, 16)
	if _, err := rand.Read(placeholder); err != nil {
		return nil, err
	}
	if err := m.db.CreateUser(user, "!plugin-account:"+hex.EncodeToString(placeholder)); err != nil {
		return nil, fmt.Errorf("failed to create plugin service account: %w", err)
	}

	plain, hash, err := IssueToken()
	if err != nil {
		return nil, err
	}

	now := time.Now()
	record := &models.InstalledPlugin{
		ID:            pluginID,
		Name:          manifest.Plugin.Name,
		Version:       manifest.Plugin.Version,
		ManifestPath:  manifest.Dir,
		Enabled:       true,
		AuthTokenHash: hash,
		ServiceUserID: user.ID,
		Status:        models.PluginStatusStopped,
		DiscoveredAt:  now,
		UpdatedAt:     now,
	}
	if err := m.db.CreateInstalledPlugin(record); err != nil {
		return nil, fmt.Errorf("failed to persist installed plugin: %w", err)
	}

	m.log.Info("plugin provisioned", "plugin", pluginID, "service_user_id", user.ID)
	return &provisionedPlugin{record: record, plainToken: plain}, nil
}

// rotateToken persists a freshly-issued token hash for an existing plugin.
func (m *Manager) rotateToken(pluginID, hash string) error {
	return m.db.SetPluginAuthTokenHash(pluginID, hash)
}

// start builds the plugin's env and launches its Supervisor.
func (m *Manager) start(manifest *Manifest, installed *provisionedPlugin) error {
	pluginID := manifest.Plugin.ID

	env := map[string]string{
		"CONCORD_WS_URL":      m.wsURL,
		"CONCORD_PLUGIN_ID":   pluginID,
		"CONCORD_PLUGIN_TOKEN": installed.plainToken,
	}

	sup := NewSupervisor(pluginID, manifest, env, m.log, func(status, lastError string) {
		if err := m.db.SetPluginStatus(pluginID, status, lastError); err != nil {
			m.log.Error("failed to persist plugin status", "plugin", pluginID, "error", err)
		}
	})
	m.supervisors[pluginID] = sup
	return sup.Start()
}

// SetEnabled toggles a plugin's enabled flag and starts/stops its process
// live — no restart of Concord itself required, unlike initial discovery.
func (m *Manager) SetEnabled(pluginID string, enabled bool) error {
	if err := m.db.SetPluginEnabled(pluginID, enabled); err != nil {
		return err
	}

	if enabled {
		manifest, ok := m.registry.Manifest(pluginID)
		if !ok {
			return fmt.Errorf("plugin %q not found in registry", pluginID)
		}
		record, err := m.db.GetInstalledPlugin(pluginID)
		if err != nil {
			return err
		}
		plain, hash, err := IssueToken()
		if err != nil {
			return err
		}
		if err := m.rotateToken(pluginID, hash); err != nil {
			return err
		}
		record.AuthTokenHash = hash
		return m.start(manifest, &provisionedPlugin{record: record, plainToken: plain})
	}

	if sup, ok := m.supervisors[pluginID]; ok {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		delete(m.supervisors, pluginID)
		return sup.Stop(ctx)
	}
	return nil
}

// MarkRunning promotes a plugin's status from "spawned" to "running" once its
// process has successfully identified over its own WebSocket connection —
// the only point at which Concord actually knows the plugin is healthy, as
// opposed to merely having launched an OS process.
func (m *Manager) MarkRunning(pluginID string) error {
	return m.db.SetPluginStatus(pluginID, models.PluginStatusRunning, "")
}

// AuthenticateToken resolves the plugin + service-account user a plugin
// process is identifying as, from the plaintext token it presented.
func (m *Manager) AuthenticateToken(token string) (*models.InstalledPlugin, error) {
	hash := HashToken(token)
	installed, err := m.db.GetInstalledPluginByTokenHash(hash)
	if err != nil {
		return nil, err
	}
	if installed == nil {
		return nil, fmt.Errorf("invalid plugin token")
	}
	return installed, nil
}

// ServiceUserID exposes a plugin's service-account user id, for routing
// relayed pane/event traffic to the right process.
func (m *Manager) ServiceUserIDFor(pluginID string) (uuid.UUID, error) {
	installed, err := m.db.GetInstalledPlugin(pluginID)
	if err != nil {
		return uuid.Nil, err
	}
	if installed == nil {
		return uuid.Nil, fmt.Errorf("plugin %q not installed", pluginID)
	}
	return installed.ServiceUserID, nil
}

// Shutdown stops every supervised plugin process gracefully.
func (m *Manager) Shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for id, sup := range m.supervisors {
		if err := sup.Stop(ctx); err != nil {
			m.log.Warn("plugin did not stop cleanly", "plugin", id, "error", err)
		}
	}
}
