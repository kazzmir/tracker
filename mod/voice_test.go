package mod

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
	"testing"
)

func TestArpeggioDoesNotChangeBasePeriod(t *testing.T) {
	player := MakePlayer(&ModFile{
		Channels: 1, SongLength: 1, Orders: []byte{0},
		Patterns: []Pattern{{Rows: []Row{{Notes: []Note{{}}}}}},
	}, 48000)
	channel := player.Channels[0]
	channel.applyNote(&Note{PeriodFrequency: 428, EffectNumber: EffectArpeggio, EffectParameter: 0x40})
	for tick, want := range []int{428, 339, 428} {
		channel.UpdateTick(tick)
		if got := channel.arpeggioPeriod(channel.CurrentFrequency * 4); got != want*4 {
			t.Fatalf("tick %d pitch = %g, want %d", tick, float64(got)/4, want)
		}
		if channel.CurrentFrequency != 428 {
			t.Fatalf("tick %d changed base period to %d", tick, channel.CurrentFrequency)
		}
	}
	channel.UpdateTick(1)
	channel.UpdateRow()
	if got := channel.arpeggioPeriod(channel.CurrentFrequency * 4); got != 428*4 {
		t.Fatalf("arpeggio persists after the effect ends: period %g", float64(got)/4)
	}
}

func TestNoteAndInstrumentAreIndependent(t *testing.T) {
	p := MakePlayer(&ModFile{Channels: 1, Samples: []Sample{{Volume: 32, Data: make([]float32, 16)}}}, 48000)
	ch := p.Channels[0]
	ch.applyNote(&Note{SampleNumber: 1, PeriodFrequency: 428, EffectNumber: EffectSetVolume, EffectParameter: 64})
	if ch.Volume != 1 {
		t.Fatalf("C40 should override the sample's default volume, got %v", ch.Volume)
	}
	ch.startPosition = 7.25
	ch.applyNote(&Note{SampleNumber: 1})
	if ch.Volume != .5 || ch.startPosition != 7.25 {
		t.Fatalf("instrument-only row: volume %v, position %v", ch.Volume, ch.startPosition)
	}
	ch.applyNote(&Note{PeriodFrequency: 320, EffectNumber: EffectTonePortamento, EffectParameter: 4})
	ch.UpdateTick(1)
	if ch.CurrentFrequency != 424 || ch.startPosition != 7.25 {
		t.Fatalf("tone portamento: period %d, position %v", ch.CurrentFrequency, ch.startPosition)
	}
	ch.applyNote(&Note{PeriodFrequency: 320, EffectNumber: EffectExtra, EffectParameter: 0x11})
	if ch.CurrentFrequency != 319 || ch.startPosition != 0 {
		t.Fatalf("note-only row with fine slide: period %d, position %v", ch.CurrentFrequency, ch.startPosition)
	}
}

func TestLiveAndOfflineUseSameStereoMix(t *testing.T) {
	file := &ModFile{
		Channels: 4, SongLength: 1, Orders: []byte{0},
		Patterns: []Pattern{{Rows: []Row{{Notes: []Note{{}, {SampleNumber: 1, PeriodFrequency: 428}}}}}},
		Samples:  []Sample{{Volume: 64, Data: []float32{1, 1, 1, 1}, LoopLength: 2}},
	}
	live := MakePlayer(file, 48000)
	live.Update(0.01)
	got := make([]byte, 480*8)
	if n, err := live.Channels[1].Read(got); n != len(got) || err != nil {
		t.Fatalf("channel read = %d, %v", n, err)
	}
	offline := make([]byte, len(got))
	if _, err := io.ReadFull(MakePlayer(file, 48000).RenderToPCM(), offline); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, offline) {
		t.Fatal("live and offline renders disagree")
	}
	last := got[len(got)-8:]
	l := math.Float32frombits(binary.LittleEndian.Uint32(last))
	r := math.Float32frombits(binary.LittleEndian.Uint32(last[4:]))
	if math.Abs(float64(l)-0.125) > 1e-6 || math.Abs(float64(r)-0.375) > 1e-6 {
		t.Fatalf("channel 1 stereo levels = %v, %v", l, r)
	}
}

func TestLoopKeepsFractionalPosition(t *testing.T) {
	p := MakePlayer(&ModFile{Channels: 1}, 48000)
	ch := p.Channels[0]
	ch.CurrentSample = &Sample{LoopStart: 1, LoopLength: 2, Data: []float32{0, 0, 1, 2, 3, 4, 99}}
	ch.CurrentFrequency = 240
	ch.startPosition = 5.75
	ch.render(1)
	want := 2 + 5.75 + float64(palSampleClock)/(48000*240) - 6
	if math.Abs(ch.startPosition-want) > 1e-12 {
		t.Fatalf("loop position %v, want %v", ch.startPosition, want)
	}
}
