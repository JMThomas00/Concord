//go:build ignore

// gen draws the tic-tac-toe example's client files: the X and O pieces and
// its sounds. Run it from this folder: go run gen.go
package main

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const size = 128

func main() {
	piece("assets/x.png", color.NRGBA{0xFF, 0x55, 0x55, 255}, func(x, y float64) float64 {
		// distance to either diagonal stroke (round-ended segments)
		return math.Min(segment(x, y, .24, .24, .76, .76), segment(x, y, .76, .24, .24, .76))
	})
	piece("assets/o.png", color.NRGBA{0x8B, 0xE9, 0xFD, 255}, func(x, y float64) float64 {
		return math.Abs(math.Hypot(x-.5, y-.5) - .3)
	})
	sound("sounds/move.wav", func(t float64) float64 {
		// a soft wooden click: a quick low thump with a fast decay
		return .6 * math.Sin(2*math.Pi*220*t) * math.Exp(-t*60)
	}, .09)
	sound("sounds/win.wav", func(t float64) float64 {
		// an arpeggio up a major chord, each note fading out
		v := 0.0
		for i, f := range []float64{523.25, 659.25, 783.99, 1046.5} {
			s := t - float64(i)*.11
			if s >= 0 {
				v += .25 * math.Sin(2*math.Pi*f*s) * math.Exp(-s*6)
			}
		}
		return v
	}, .9)
}

// piece draws a stroke (dist returns the distance to it in cell units)
// with anti-aliased edges and a soft glow, on a transparent background.
func piece(path string, c color.NRGBA, dist func(x, y float64) float64) {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	const width, glow = .055, .09
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var a float64
			for sy := 0; sy < 4; sy++ { // 4×4 supersampling
				for sx := 0; sx < 4; sx++ {
					x := (float64(px) + (float64(sx)+.5)/4) / size
					y := (float64(py) + (float64(sy)+.5)/4) / size
					d := dist(x, y)
					switch {
					case d < width:
						a += 1
					case d < width+glow:
						a += .25 * math.Pow(1-(d-width)/glow, 2)
					}
				}
			}
			a /= 16
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{c.R, c.G, c.B, uint8(a * 255)})
			}
		}
	}
	write(path, func(f *os.File) error { return png.Encode(f, img) })
}

// sound writes a 16-bit mono 48 kHz WAV of wave over seconds.
func sound(path string, wave func(t float64) float64, seconds float64) {
	const rate = 48000
	n := int(seconds * rate)
	data := make([]byte, 0, n*2)
	for i := 0; i < n; i++ {
		v := wave(float64(i) / rate)
		data = binary.LittleEndian.AppendUint16(data, uint16(int16(math.Max(-1, math.Min(1, v))*32767)))
	}
	h := []byte("RIFF")
	h = binary.LittleEndian.AppendUint32(h, uint32(36+len(data)))
	h = append(h, "WAVEfmt "...)
	h = binary.LittleEndian.AppendUint32(h, 16)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint16(h, 1)
	h = binary.LittleEndian.AppendUint32(h, rate)
	h = binary.LittleEndian.AppendUint32(h, rate*2)
	h = binary.LittleEndian.AppendUint16(h, 2)
	h = binary.LittleEndian.AppendUint16(h, 16)
	h = append(h, "data"...)
	h = binary.LittleEndian.AppendUint32(h, uint32(len(data)))
	write(path, func(f *os.File) error { _, err := f.Write(append(h, data...)); return err })
}

func write(path string, fn func(*os.File) error) {
	_ = os.MkdirAll(dirOf(path), 0o755)
	f, err := os.Create(path)
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := fn(f); err != nil {
		panic(err)
	}
}

func dirOf(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// segment is the distance from (x, y) to the segment (ax, ay)-(bx, by).
func segment(x, y, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	t := math.Max(0, math.Min(1, ((x-ax)*dx+(y-ay)*dy)/(dx*dx+dy*dy)))
	return math.Hypot(x-(ax+t*dx), y-(ay+t*dy))
}
