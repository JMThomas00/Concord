//go:build !novoice

package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"time"

	malgo "github.com/gen2brain/malgo"
	"github.com/hraban/opus"
)

// sfxPlayer plays plugin sounds on its own output device, separate from
// voice. The device opens on the first sound and closes after a few idle
// seconds, so Concord doesn't hold the speakers open for nothing.
type sfxPlayer struct {
	mu      sync.Mutex
	openMu  sync.Mutex // serializes open(); lock order is openMu -> mu, never the reverse
	ctx     *malgo.AllocatedContext
	dev     *malgo.Device
	playing []*sfxVoice
	idle    *time.Timer
	output  string // device name ("" = the system default)
}

type sfxVoice struct {
	pcm  []int16
	pos  int
	gain float64
}

const sfxIdleClose = 5 * time.Second

// sfxAvailable says whether this build can play sounds.
const sfxAvailable = true

func newSFXPlayer(outputDevice string) *sfxPlayer { return &sfxPlayer{output: outputDevice} }

// Play mixes a decoded sound (48 kHz stereo) into the output.
func (p *sfxPlayer) Play(pcm []int16, gain float64) error {
	if len(pcm) == 0 || gain <= 0 {
		return nil
	}
	p.mu.Lock()
	if len(p.playing) >= 16 { // drop the oldest rather than pile up
		p.playing = p.playing[1:]
	}
	p.playing = append(p.playing, &sfxVoice{pcm: pcm, gain: gain})
	if p.idle != nil {
		p.idle.Stop()
		p.idle = nil
	}
	needOpen := p.dev == nil
	p.mu.Unlock()
	if !needOpen {
		return nil
	}

	// The device's first Start() synchronously pulls an initial chunk of
	// audio through fill(), which needs p.mu itself -- so p.mu must not be
	// held here, the same reason closeIfIdle below drops it before Stop().
	// openMu only serializes concurrent opens; the voice above is already
	// queued, so there's nothing left for this goroutine to do if another
	// one wins the race and opens the device first.
	p.openMu.Lock()
	defer p.openMu.Unlock()
	p.mu.Lock()
	alreadyOpen := p.dev != nil
	p.mu.Unlock()
	if alreadyOpen {
		return nil
	}
	if err := p.open(); err != nil {
		// Nothing has played while the device was down, so drop the whole
		// queue -- otherwise every sound that failed here would burst out
		// together on the next successful open.
		p.mu.Lock()
		p.playing = nil
		p.mu.Unlock()
		return err
	}
	return nil
}

// open starts the output device. Called with openMu held and mu not held.
func (p *sfxPlayer) open() error {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, nil)
	if err != nil {
		return err
	}
	cfg := malgo.DefaultDeviceConfig(malgo.Playback)
	cfg.Playback.Format = malgo.FormatS16
	cfg.Playback.Channels = 2
	cfg.SampleRate = sfxRate
	if p.output != "" {
		if devs, err := ctx.Devices(malgo.Playback); err == nil {
			for _, d := range devs {
				if d.Name() == p.output {
					id := d.ID
					cfg.Playback.DeviceID = id.Pointer()
					break
				}
			}
		}
	}
	dev, err := malgo.InitDevice(ctx.Context, cfg, malgo.DeviceCallbacks{Data: p.fill})
	if err != nil {
		_ = ctx.Uninit()
		ctx.Free()
		return err
	}
	if err := dev.Start(); err != nil {
		dev.Uninit()
		_ = ctx.Uninit()
		ctx.Free()
		return err
	}
	p.mu.Lock()
	p.ctx, p.dev = ctx, dev
	p.mu.Unlock()
	return nil
}

// fill is the device callback: mix every playing sound into the buffer.
func (p *sfxPlayer) fill(out, _ []byte, frames uint32) {
	mix := make([]int16, int(frames)*2)
	p.mu.Lock()
	kept := p.playing[:0]
	for _, v := range p.playing {
		mixInto(mix, v.pcm[v.pos:], v.gain)
		v.pos += len(mix)
		if v.pos < len(v.pcm) {
			kept = append(kept, v)
		}
	}
	p.playing = kept
	if len(p.playing) == 0 && p.idle == nil && p.dev != nil {
		p.idle = time.AfterFunc(sfxIdleClose, p.closeIfIdle)
	}
	p.mu.Unlock()
	for i, s := range mix {
		binary.LittleEndian.PutUint16(out[i*2:], uint16(s))
	}
}

func (p *sfxPlayer) closeIfIdle() {
	p.mu.Lock()
	if len(p.playing) > 0 || p.dev == nil {
		p.mu.Unlock()
		return
	}
	dev, ctx := p.dev, p.ctx
	p.dev, p.ctx, p.idle = nil, nil, nil
	p.mu.Unlock()
	// Outside the lock: stopping waits for the callback, which takes it.
	_ = dev.Stop()
	dev.Uninit()
	_ = ctx.Uninit()
	ctx.Free()
}

// decodeOggOpus decodes an Ogg/Opus file to 48 kHz stereo.
func decodeOggOpus(data []byte) ([]int16, error) {
	channels, err := opusChannels(data)
	if err != nil {
		return nil, err
	}
	s, err := opus.NewStream(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer s.Close()
	var pcm []int16
	buf := make([]int16, 5760*channels)
	for {
		n, err := s.Read(buf)
		if errors.Is(err, io.EOF) || n == 0 {
			break
		}
		if err != nil {
			return nil, err
		}
		pcm = append(pcm, buf[:n*channels]...)
		if len(pcm) > maxSoundSeconds*sfxRate*channels {
			return nil, errors.New("sound longer than 30 seconds")
		}
	}
	return toStereo48k(pcm, channels, sfxRate), nil
}
