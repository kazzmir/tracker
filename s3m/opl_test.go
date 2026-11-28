package s3m

import (
	"bytes"
	"encoding/binary"
	"log"
	"strings"
	"testing"

	"github.com/kazzmir/tracker/common"
)

func TestLoadAndTriggerAdLibInstrument(t *testing.T) {
	// A one-channel module with a single melodic SCRI patch and an empty pattern.
	data := make([]byte, 112+80)
	data[28], data[29] = 0x1a, 16
	binary.LittleEndian.PutUint16(data[32:], 1)
	binary.LittleEndian.PutUint16(data[34:], 1)
	binary.LittleEndian.PutUint16(data[36:], 1)
	copy(data[44:], "SCRM")
	data[48], data[49], data[50], data[51] = 64, 6, 125, 48
	for index := 64; index < 96; index++ {
		data[index] = 255
	}
	data[64] = 16
	binary.LittleEndian.PutUint16(data[97:], 7)
	header := data[112:]
	header[0] = 2
	patch := OPLPatch{0x21, 1, 8, 0, 0xf2, 0xf2, 0x24, 0x24}
	copy(header[16:], patch[:])
	header[28] = 48
	binary.LittleEndian.PutUint32(header[32:], 8363)
	copy(header[76:], "SCRI")
	var output bytes.Buffer
	logger := common.WithLogger(log.New(&output, "", 0))
	module, err := Load(bytes.NewReader(data), logger)
	if err != nil {
		t.Fatal(err)
	}
	if output.Len() != 0 {
		t.Fatalf("loader logged without verbosity: %s", &output)
	}
	verbose := common.WithVerbosity(1)
	if _, err := Load(bytes.NewReader(data), logger, verbose); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Instruments: 1") {
		t.Fatalf("verbose loader omitted instrument count: %s", &output)
	}
	if len(module.ChannelMap) != 1 || module.ChannelPanning[0] != 128 {
		t.Fatalf("AdLib channel not loaded: %v, %v", module.ChannelMap, module.ChannelPanning)
	}
	instrument := module.Instruments[0]
	if instrument.OPL == nil || *instrument.OPL != patch || instrument.Volume != 48 || instrument.MiddleC != 8363 {
		t.Fatalf("wrong AdLib instrument: %+v", instrument)
	}
	output.Reset()
	player := MakePlayer(module, 48000, logger, verbose)
	if !strings.Contains(output.String(), "48000 Hz") {
		t.Fatalf("verbose player omitted initialization details: %s", &output)
	}
	track := &player.channels[0]
	track.beginRow(Note{Note: 0x40, ChangeNote: true, SampleNumber: 1, ChangeSample: true})
	track.updateTick(0)
	track.prepare()
	track.render(960)
	var audio [1920]float32
	count := track.buffer.Read(audio[:])
	peak := float32(0)
	for _, sample := range audio[:count] {
		peak = max(peak, sample)
	}
	if count != len(audio) || peak < .01 {
		t.Fatalf("AdLib note was not audible: %d samples, peak %v", count, peak)
	}
	track.beginRow(Note{Note: 254, ChangeNote: true})
	track.updateTick(0)
	track.prepare()
	if track.fm.keyOn {
		t.Fatal("note cut did not clear the OPL key")
	}
}
