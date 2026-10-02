//go:build cgo && !novoice

package rnnoise

import (
	"math"
	"math/rand"
	"testing"
)

func rms(x []float32) float64 {
	var s float64
	for _, v := range x {
		s += float64(v) * float64(v)
	}
	return math.Sqrt(s / float64(len(x)))
}

// Steady noise is removed; a speech-like signal (a voiced tone with
// harmonics, rising and falling like syllables) mostly survives and scores
// a higher speech probability.
func TestDenoise(t *testing.T) {
	d := New()
	defer d.Close()
	r := rand.New(rand.NewSource(1))

	var noiseIn, noiseOut float64
	var noiseVAD float32
	for f := 0; f < 300; f++ { // 3 s of fan-like noise
		frame := make([]float32, FrameSize)
		for i := range frame {
			frame[i] = float32(r.NormFloat64() * 1500)
		}
		noiseIn += rms(frame)
		noiseVAD = d.Process(frame)
		if f >= 100 { // after it has learned the noise
			noiseOut += rms(frame)
		}
	}
	noiseIn /= 300
	noiseOut /= 200
	if noiseOut > noiseIn/4 {
		t.Fatalf("noise only went from %.0f to %.0f RMS", noiseIn, noiseOut)
	}

	var speechVAD float32
	n := 0
	for f := 0; f < 200; f++ { // 2 s of a vowel-like tone over the same noise
		frame := make([]float32, FrameSize)
		env := 0.5 + 0.5*math.Sin(float64(f)*0.3) // syllable rhythm
		for i := range frame {
			s := float64(f*FrameSize + i)
			v := 0.0
			for h := 1; h <= 8; h++ { // 150 Hz voice with harmonics
				v += math.Sin(2*math.Pi*150*float64(h)*s/SampleRate) / float64(h)
			}
			frame[i] = float32(v*6000*env + r.NormFloat64()*1500)
		}
		p := d.Process(frame)
		if f >= 50 {
			speechVAD += p
			n++
		}
	}
	speechVAD /= float32(n)
	if speechVAD <= noiseVAD {
		t.Fatalf("speech probability %.2f isn't above noise's %.2f", speechVAD, noiseVAD)
	}
	t.Logf("noise %.0f → %.0f RMS; speech probability: noise %.2f, voice %.2f", noiseIn, noiseOut, noiseVAD, speechVAD)
}
