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

func (reader *bitReader) read(width int) (int, error) {
	for reader.count < width {
		if len(reader.data) == 0 {
			return 0, io.ErrUnexpectedEOF
		}
		reader.bits |= uint32(reader.data[0]) << reader.count
		reader.data = reader.data[1:]
		reader.count += 8
	}
	value := int(reader.bits & ((1 << width) - 1))
	reader.bits >>= width
	reader.count -= width
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
		for index := 0; index < count; {
			if width < 1 || width > maxWidth {
				// Some modules have a damaged tail in an otherwise complete
				// block. Keep the decoded prefix and leave the rest silent.
				break
			}
			code, err := reader.read(width)
			if err != nil {
				return nil, err
			}
			top := 1 << (width - 1)
			// Small widths use a sentinel plus a selector. Medium widths reserve
			// a range around the sign bit. The widest width has a dedicated flag.
			change := -1
			switch {
			case width <= 6 && code == top:
				change, err = reader.read(selectorBits)
				if err != nil {
					return nil, err
				}
			case width > 6 && width < maxWidth:
				low := top - (1 << (selectorBits - 1))
				if code >= low && code < low+(1<<selectorBits) {
					change = code - low
				}
			case width == maxWidth && code&top != 0:
				width = (code &^ top) + 1
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
			if width < maxWidth && code&top != 0 {
				code -= top * 2
			}
			delta += uint16(code)
			value += delta
			pcm := delta
			if it215 {
				pcm = value
			}
			if bits == 8 {
				dst[index] = float32(int8(pcm)) / 128
			} else {
				dst[index] = float32(int16(pcm)) / 32768
			}
			index++
		}
		dst = dst[count:]
	}
	return data, nil
}
