package main

import (
	"math"
	"testing"
)

type spectrumTone []float32

func (tone spectrumTone) GetChannelCount() int                        { return 1 }
func (tone spectrumTone) GetSpectrumChannel(_ int, dst []float32) int { return copy(dst, tone) }

func TestSpectrumFindsToneAndReleases(t *testing.T) {
	view := newSpectrum(48000)
	for _, bin := range []int{4, 48, 384} {
		tone := make(spectrumTone, 2*spectrumSize)
		for frame := range spectrumSize {
			value := float32(.5 * math.Sin(2*math.Pi*float64(frame*bin)/spectrumSize))
			// Opposite stereo phases must still register at the right frequency.
			tone[frame*2], tone[frame*2+1] = value, -value
		}
		view.update(tone, 1, 1)
		strongest := 0
		for band, level := range view.levels {
			if level > view.levels[strongest] {
				strongest = band
			}
		}
		level := view.levels[strongest]
		if bin < view.edges[strongest] || bin >= view.edges[strongest+1] || math.Abs(level-(1+20*math.Log10(.5)/60)) > .001 {
			t.Fatalf("bin %d: strongest band %d [%d, %d), level %.3f", bin, strongest, view.edges[strongest], view.edges[strongest+1], level)
		}
		view.update(tone, .5, .1)
		if got := view.levels[strongest]; got >= level || got == 0 {
			t.Fatalf("volume reduction did not lower the bar: %.3f -> %.3f", level, got)
		}
		view.update(nil, 0, 1)
		for band, level := range view.levels {
			if level != 0 {
				t.Fatalf("band %d remains at %.3f after playback pauses", band, level)
			}
		}
	}
}
