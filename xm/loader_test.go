package xm

import (
	"bytes"
	"encoding/binary"
	"io"
	"log"
	"testing"
)

func TestMultiSampleInstrument(t *testing.T) {
	// One instrument header, then two sample headers, then both samples' data.
	data := make([]byte, 263+2*40)
	binary.LittleEndian.PutUint16(data[27:], 2)
	binary.LittleEndian.PutUint32(data[29:], 40)
	data[33+48] = 1
	h := data[263:]
	binary.LittleEndian.PutUint32(h, 2)
	binary.LittleEndian.PutUint32(h[40:], 6)
	binary.LittleEndian.PutUint32(h[44:], 2)
	binary.LittleEndian.PutUint32(h[48:], 4)
	h[54] = 0x11
	data = append(data, 32, 32, 0, 64, 0, 224, 0, 224)
	instrument, err := readInstrument(log.New(io.Discard, "", 0), bytes.NewReader(data[4:]), 263)
	if err != nil {
		t.Fatal(err)
	}
	if instrument.Keymap[48] != 1 || len(instrument.Samples) != 2 {
		t.Fatal("incorrect keymap or sample count")
	}
	a, b := instrument.Samples[0], instrument.Samples[1]
	if len(a.Data) != 2 || a.Data[1] != .5 || len(b.Data) != 3 || b.Data[0] != .5 || b.Data[1] != .25 || b.Data[2] != 0 || b.LoopStart != 1 || b.LoopLength != 2 {
		t.Fatalf("bad samples: %+v %+v", a, b)
	}
}
