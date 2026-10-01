//go:build novoice

package client

import "errors"

// Builds without voice have no audio output, so plugin sounds are silent.

const sfxAvailable = false

type sfxPlayer struct{}

func newSFXPlayer(string) *sfxPlayer { return &sfxPlayer{} }

func (p *sfxPlayer) Play([]int16, float64) error { return nil }

func decodeOggOpus([]byte) ([]int16, error) {
	return nil, errors.New("this build has no audio")
}
