package client

import (
	"encoding/binary"
	"testing"
)

// wav builds a PCM WAV file.
func wav(channels, rate, bits int, samples []int) []byte {
	data := make([]byte, 0, len(samples)*bits/8)
	for _, s := range samples {
		if bits == 8 {
			data = append(data, byte(s+128))
		} else {
			data = binary.LittleEndian.AppendUint16(data, uint16(int16(s)))
		}
	}
	b := []byte("RIFF\x00\x00\x00\x00WAVEfmt ")
	b = binary.LittleEndian.AppendUint32(b, 16)
	b = binary.LittleEndian.AppendUint16(b, 1)
	b = binary.LittleEndian.AppendUint16(b, uint16(channels))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate))
	b = binary.LittleEndian.AppendUint32(b, uint32(rate*channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(channels*bits/8))
	b = binary.LittleEndian.AppendUint16(b, uint16(bits))
	b = append(b, "data"...)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

func TestDecodeWAV(t *testing.T) {
	// Mono 24 kHz becomes stereo 48 kHz: twice the frames, both channels.
	pcm, err := decodeWAV(wav(1, 24000, 16, []int{1000, 1000, 1000, 1000}))
	if err != nil || len(pcm) != 4*2*2 || pcm[0] != 1000 || pcm[1] != 1000 {
		t.Fatalf("mono 24k: %v %v", pcm, err)
	}
	// 8-bit stereo at 48 kHz passes through.
	pcm, err = decodeWAV(wav(2, 48000, 8, []int{64, -64}))
	if err != nil || len(pcm) != 2 || pcm[0] != 64<<8 || pcm[1] != -64<<8 {
		t.Fatalf("8-bit stereo: %v %v", pcm, err)
	}
	for _, bad := range [][]byte{[]byte("nope"), wav(6, 48000, 16, []int{0}), wav(1, 48000, 24, []int{0})} {
		if _, err := decodeWAV(bad); err == nil {
			t.Fatal("decoded an unsupported file")
		}
	}
	if _, err := decodeWAV(wav(1, 8000, 8, make([]int, 8000*31))); err == nil {
		t.Fatal("accepted a sound over 30 seconds")
	}
}

func TestMixIntoClips(t *testing.T) {
	out := []int16{30000, -30000, 5}
	mixInto(out, []int16{10000, -10000, 5}, 1)
	if out[0] != 32767 || out[1] != -32768 || out[2] != 10 {
		t.Fatalf("mix %v", out)
	}
}

func TestOpusChannels(t *testing.T) {
	head := append([]byte("OggS....OpusHead\x01"), 2)
	if ch, err := opusChannels(head); err != nil || ch != 2 {
		t.Fatalf("%d %v", ch, err)
	}
	if _, err := opusChannels([]byte("RIFF")); err == nil {
		t.Fatal("not Opus")
	}
}
