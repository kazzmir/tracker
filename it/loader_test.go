package it

import (
	"bytes"
	"errors"
	"io"
	"reflect"
	"testing"
)

func TestCompressedSamples(t *testing.T) {
	// One block at the default width. Encoded deltas are 10, -3, 1.
	for _, bits := range []int{8, 16} {
		width := bits + 1
		word := uint64(10) | uint64((1<<bits)-3)<<width | 1<<(2*width)
		n := (3*width + 7) / 8
		raw := []byte{byte(n), 0}
		for i := range n {
			raw = append(raw, byte(word>>(8*i)))
		}
		for _, test := range []struct {
			it215 bool
			want  []float32
		}{{false, []float32{10, 7, 8}}, {true, []float32{10, 17, 25}}} {
			out := make([]float32, 3)
			rest, err := decompress(append(append([]byte{}, raw...), 255), out, bits, test.it215)
			if err != nil || !bytes.Equal(rest, []byte{255}) {
				t.Fatalf("decode: %x, %v", rest, err)
			}
			for i := range out {
				out[i] *= float32(uint32(1) << uint(bits-1))
			}
			if !reflect.DeepEqual(out, test.want) {
				t.Fatalf("%d-bit, IT215=%v: got %v, want %v", bits, test.it215, out, test.want)
			}
		}
		if _, err := decompress(raw[:len(raw)-1], make([]float32, 3), bits, false); err == nil {
			t.Fatal("truncated block accepted")
		}
	}
}

func TestPatternMaskReuse(t *testing.T) {
	packed := []byte{0x81, 15, 0, 1, 32, 1, 6, 0, 0x81, 0xf0, 0, 1, 0}
	raw := append([]byte{0, byte(len(packed)), 0, 3, 0, 0, 0, 0, 0}, packed...)
	p, channels, err := readPattern(raw, 1)
	if err != nil || channels != 1 {
		t.Fatalf("pattern: %d, %v", channels, err)
	}
	want := Note{pitch: 0, instrument: 1, volume: 32, command: 1, param: 6, fields: 15}
	for i, row := range p.rows {
		if row[0] != want {
			t.Fatalf("row %d: %+v", i, row[0])
		}
	}
}

func TestInvalidModule(t *testing.T) {
	for _, tt := range []struct {
		input string
		err   error
	}{{"xxxx", ErrNotIT}, {"IMPM", io.ErrUnexpectedEOF}} {
		if _, err := Load(bytes.NewBufferString(tt.input)); !errors.Is(err, tt.err) {
			t.Fatalf("%q: %v", tt.input, err)
		}
	}
}
