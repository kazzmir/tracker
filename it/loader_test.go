package it

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestCompressedSamples(test *testing.T) {
	// One block at the default width. Encoded deltas are 10, -3, 1.
	for _, bits := range []int{8, 16} {
		width := bits + 1
		word := uint64(10) | uint64((1<<bits)-3)<<width | 1<<(2*width)
		size := (3*width + 7) / 8
		raw := []byte{byte(size), 0}
		for index := range size {
			raw = append(raw, byte(word>>(8*index)))
		}
		for _, variant := range []struct {
			it215 bool
			want  []float32
		}{{false, []float32{10, 7, 8}}, {true, []float32{10, 17, 25}}} {
			out := make([]float32, 3)
			rest, err := decompress(append(append([]byte{}, raw...), 255), out, bits, variant.it215)
			if err != nil || !bytes.Equal(rest, []byte{255}) {
				test.Fatalf("decode: %x, %v", rest, err)
			}
			for index := range out {
				out[index] *= float32(uint32(1) << uint(bits-1))
			}
			if !reflect.DeepEqual(out, variant.want) {
				test.Fatalf("%d-bit, IT215=%v: got %v, want %v", bits, variant.it215, out, variant.want)
			}
		}
		if _, err := decompress(raw[:len(raw)-1], make([]float32, 3), bits, false); err == nil {
			test.Fatal("truncated block accepted")
		}
	}
}

func TestPatternMaskReuse(test *testing.T) {
	packed := []byte{0x81, 15, 0, 1, 32, 1, 6, 0, 0x81, 0xf0, 0, 1, 0}
	raw := append([]byte{0, byte(len(packed)), 0, 3, 0, 0, 0, 0, 0}, packed...)
	pat, channels, err := readPattern(raw, 1)
	if err != nil || channels != 1 {
		test.Fatalf("pattern: %d, %v", channels, err)
	}
	want := Note{pitch: 0, instrument: 1, volume: 32, command: 1, param: 6, fields: 15}
	for index, row := range pat.rows {
		if row[0] != want {
			test.Fatalf("row %d: %+v", index, row[0])
		}
	}
}

func TestInvalidModule(test *testing.T) {
	for _, variant := range []struct {
		input string
		err   error
	}{{"xxxx", ErrNotIT}, {"IMPM", io.ErrUnexpectedEOF}} {
		if _, err := Load(bytes.NewBufferString(variant.input)); !errors.Is(err, variant.err) {
			test.Fatalf("%q: %v", variant.input, err)
		}
	}
}
