package protocol

import (
	"time"

	"github.com/google/uuid"
)

// Server Settings › About (OpServerAdmin, 66).

// Server admin actions.
const (
	ServerAdminStatus  = "status"  // send SERVER_STATUS (Manage Server)
	ServerAdminBackup  = "backup"  // take a backup now (Administrator)
	ServerAdminRestart = "restart" // stop and start again (Administrator)
)

// ServerAdminRequest asks the server for its status or to act.
type ServerAdminRequest struct {
	ServerID uuid.UUID `json:"server_id"`
	Action   string    `json:"action"`
}

// ServerAdminResult says how an action went.
type ServerAdminResult struct {
	Action  string `json:"action"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// How a server was started, as far as it can tell (ServerStatusPayload.RunsAs).
const (
	RunsAsDocker  = "docker"  // in a container
	RunsAsSystemd = "systemd" // a systemd service
	RunsAsLaunchd = "launchd" // a macOS launchd job
	RunsAsWindows = "windows" // the installer's Windows startup task (the SYSTEM account)
	RunsAsManual  = "manual"  // started by hand, or by something it can't recognise
)

// ServerStatusPayload is the server's health and how it runs.
type ServerStatusPayload struct {
	StartedAt time.Time `json:"started_at"`
	Now       time.Time `json:"now"` // the server's clock, so uptime doesn't depend on the client's

	OS   string `json:"os"`   // runtime.GOOS
	Arch string `json:"arch"` // runtime.GOARCH

	Members  int `json:"members"`
	Online   int `json:"online"`
	Channels int `json:"channels"`
	Messages int `json:"messages"`

	DatabaseBytes int64 `json:"database_bytes"` // the database plus its write-ahead log

	PluginsInstalled int `json:"plugins_installed"`
	PluginsRunning   int `json:"plugins_running"`

	Grapevine ServerGrapevineStatus `json:"grapevine"`

	// Backups: where Back up now writes, and the newest one (zero if none).
	BackupsDir      string    `json:"backups_dir,omitempty"`
	BackupCount     int       `json:"backup_count"`
	LastBackupAt    time.Time `json:"last_backup_at,omitempty"`
	LastBackupBytes int64     `json:"last_backup_bytes,omitempty"`
	BackingUp       bool      `json:"backing_up,omitempty"`

	RunsAs     string `json:"runs_as"`
	CanRestart bool   `json:"can_restart"`
	// RestartNote says why Restart isn't offered, when it isn't.
	RestartNote string `json:"restart_note,omitempty"`

	// CanAct: the asker may back up and restart (an administrator).
	CanAct bool `json:"can_act"`
}

// ServerGrapevineStatus is the server's Grapevine listing as it last saw it.
type ServerGrapevineStatus struct {
	Enabled       bool      `json:"enabled"`
	HubURL        string    `json:"hub_url,omitempty"`
	Listed        bool      `json:"listed"` // registered with the hub
	LastHeartbeat time.Time `json:"last_heartbeat,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
}
