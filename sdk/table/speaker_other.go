//go:build !windows

package table

import (
	"errors"
	"os/exec"
	"runtime"
	"sync"
)

// Elsewhere a sound is played by the system's own player, from a file.
const soundNeedsFile = true

// player is the first sound player found: afplay on macOS; PipeWire's,
// PulseAudio's or ALSA's on Linux.
var player = sync.OnceValue(func() []string {
	cands := [][]string{{"pw-play"}, {"paplay"}, {"aplay", "-q"}}
	if runtime.GOOS == "darwin" {
		cands = [][]string{{"afplay"}}
	}
	for _, c := range cands {
		if p, err := exec.LookPath(c[0]); err == nil {
			return append([]string{p}, c[1:]...)
		}
	}
	return nil
})

func canPlaySounds() bool { return player() != nil }

// playSound starts file playing and returns.
func playSound(file string, _ []byte) error {
	p := player()
	if p == nil {
		return errors.New("no sound player found")
	}
	c := exec.Command(p[0], append(p[1:], file)...)
	if err := c.Start(); err != nil {
		return err
	}
	go c.Wait() //nolint:errcheck
	return nil
}
