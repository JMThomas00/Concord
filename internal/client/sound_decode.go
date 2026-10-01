package client

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

// Plugin sounds are decoded to 48 kHz interleaved stereo int16, which is
// what the sound player mixes.

const sfxRate = 48000

// maxSoundSeconds caps how long a plugin sound can be.
const maxSoundSeconds = 30

// decodeWAV reads a PCM WAV file (8- or 16-bit, mono or stereo, any rate).
func decodeWAV(data []byte) ([]int16, error) {
	if len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var (
		channels, bits int
		rate           int
		pcm            []byte
	)
	for p := 12; p+8 <= len(data); {
		id := string(data[p : p+4])
		size := int(binary.LittleEndian.Uint32(data[p+4 : p+8]))
		body := data[p+8:]
		if size > len(body) {
			size = len(body)
		}
		switch id {
		case "fmt ":
			if size < 16 {
				return nil, errors.New("bad WAV format chunk")
			}
			if format := binary.LittleEndian.Uint16(body[0:2]); format != 1 && format != 0xFFFE {
				return nil, fmt.Errorf("WAV format %d isn't PCM", format)
			}
			channels = int(binary.LittleEndian.Uint16(body[2:4]))
			rate = int(binary.LittleEndian.Uint32(body[4:8]))
			bits = int(binary.LittleEndian.Uint16(body[14:16]))
		case "data":
			pcm = body[:size]
		}
		p += 8 + size + size%2
	}
	if channels < 1 || channels > 2 || rate < 4000 || rate > 192000 || (bits != 8 && bits != 16) || pcm == nil {
		return nil, fmt.Errorf("unsupported WAV (%d channels, %d Hz, %d-bit)", channels, rate, bits)
	}
	frames := len(pcm) / (channels * bits / 8)
	if frames > maxSoundSeconds*rate {
		return nil, fmt.Errorf("sound longer than %d seconds", maxSoundSeconds)
	}
	samples := make([]int16, frames*channels)
	for i := range samples {
		if bits == 8 {
			samples[i] = int16(int(pcm[i])-128) << 8
		} else {
			samples[i] = int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		}
	}
	return toStereo48k(samples, channels, rate), nil
}

// toStereo48k converts interleaved PCM to 48 kHz stereo, resampling
// linearly.
func toStereo48k(in []int16, channels, rate int) []int16 {
	frames := len(in) / channels
	if frames == 0 {
		return nil
	}
	outFrames := int(int64(frames) * sfxRate / int64(rate))
	out := make([]int16, outFrames*2)
	sample := func(f, c int) float64 {
		if f >= frames {
			f = frames - 1
		}
		if channels == 1 {
			c = 0
		}
		return float64(in[f*channels+c])
	}
	for i := 0; i < outFrames; i++ {
		pos := float64(i) * float64(rate) / sfxRate
		f := int(pos)
		t := pos - float64(f)
		for c := 0; c < 2; c++ {
			out[i*2+c] = int16(sample(f, c)*(1-t) + sample(f+1, c)*t)
		}
	}
	return out
}

// opusChannels reads the channel count from an Ogg/Opus file's OpusHead.
func opusChannels(data []byte) (int, error) {
	i := bytes.Index(data, []byte("OpusHead"))
	if i < 0 || i+10 > len(data) {
		return 0, errors.New("not an Ogg/Opus file")
	}
	ch := int(data[i+9])
	if ch < 1 || ch > 2 {
		return 0, fmt.Errorf("%d-channel Opus isn't supported", ch)
	}
	return ch, nil
}

// mixInto adds a sound's samples into out at the given gain, clipping.
func mixInto(out []int16, src []int16, gain float64) {
	for i := range out {
		if i >= len(src) {
			return
		}
		v := int(out[i]) + int(float64(src[i])*gain)
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
}
