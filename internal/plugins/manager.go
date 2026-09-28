package plugins

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	charmlog "github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/google/uuid"
)

// Manager discovers installed plugins, provisions their service-account
// identities, and supervises their processes for the lifetime of the
// server -- including loading, unloading, restarting and updating individual
// plugins live, without restarting Concord.
//
// Lifecycle operations (Load, Unload, Restart, Update, Rescan, SetEnabled,
// Uninstall) are serialized by opMu, so two admins clicking at once can't
// interleave a stop with a start. mu guards the maps and the published
// Registry for the brief reads the request handlers make.
type Manager struct {
	db    *database.DB
	wsURL string
	log   *charmlog.Logger

	opMu sync.Mutex

	mu          sync.Mutex
	registry    *Registry
	pluginsDir  string
	dataRoot    string                   // <plugins_dir>/../PluginData, see start
	supervisors map[string]*Supervisor   // by plugin id
	identified  map[string]chan struct{} // closed by MarkRunning; see waitIdentified

	// onStopped runs after a plugin's process is stopped (the server
	// disconnects its service account, so nothing authenticated with the
	// old token lingers); onRegistryChanged runs after the set of plugins or
	// their channel kinds changed (the server pushes it to clients).
	onStopped         func(pluginID string)
	onRegistryChanged func()
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
		identified:  make(map[string]chan struct{}),
		registry:    newRegistry(),
	}
}

// SetHooks registers the server's callbacks; see Manager.onStopped and
// Manager.onRegistryChanged.
func (m *Manager) SetHooks(onStopped func(pluginID string), onRegistryChanged func()) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStopped, m.onRegistryChanged = onStopped, onRegistryChanged
}

// Registry returns the current plugin/channel-kind registry. The returned
// value is immutable; a later Load/Unload/Rescan publishes a new one.
func (m *Manager) Registry() *Registry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registry
}

// PluginsDir is the directory plugins are installed into.
func (m *Manager) PluginsDir() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pluginsDir
}

// LoadAll discovers plugins under pluginsDir, provisions any new ones
// (service-account user + token), and starts a Supervisor for each enabled
// plugin. Malformed plugin folders are logged and skipped, not fatal.
func (m *Manager) LoadAll(pluginsDir string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	if abs, err := filepath.Abs(pluginsDir); err == nil {
		pluginsDir = abs
	}
	reg, errs := Discover(pluginsDir)
	m.mu.Lock()
	m.pluginsDir = pluginsDir
	m.dataRoot = filepath.Join(filepath.Dir(pluginsDir), "PluginData")
	m.registry = reg
	m.mu.Unlock()

	for folder, err := range errs {
		m.log.Warn("skipping invalid plugin", "folder", folder, "error", err)
	}
	for _, manifest := range reg.All() {
		if err := m.startIfEnabled(manifest); err != nil {
			m.log.Error("failed to start plugin", "plugin", manifest.Plugin.ID, "error", err)
		}
	}
	return nil
}

// provisionedPlugin bundles a DB record with the plaintext token issued this
// run (only ever held in memory, passed to the child process's environment).
type provisionedPlugin struct {
	record     *models.InstalledPlugin
	plainToken string
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
	// email is a synthetic, unique-per-plugin placeholder — the users.email
	// column is UNIQUE, and SQLite only exempts NULL from that (not ""), so
	// a shared blank email collides as soon as a second plugin is installed.
	user := models.NewUser(manifest.Plugin.Name, "plugin+"+pluginID+"@service.concord.internal")
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

// ── Live lifecycle ──────────────────────────────────────────────────────────

// stopTimeout bounds how long stopping one plugin may take before its
// process is killed.
const stopTimeout = 10 * time.Second

// startIfEnabled provisions the plugin (rotating its token) and, if it's
// enabled, starts a fresh Supervisor. Caller holds opMu and has stopped any
// previous run.
func (m *Manager) startIfEnabled(manifest *Manifest) error {
	installed, err := m.ensureInstalledPlugin(manifest)
	if err != nil {
		return fmt.Errorf("failed to provision plugin: %w", err)
	}
	if !installed.record.Enabled {
		m.log.Info("plugin discovered but disabled, not starting", "plugin", manifest.Plugin.ID)
		return nil
	}
	return m.start(manifest, installed)
}

// start builds the plugin's env and launches its Supervisor.
func (m *Manager) start(manifest *Manifest, installed *provisionedPlugin) error {
	pluginID := manifest.Plugin.ID

	env := map[string]string{
		"CONCORD_WS_URL":       m.wsURL,
		"CONCORD_PLUGIN_ID":    pluginID,
		"CONCORD_PLUGIN_TOKEN": installed.plainToken,
	}
	m.mu.Lock()
	dataRoot := m.dataRoot
	m.mu.Unlock()
	if dataRoot != "" {
		dataDir := filepath.Join(dataRoot, pluginID)
		if err := os.MkdirAll(dataDir, 0o700); err != nil {
			m.log.Warn("could not create plugin data folder", "plugin", pluginID, "path", dataDir, "error", err)
		} else {
			env["CONCORD_PLUGIN_DATA_DIR"] = dataDir
		}
	}

	sup := NewSupervisor(pluginID, manifest, env, m.log, func(status, lastError string) {
		if err := m.db.SetPluginStatus(pluginID, status, lastError); err != nil {
			m.log.Error("failed to persist plugin status", "plugin", pluginID, "error", err)
		}
	})
	m.mu.Lock()
	m.supervisors[pluginID] = sup
	m.identified[pluginID] = make(chan struct{})
	m.mu.Unlock()
	return sup.Start()
}

// stop ends a plugin's current run, if any, waiting for its process to
// exit, then lets the server drop its connection. Caller holds opMu.
func (m *Manager) stop(pluginID string) {
	m.mu.Lock()
	sup := m.supervisors[pluginID]
	delete(m.supervisors, pluginID)
	delete(m.identified, pluginID)
	onStopped := m.onStopped
	m.mu.Unlock()
	if sup == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), stopTimeout)
	defer cancel()
	if err := sup.Stop(ctx); err != nil {
		m.log.Warn("plugin did not stop cleanly", "plugin", pluginID, "error", err)
	}
	if onStopped != nil {
		onStopped(pluginID)
	}
}

func (m *Manager) registryChanged() {
	m.mu.Lock()
	cb := m.onRegistryChanged
	m.mu.Unlock()
	if cb != nil {
		cb()
	}
}

func (m *Manager) setRegistry(r *Registry) {
	m.mu.Lock()
	m.registry = r
	m.mu.Unlock()
}

// Load (re)reads Plugins/<pluginID>/plugin.toml and starts the plugin if
// it's enabled -- for a freshly installed folder, or to pick up an edited
// manifest. A running copy is stopped first.
func (m *Manager) Load(pluginID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	manifest, err := LoadPluginFolder(filepath.Join(m.PluginsDir(), pluginID))
	if err != nil {
		return err
	}
	m.stop(pluginID)
	m.setRegistry(m.Registry().with(manifest))
	defer m.registryChanged()
	return m.startIfEnabled(manifest)
}

// Unload stops a plugin and forgets it until its folder is loaded again.
// Its database records, channels and data folder are kept.
func (m *Manager) Unload(pluginID string) {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.stop(pluginID)
	m.setRegistry(m.Registry().without(pluginID))
	m.registryChanged()
}

// Restart stops a plugin and starts it again with a fresh token -- also
// how an admin revives one that crashed past its restart limit.
func (m *Manager) Restart(pluginID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	manifest, ok := m.Registry().Manifest(pluginID)
	if !ok {
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}
	m.stop(pluginID)
	return m.startIfEnabled(manifest)
}

// SetEnabled toggles a plugin's enabled flag and starts/stops its process
// live. Enabling an already-running plugin restarts it cleanly rather than
// launching a second copy.
func (m *Manager) SetEnabled(pluginID string, enabled bool) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if err := m.db.SetPluginEnabled(pluginID, enabled); err != nil {
		return err
	}
	m.stop(pluginID)
	if !enabled {
		return nil
	}
	manifest, ok := m.Registry().Manifest(pluginID)
	if !ok {
		return fmt.Errorf("plugin %q not found in registry", pluginID)
	}
	return m.startIfEnabled(manifest)
}

// Uninstall stops a plugin and deletes its folder. Its data folder
// (PluginData/<id>), service account and channels are kept, so reinstalling
// picks up where it left off.
func (m *Manager) Uninstall(pluginID string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	if !validPluginIDPattern.MatchString(pluginID) {
		return fmt.Errorf("invalid plugin id %q", pluginID)
	}
	m.stop(pluginID)
	m.setRegistry(m.Registry().without(pluginID))
	defer m.registryChanged()
	if err := os.RemoveAll(filepath.Join(m.PluginsDir(), pluginID)); err != nil {
		return fmt.Errorf("failed to remove plugin folder: %w", err)
	}
	return m.db.SetPluginStatus(pluginID, models.PluginStatusStopped, "uninstalled")
}

// RescanResult reports what Rescan changed.
type RescanResult struct {
	Added, Removed, Reloaded []string
	Invalid                  map[string]error
}

// Rescan re-reads the plugins folder: new folders are loaded, missing ones
// unloaded, and plugins whose manifest version changed are restarted.
// Unchanged plugins keep running untouched.
func (m *Manager) Rescan() RescanResult {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	fresh, errs := Discover(m.PluginsDir())
	old := m.Registry()
	res := RescanResult{Invalid: errs}

	for _, manifest := range old.All() {
		if _, ok := fresh.Manifest(manifest.Plugin.ID); !ok {
			m.stop(manifest.Plugin.ID)
			res.Removed = append(res.Removed, manifest.Plugin.ID)
		}
	}
	m.setRegistry(fresh)
	for _, manifest := range fresh.All() {
		id := manifest.Plugin.ID
		prev, existed := old.Manifest(id)
		switch {
		case !existed:
			res.Added = append(res.Added, id)
		case prev.Plugin.Version != manifest.Plugin.Version:
			m.stop(id)
			res.Reloaded = append(res.Reloaded, id)
		default:
			continue
		}
		if err := m.startIfEnabled(manifest); err != nil {
			m.log.Error("failed to start plugin", "plugin", id, "error", err)
		}
	}
	for folder, err := range errs {
		m.log.Warn("skipping invalid plugin", "folder", folder, "error", err)
	}
	m.registryChanged()
	return res
}

// Update replaces an installed plugin with the version in stagedDir (a
// folder under Plugins/.staging, as FetchToStaging produces), keeping its
// data folder. If the new version doesn't identify within its
// startup_timeout_seconds (default 20s), the old version is put back and
// started again, and an error explains why.
func (m *Manager) Update(pluginID, stagedDir string) error {
	m.opMu.Lock()
	defer m.opMu.Unlock()

	newManifest, err := LoadManifest(filepath.Join(stagedDir, "plugin.toml"))
	if err != nil {
		return err
	}
	if newManifest.Plugin.ID != pluginID {
		return fmt.Errorf("staged plugin is %q, not %q", newManifest.Plugin.ID, pluginID)
	}
	pluginsDir := m.PluginsDir()
	live := filepath.Join(pluginsDir, pluginID)
	backup := filepath.Join(pluginsDir, ".backup", pluginID)
	if _, err := os.Stat(live); err != nil {
		return fmt.Errorf("plugin %q is not installed", pluginID)
	}

	m.stop(pluginID)
	if err := os.MkdirAll(filepath.Dir(backup), 0o755); err != nil {
		m.restartFrom(live, pluginID)
		return err
	}
	_ = os.RemoveAll(backup)
	if err := os.Rename(live, backup); err != nil {
		m.restartFrom(live, pluginID)
		return fmt.Errorf("failed to set the current version aside: %w", err)
	}
	if err := os.Rename(stagedDir, live); err != nil {
		_ = os.Rename(backup, live)
		m.restartFrom(live, pluginID)
		return fmt.Errorf("failed to move the new version into place: %w", err)
	}

	manifest, err := LoadPluginFolder(live)
	if err == nil {
		m.setRegistry(m.Registry().with(manifest))
		err = m.startIfEnabled(manifest)
		if err == nil {
			err = m.waitIdentified(pluginID, manifest)
		}
	}
	if err == nil {
		_ = os.RemoveAll(backup)
		m.registryChanged()
		return nil
	}

	// Roll back; the failed version goes to .backup/<id>.failed for a look.
	m.log.Error("plugin update failed, rolling back", "plugin", pluginID, "error", err)
	m.stop(pluginID)
	failed := backup + ".failed"
	_ = os.RemoveAll(failed)
	_ = os.Rename(live, failed)
	if rerr := os.Rename(backup, live); rerr != nil {
		return fmt.Errorf("update failed (%v) and restoring the previous version also failed: %w", err, rerr)
	}
	m.restartFrom(live, pluginID)
	return fmt.Errorf("update failed and was rolled back: %w", err)
}

// restartFrom reloads and starts the plugin in dir after a failed step.
func (m *Manager) restartFrom(dir, pluginID string) {
	manifest, err := LoadPluginFolder(dir)
	if err != nil {
		m.log.Error("could not reload plugin", "plugin", pluginID, "error", err)
		return
	}
	m.setRegistry(m.Registry().with(manifest))
	if err := m.startIfEnabled(manifest); err != nil {
		m.log.Error("could not restart plugin", "plugin", pluginID, "error", err)
	}
	m.registryChanged()
}

// waitIdentified blocks until the plugin's process connects and identifies
// (MarkRunning), or its startup timeout passes, or its supervisor gives up.
// A disabled plugin isn't waited for.
func (m *Manager) waitIdentified(pluginID string, manifest *Manifest) error {
	m.mu.Lock()
	ch, ok := m.identified[pluginID]
	sup := m.supervisors[pluginID]
	m.mu.Unlock()
	if !ok || sup == nil {
		return nil // disabled: nothing was started
	}
	timeout := time.Duration(manifest.Process.StartupTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	select {
	case <-ch:
		return nil
	case <-sup.Done():
		return fmt.Errorf("the new version kept crashing on startup")
	case <-time.After(timeout):
		return fmt.Errorf("the new version didn't connect within %v", timeout)
	}
}

// MarkRunning promotes a plugin's status from "spawned" to "running" once its
// process has successfully identified over its own WebSocket connection —
// the only point at which Concord actually knows the plugin is healthy, as
// opposed to merely having launched an OS process.
func (m *Manager) MarkRunning(pluginID string) error {
	m.mu.Lock()
	if ch, ok := m.identified[pluginID]; ok {
		select {
		case <-ch:
		default:
			close(ch)
		}
	}
	m.mu.Unlock()
	return m.db.SetPluginStatus(pluginID, models.PluginStatusRunning, "")
}

// Shutdown stops every supervised plugin process.
func (m *Manager) Shutdown() {
	m.opMu.Lock()
	defer m.opMu.Unlock()
	m.mu.Lock()
	ids := make([]string, 0, len(m.supervisors))
	for id := range m.supervisors {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, id := range ids {
		wg.Add(1)
		go func(id string) { defer wg.Done(); m.stop(id) }(id)
	}
	wg.Wait()
}
