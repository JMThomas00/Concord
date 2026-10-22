package table

import (
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/JMThomas00/Concord/sdk/arcade"
)

// speaker plays a standalone game's sounds on this computer, as Concord's
// client does in a channel: the files from the game's client/ folder
// (beside the program, as a release unpacks it, or in the working folder
// under go run), and the arcade kit's sounds made here when that folder
// isn't there. WAV only: that's what the kit and the games ship.
type speaker struct {
	dirs  []string // client/ folders to look in
	cache string   // where a made sound is written, for players that need a file
	out   func(file string, data []byte) error

	mu    sync.Mutex
	files map[string]soundFile // by asset; a zero soundFile is a sound not found
}

type soundFile struct {
	path string
	data []byte
}

func newSpeaker(dir string) *speaker {
	var dirs []string
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Join(filepath.Dir(exe), "client"))
	}
	dirs = append(dirs, "client")
	return &speaker{dirs: dirs, cache: filepath.Join(dir, "sounds"), out: playSound, files: map[string]soundFile{}}
}

// play plays asset (a path in client/, like "sounds/move.wav") without
// waiting for it to finish. A sound that can't be found is skipped.
func (s *speaker) play(asset string) {
	if f := s.load(asset); f.data != nil {
		_ = s.out(f.path, f.data)
	}
}

func (s *speaker) load(asset string) soundFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, ok := s.files[asset]
	if !ok {
		f = s.find(asset)
		s.files[asset] = f
	}
	return f
}

func (s *speaker) find(asset string) soundFile {
	name := filepath.FromSlash(path.Clean(asset))
	if !strings.EqualFold(filepath.Ext(name), ".wav") || !filepath.IsLocal(name) {
		return soundFile{}
	}
	for _, d := range s.dirs {
		p := filepath.Join(d, name)
		if data, err := os.ReadFile(p); err == nil {
			return soundFile{p, data}
		}
	}
	data, ok := arcade.SoundKit()[path.Clean(asset)]
	if !ok {
		return soundFile{}
	}
	if !soundNeedsFile {
		return soundFile{data: data}
	}
	p := filepath.Join(s.cache, name)
	if os.MkdirAll(filepath.Dir(p), 0o755) != nil || os.WriteFile(p, data, 0o644) != nil {
		return soundFile{}
	}
	return soundFile{p, data}
}
