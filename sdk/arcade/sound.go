package arcade

import (
	"bytes"
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
)

// The sound kit: short chiptune effects made from square, triangle and
// sawtooth waves, like an 8-bit sound chip. They're generated here, so
// every game has the same sounds and nothing is borrowed. Write them into
// a plugin's client/ folder with WriteSoundKit and play them with
// plugin.Conn.PlaySound(channel, viewer, arcade.SoundCoin, 0).
const (
	SoundBlip    = "sounds/blip.wav"    // the menu cursor moves
	SoundSelect  = "sounds/select.wav"  // a choice is made
	SoundBack    = "sounds/back.wav"    // back one screen
	SoundCoin    = "sounds/coin.wav"    // PRESS ENTER
	SoundAmber   = "sounds/amber.wav"   // a countdown light
	SoundGo      = "sounds/go.wav"      // green: go
	SoundRed     = "sounds/red.wav"     // a foul or a false start
	SoundMistake = "sounds/mistake.wav" // a mistake
	SoundFinish  = "sounds/finish.wav"  // the finish fanfare
	SoundRecord  = "sounds/record.wav"  // a new record, an unlock
)

type wave int

const (
	square wave = iota
	triangle
	sawtooth
)

// note is one tone: frequency (Hz), start and length (seconds), loudness
// (0-1) and an optional slide to another frequency.
type note struct {
	freq, at, dur, vol, slide float64
	w                         wave
}

const sampleRate = 22050

var kit = map[string][]note{
	SoundBlip:    {{freq: 880, dur: .045, vol: .5}},
	SoundSelect:  {{freq: 660, dur: .06, vol: .5}, {freq: 990, at: .06, dur: .09, vol: .5}},
	SoundBack:    {{freq: 660, dur: .06, vol: .5}, {freq: 440, at: .06, dur: .09, vol: .5}},
	SoundCoin:    {{freq: 988, dur: .08, vol: .5}, {freq: 1319, at: .08, dur: .38, vol: .5}},
	SoundAmber:   {{freq: 620, dur: .16, vol: .6}},
	SoundGo:      {{freq: 1240, dur: .5, vol: .5}, {freq: 620, dur: .5, vol: .5, w: triangle}},
	SoundRed:     {{freq: 110, dur: .45, vol: .45, w: sawtooth}, {freq: 116, dur: .45, vol: .3}},
	SoundMistake: {{freq: 320, dur: .12, vol: .45, slide: 90}},
	SoundFinish:  {{freq: 523, dur: .11, vol: .5}, {freq: 659, at: .11, dur: .11, vol: .5}, {freq: 784, at: .22, dur: .11, vol: .5}, {freq: 1047, at: .33, dur: .45, vol: .5}},
	SoundRecord: func() []note {
		var ns []note
		for i, f := range []float64{523, 587, 659, 698, 784, 880, 988, 1047} {
			ns = append(ns, note{freq: f, at: float64(i) * .05, dur: .06, vol: .45})
		}
		for i := 0; i < 6; i++ {
			f := 1568.0
			if i%2 == 1 {
				f = 1319
			}
			ns = append(ns, note{freq: f, at: .42 + float64(i)*.06, dur: .06, vol: .4})
		}
		return ns
	}(),
}

// SoundKit returns every sound in the kit as WAV files (16-bit mono PCM),
// keyed by their path in client/.
func SoundKit() map[string][]byte {
	out := map[string][]byte{}
	for name, notes := range kit {
		out[name] = wav(render(notes))
	}
	return out
}

// WriteSoundKit writes the kit into a plugin's client folder (e.g.
// "client"), creating client/sounds/. Run it from a go:generate tool, and
// commit the files so releases include them.
func WriteSoundKit(clientDir string) error {
	for name, data := range SoundKit() {
		path := filepath.Join(clientDir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func render(notes []note) []int16 {
	end := 0.0
	for _, n := range notes {
		end = max(end, n.at+n.dur)
	}
	buf := make([]float64, int((end+.03)*sampleRate))
	for _, n := range notes {
		start := int(n.at * sampleRate)
		length := int(n.dur * sampleRate)
		phase := 0.0
		for i := 0; i < length && start+i < len(buf); i++ {
			t := float64(i) / float64(length)
			f := n.freq
			if n.slide > 0 {
				f = n.freq * math.Pow(n.slide/n.freq, t)
			}
			phase += f / sampleRate
			p := phase - math.Floor(phase)
			var v float64
			switch n.w {
			case triangle:
				v = 4*math.Abs(p-.5) - 1
			case sawtooth:
				v = 2*p - 1
			default:
				v = 1
				if p >= .5 {
					v = -1
				}
			}
			// a quick attack, then an exponential fade, like the mockups
			env := math.Min(1, float64(i)/(.008*sampleRate)) * math.Exp(-4*t)
			buf[start+i] += v * env * n.vol
		}
	}
	out := make([]int16, len(buf))
	for i, v := range buf {
		v = math.Max(-1, math.Min(1, v*.5))
		out[i] = int16(v * 32000)
	}
	return out
}

func wav(samples []int16) []byte {
	var b bytes.Buffer
	data := len(samples) * 2
	w := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	b.WriteString("RIFF")
	w(uint32(36 + data))
	b.WriteString("WAVEfmt ")
	w(uint32(16))
	w(uint16(1)) // PCM
	w(uint16(1)) // mono
	w(uint32(sampleRate))
	w(uint32(sampleRate * 2))
	w(uint16(2))
	w(uint16(16))
	b.WriteString("data")
	w(uint32(data))
	w(samples)
	return b.Bytes()
}
