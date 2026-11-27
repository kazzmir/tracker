package main

import (
	"image/color"
	"math"
	"math/bits"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

const (
	spectrumSize   = 2048
	spectrumBars   = 28
	spectrumWidth  = 392
	spectrumHeight = 64
)

type spectrumSource interface {
	GetChannelCount() int
	// GetSpectrumChannel returns the number of interleaved samples copied.
	GetSpectrumChannel(int, []float32) int
}

type spectrum struct {
	scratch     [2 * spectrumSize]float32
	left, right [spectrumSize]complex128
	window      [spectrumSize]float64
	windowSum   float64
	reverse     [spectrumSize]int
	roots       [spectrumSize / 2]complex128
	edges       [spectrumBars + 1]int
	levels      [spectrumBars]float64
}

func newSpectrum(rate int) *spectrum {
	view := &spectrum{}
	for index := range spectrumSize {
		view.window[index] = .5 - .5*math.Cos(2*math.Pi*float64(index)/(spectrumSize-1))
		view.windowSum += view.window[index]
		view.reverse[index] = int(bits.Reverse(uint(index)) >> (bits.UintSize - (bits.Len(spectrumSize) - 1)))
	}
	for index := range view.roots {
		sin, cos := math.Sincos(-2 * math.Pi * float64(index) / spectrumSize)
		view.roots[index] = complex(cos, sin)
	}
	binHz := float64(rate) / spectrumSize
	low, high := 45.0, min(16000.0, float64(rate)/2)
	for index := range view.edges {
		hz := low * math.Pow(high/low, float64(index)/spectrumBars)
		bin := max(1, int(math.Round(hz/binHz)))
		if index > 0 {
			bin = max(view.edges[index-1]+1, bin)
		}
		view.edges[index] = min(spectrumSize/2, bin)
	}
	return view
}

func (view *spectrum) transform(data *[spectrumSize]complex128) {
	// Samples are assembled directly in bit-reversed order for a radix-2 FFT.
	for span := 2; span <= spectrumSize; span *= 2 {
		half := span / 2
		for start := 0; start < spectrumSize; start += span {
			for offset := range half {
				even := data[start+offset]
				odd := data[start+offset+half] * view.roots[offset*(spectrumSize/span)]
				data[start+offset], data[start+offset+half] = even+odd, even-odd
			}
		}
	}
}

func (view *spectrum) update(source spectrumSource, gain, elapsed float64) {
	var targets [spectrumBars]float64
	if source != nil && gain > 0 {
		clear(view.left[:])
		clear(view.right[:])
		for channel := range source.GetChannelCount() {
			count := source.GetSpectrumChannel(channel, view.scratch[:]) / 2
			for sample := range count {
				frame := spectrumSize - count + sample
				index := view.reverse[frame]
				view.left[index] += complex(float64(view.scratch[2*sample])*view.window[frame], 0)
				view.right[index] += complex(float64(view.scratch[2*sample+1])*view.window[frame], 0)
			}
		}
		view.transform(&view.left)
		view.transform(&view.right)
		for band := range targets {
			power := 0.0
			for bin := view.edges[band]; bin < view.edges[band+1]; bin++ {
				left, right := view.left[bin], view.right[bin]
				// Combine stereo power, not samples: surround would otherwise cancel.
				power = max(power, (real(left)*real(left)+imag(left)*imag(left)+real(right)*real(right)+imag(right)*imag(right))/2)
			}
			amplitude := 2 * gain * math.Sqrt(power) / view.windowSum
			const floorDB = -60
			targets[band] = max(0, min(1, (20*math.Log10(amplitude)-floorDB)/-floorDB))
		}
	}
	for band, target := range targets {
		// Fast attack and a short release keep the bars readable between beats.
		view.levels[band] = max(target, view.levels[band]-2.8*elapsed)
	}
}

func (view *spectrum) draw(image *ebiten.Image) {
	image.Fill(color.RGBA{16, 22, 28, 255})
	for _, y := range []float32{16, 32, 48} {
		vector.DrawFilledRect(image, 0, y, spectrumWidth, 1, color.RGBA{35, 47, 56, 255}, false)
	}
	const barWidth = spectrumWidth / spectrumBars
	for band, level := range view.levels {
		height := float32(math.Round(level * (spectrumHeight - 4)))
		if height > 0 {
			vector.DrawFilledRect(image, float32(band*barWidth+1), spectrumHeight-2-height, barWidth-2, height,
				color.RGBA{uint8(50 + level*120), 207, 186, 255}, false)
		}
	}
}
