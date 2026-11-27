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

func TestCompressedBlockRecoversFromInvalidWidth(test *testing.T) {
	// First block starts with delta 10, then switches to illegal width 256.
	// The next block must start at the next 32 KiB of decoded output.
	raw := []byte{3, 0, 10, 254, 3, 2, 0, 7, 0}
	out := make([]float32, 32769)
	remaining, err := decompress(raw, out, 8, false)
	if err != nil || len(remaining) != 0 {
		test.Fatalf("decode: %x, %v", remaining, err)
	}
	if out[0] != 10.0/128 || out[1] != 0 || out[32767] != 0 || out[32768] != 7.0/128 {
		test.Fatal("invalid width discarded valid samples or displaced the next block")
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

func TestLegacyInstrumentAndMixVolume(test *testing.T) {
	data := make([]byte, 197+554)
	copy(data, "IMPM")
	le.PutUint16(data[32:], 1)
	le.PutUint16(data[34:], 1)
	le.PutUint16(data[42:], 0x100)
	le.PutUint16(data[44:], 4)
	data[49], data[192] = 128, 255
	le.PutUint32(data[193:], 197)
	header := data[197:]
	copy(header, "IMPI")
	header[17], header[20], header[21] = 5, 1, 1
	le.PutUint16(header[24:], 10)
	header[26], header[27], header[64], header[65] = 3, 1, 48, 2
	copy(header[504:], []byte{0, 64, 12, 32, 255})
	module, err := Load(bytes.NewReader(data))
	if err != nil {
		test.Fatal(err)
	}
	inst := module.instruments[0]
	if module.mixVolume != 128 || inst.globalVolume != 128 || inst.pan != 128 || inst.fadeout != 20 || inst.nna != 3 || inst.dct != 1 || inst.notes[0] != 48 || inst.samples[0] != 2 {
		test.Fatalf("wrong legacy instrument or mix volume: %+v, %d", inst, module.mixVolume)
	}
	if inst.volume.count != 2 || inst.volume.points[1] != (point{12, 32}) || inst.volume.advance(12, false) != 12 || inst.panning.enabled() {
		test.Fatalf("wrong legacy envelope: %+v", inst.volume)
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
