package it

import (
	"encoding/binary"
	"fmt"
	"io"
)

type bitReader struct {
	data  []byte
	bits  uint32
	count int
}

func (r *bitReader) read(width int) (int, error) {
	for r.count < width {
		if len(r.data) == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		r.bits |= uint32(r.data[0]) << r.count
		r.data = r.data[1:]
		r.count += 8
	}
	value := int(r.bits & ((1 << width) - 1))
	r.bits >>= width
	r.count -= width
	return value, nil
}

// IT compresses each channel separately, with fresh delta accumulators every
// 32 KiB of decoded PCM. IT 2.15 uses an additional delta integration.
func decompress(data []byte, dst []float32, bits int, it215 bool) ([]byte, error) {
	maxWidth := bits + 1
	selectorBits := 3
	if bits == 16 {
		selectorBits = 4
	}
	for len(dst) > 0 {
		if len(data) < 2 {
			return nil, io.ErrUnexpectedEOF
		}
		size := int(binary.LittleEndian.Uint16(data))
		data = data[2:]
		if size == 0 || size > len(data) {
			return nil, fmt.Errorf("invalid IT compressed block size %d", size)
		}
		reader := bitReader{data: data[:size]}
		data = data[size:]
		count := min(len(dst), 32768/(bits/8))
		width := maxWidth
		var delta, value uint16
		for i := 0; i < count; {
			if width < 1 || width > maxWidth {
				return nil, fmt.Errorf("invalid IT sample bit width %d", width)
			}
			v, err := reader.read(width)
			if err != nil {
				return nil, err
			}
			top := 1 << (width - 1)
			// Small widths use a sentinel plus a selector. Medium widths reserve
			// a range around the sign bit. The widest width has a dedicated flag.
			change := -1
			switch {
			case width <= 6 && v == top:
				change, err = reader.read(selectorBits)
				if err != nil {
					return nil, err
				}
			case width > 6 && width < maxWidth:
				low := top - (1 << (selectorBits - 1))
				if v >= low && v < low+(1<<selectorBits) {
					change = v - low
				}
			case width == maxWidth && v&top != 0:
				width = (v &^ top) + 1
				continue
			}
			if change >= 0 {
				next := change + 1
				if next >= width {
					next++
				}
				width = next
				continue
			}
			if width < maxWidth && v&top != 0 {
				v -= top * 2
			}
			delta += uint16(v)
			value += delta
			pcm := delta
			if it215 {
				pcm = value
			}
			if bits == 8 {
				dst[i] = float32(int8(pcm)) / 128
			} else {
				dst[i] = float32(int16(pcm)) / 32768
			}
			i++
		}
		dst = dst[count:]
	}
	return data, nil
}
