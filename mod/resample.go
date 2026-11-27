package mod

import "math"

const (
	filterTaps   = 8
	filterPhases = 1024
)

// Windowed sinc interpolation preserves transients better than linear
// interpolation. Precompute the weights so the mixer only does multiply-adds.
var sampleFilter = func() [filterPhases][filterTaps]float32 {
	var table [filterPhases][filterTaps]float32
	sinc := func(x float64) float64 {
		if x == 0 {
			return 1
		}
		p := math.Pi * x
		return math.Sin(p) / p
	}
	for phase := range table {
		var sum float32
		for tap := range table[phase] {
			x := float64(tap-3) - float64(phase)/filterPhases
			w := float32(sinc(x) * sinc(x/4))
			table[phase][tap] = w
			sum += w
		}
		for tap := range table[phase] {
			table[phase][tap] /= sum
		}
	}
	return table
}()

func interpolate(data []float32, position float64, end, loopStart int, looping, looped bool) float32 {
	i := int(position)
	weights := &sampleFilter[int((position-float64(i))*filterPhases)]
	var value float32
	if i >= 3 && i+4 < end && (!looped || i-3 >= loopStart) {
		for tap, sample := range data[i-3 : i+5] {
			value += sample * weights[tap]
		}
		return value
	}
	for tap, weight := range weights {
		index := i - 3 + tap
		if looping && (index >= end || (looped && index < loopStart)) {
			length := end - loopStart
			index = loopStart + ((index-loopStart)%length+length)%length
		} else {
			index = max(0, min(index, end-1))
		}
		value += data[index] * weight
	}
	return value
}
