package installer

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// The installer remembers what it installed where, in ~/.concord/install.json,
// so the client's Updates page (and the installer run again) can update,
// reconfigure or remove it without asking.

// Record is everything installed on this computer.
type Record struct {
	Components map[string]RecordEntry `json:"components"`
	UpdatedAt  time.Time              `json:"updated_at"`
}

// RecordEntry is one installed component.
type RecordEntry struct {
	Dir     string `json:"dir"`
	Start   string `json:"start,omitempty"` // server and hub: StartAtBoot, StartAtLogin or StartNever
	Version string `json:"version,omitempty"`
}

// RecordPath is where the record lives, for the person whose home it is.
func RecordPath(home string) string { return filepath.Join(ConfigDir(home), "install.json") }

// LoadRecord reads the record, or returns an empty one.
func LoadRecord(home string) *Record {
	r := &Record{Components: map[string]RecordEntry{}}
	if b, err := os.ReadFile(RecordPath(home)); err == nil {
		_ = json.Unmarshal(b, r)
	}
	if r.Components == nil {
		r.Components = map[string]RecordEntry{}
	}
	return r
}

// Save writes the record (or, with nothing in it, removes it).
func (r *Record) Save(home string) error {
	if len(r.Components) == 0 {
		err := os.Remove(RecordPath(home))
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	r.UpdatedAt = time.Now().UTC()
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(RecordPath(home)), 0o755); err != nil {
		return err
	}
	return os.WriteFile(RecordPath(home), b, 0o644)
}

// Installed lists the recorded components, in install order.
func (r *Record) Installed() []string {
	var out []string
	for _, c := range []string{Client, Server, Hub} {
		if _, ok := r.Components[c]; ok {
			out = append(out, c)
		}
	}
	return out
}

// ApplyRecord points the plan at what's already installed: the same parts,
// folders and start modes.
func (pl *Plan) ApplyRecord(r *Record) {
	pl.Components = r.Installed()
	for c, e := range r.Components {
		switch c {
		case Client:
			pl.ClientDir = e.Dir
		case Server:
			pl.ServerDir = e.Dir
			if e.Start != "" {
				pl.ServerStart = e.Start
			}
		case Hub:
			pl.HubDir = e.Dir
			if e.Start != "" {
				pl.HubStart = e.Start
			}
		}
	}
}

// recordInstall adds what was just installed to the record.
func (r *Runner) recordInstall() error {
	if r.Plan.DryRun {
		return nil
	}
	rec := LoadRecord(r.Plan.Platform.Home)
	for _, c := range r.Result.Installed {
		e := RecordEntry{Dir: r.Plan.Dir(c), Version: r.Result.Tag}
		if c != Client {
			e.Start = r.Plan.start(c)
		}
		rec.Components[c] = e
	}
	return rec.Save(r.Plan.Platform.Home)
}
