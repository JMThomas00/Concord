package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// PluginInstance is one named copy of a plugin that allows several (see
// plugins.Manifest's Instances). ID is the instance's own plugin ID (its
// service account, settings, channels and data folder hang off it); BaseID
// is the installed plugin whose files it runs.
type PluginInstance struct {
	ID        string
	BaseID    string
	Name      string
	CreatedAt time.Time
}

// ListPluginInstances returns every instance, oldest first.
func (db *DB) ListPluginInstances() ([]PluginInstance, error) {
	rows, err := db.Query(`SELECT id, base_id, name, created_at FROM plugin_instances ORDER BY created_at, id`)
	if err != nil {
		return nil, fmt.Errorf("failed to list plugin instances: %w", err)
	}
	defer rows.Close()
	var out []PluginInstance
	for rows.Next() {
		var p PluginInstance
		if err := rows.Scan(&p.ID, &p.BaseID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetPluginInstance returns the instance with this ID, or ok=false.
func (db *DB) GetPluginInstance(id string) (PluginInstance, bool, error) {
	var p PluginInstance
	err := db.QueryRow(`SELECT id, base_id, name, created_at FROM plugin_instances WHERE id = ?`, id).
		Scan(&p.ID, &p.BaseID, &p.Name, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return p, false, nil
	}
	if err != nil {
		return p, false, fmt.Errorf("failed to load plugin instance %q: %w", id, err)
	}
	return p, true, nil
}

// SavePluginInstance creates or renames an instance.
func (db *DB) SavePluginInstance(id, baseID, name string) error {
	_, err := db.Exec(`
		INSERT INTO plugin_instances (id, base_id, name, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, base_id = excluded.base_id`,
		id, baseID, name, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("failed to save plugin instance %q: %w", id, err)
	}
	return nil
}

// DeletePluginInstance forgets an instance (its account, settings, channels
// and data folder are kept, like an uninstalled plugin's).
func (db *DB) DeletePluginInstance(id string) error {
	_, err := db.Exec(`DELETE FROM plugin_instances WHERE id = ?`, id)
	return err
}

// RenameServiceAccount renames a plugin's service account, the name members
// see on its messages and in mentions.
func (db *DB) RenameServiceAccount(userID uuid.UUID, name string) error {
	_, err := db.Exec(`UPDATE users SET username = ?, display_name = ? WHERE id = ? AND is_service_account = 1`,
		name, name, userID.String())
	if err != nil {
		return fmt.Errorf("failed to rename service account: %w", err)
	}
	return nil
}
