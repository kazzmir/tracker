package s3m

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestScreamTrackerSlidesAndNoteDelay(t *testing.T) {
	file := &S3MFile{FastVolumeSlides: true, InitialSpeed: 3, InitialTempo: 125, ChannelMap: map[int]int{0: 0}, Instruments: []Instrument{{Volume: 20, MiddleC: 8363, Data: []float32{.5, .25}}}}
	player := MakePlayer(file, 48000)
	track := &player.channels[0]
	track.beginRow(Note{ChangeNote: true, Note: 0x40, ChangeSample: true, SampleNumber: 1, ChangeEffect: true, EffectNumber: EffectSetExtra, EffectParameter: 0xd2})
	track.updateTick(0)
	track.updateTick(1)
	if track.voice != nil {
		t.Fatal("sample started before SD2")
	}
	track.updateTick(2)
	if track.voice == nil || track.period != 1712 || track.volume != 20 {
		t.Fatalf("delayed note has period %d and volume %d", track.period, track.volume)
	}
	track.beginRow(Note{ChangeEffect: true, EffectNumber: EffectVolumeSlide, EffectParameter: 0x1f})
	track.updateTick(0)
	track.updateTick(1)
	if track.volume != 21 {
		t.Fatalf("D1F applied on more than the first tick: %d", track.volume)
	}
	track.beginRow(Note{ChangeEffect: true, EffectNumber: EffectPortamentoDown, EffectParameter: 0})
	track.updateTick(0)
	track.updateTick(1)
	if track.period != 1712+0x1f*4 {
		t.Fatalf("E00 did not recall shared effect memory: %d", track.period)
	}
	track.beginRow(Note{ChangeEffect: true, EffectNumber: EffectVibratoAndVolumeSlide, EffectParameter: 2})
	track.updateTick(0)
	if track.volume != 19 {
		t.Fatalf("ST3.00 fast slide did not apply on tick zero: %d", track.volume)
	}
}

func TestImpulseTrackerExportPitchAndMemory(t *testing.T) {
	file := &S3MFile{impulseTracker: true, separateEffectMemory: true, ChannelMap: map[int]int{0: 0}, Instruments: []Instrument{{MiddleC: 8363, Data: []float32{1}}}}
	track := &MakePlayer(file, 48000).channels[0]
	track.beginRow(Note{ChangeNote: true, Note: 0x40, ChangeSample: true, SampleNumber: 1})
	track.updateTick(0)
	if track.period != 8363 {
		t.Fatalf("C-4 frequency = %d, want 8363 Hz", track.period)
	}
	for _, command := range []Note{
		{ChangeEffect: true, EffectNumber: EffectPortamentoDown, EffectParameter: 1},
		{ChangeEffect: true, EffectNumber: EffectVolumeSlide, EffectParameter: 0x20},
		{ChangeEffect: true, EffectNumber: EffectPortamentoUp},
	} {
		track.beginRow(command)
		track.updateTick(0)
		track.updateTick(1)
	}
	if track.period != 8362 {
		t.Fatalf("frequency after E01, D20, F00 = %d, want 8362 Hz", track.period)
	}
}

func TestTempoAndGlobalVolumeSlides(t *testing.T) {
	player := MakePlayer(&S3MFile{GlobalVolume: 40, InitialTempo: 125, ChannelMap: map[int]int{0: 0}}, 48000)
	track := &player.channels[0]
	for _, command := range []Note{
		{ChangeEffect: true, EffectNumber: EffectSetTempo, EffectParameter: 0x03},
		{ChangeEffect: true, EffectNumber: EffectSetTempo},
		{ChangeEffect: true, EffectNumber: EffectGlobalVolumeSlide, EffectParameter: 0x2f},
		{ChangeEffect: true, EffectNumber: EffectGlobalVolumeSlide, EffectParameter: 0x01},
		{ChangeEffect: true, EffectNumber: EffectGlobalVolumeSlide},
	} {
		track.beginRow(command)
		track.updateTick(0)
		track.updateTick(1)
		track.updateTick(2)
	}
	if player.BPM != 113 || player.GlobalVolume != 38 {
		t.Fatalf("tempo = %d, global volume = %d; want 113 and 38", player.BPM, player.GlobalVolume)
	}
}

func TestSampleOffsetReplacesPreviousOffsetAndWraps(t *testing.T) {
	file := &S3MFile{ChannelMap: map[int]int{0: 0}, Instruments: []Instrument{{Data: make([]float32, 1024), Loop: true, LoopBegin: 100, LoopEnd: 700}}}
	track := &MakePlayer(file, 48000).channels[0]
	for _, row := range []struct {
		note     Note
		position float64
	}{
		{Note{ChangeNote: true, Note: 0x40, ChangeSample: true, SampleNumber: 1, ChangeEffect: true, EffectNumber: EffectSampleOffset, EffectParameter: 1}, 256},
		{Note{ChangeNote: true, Note: 0x40, ChangeEffect: true, EffectNumber: EffectSampleOffset, EffectParameter: 2}, 512},
		{Note{ChangeNote: true, Note: 0x40}, 512},
		{Note{ChangeNote: true, Note: 0x40, ChangeEffect: true, EffectNumber: EffectSampleOffset, EffectParameter: 3}, 168},
	} {
		track.beginRow(row.note)
		track.updateTick(0)
		if track.position != row.position {
			t.Fatalf("sample position = %g, want %g", track.position, row.position)
		}
	}
}

func TestGUSPortamentoKeepsPlayingSample(t *testing.T) {
	file := &S3MFile{ChannelMap: map[int]int{0: 0}, screamTrackerGUS: true, Instruments: []Instrument{
		{Data: make([]float32, 32), MiddleC: 8363},
		{Data: make([]float32, 32), MiddleC: 16726},
	}}
	track := &MakePlayer(file, 48000).channels[0]
	track.beginRow(Note{ChangeNote: true, Note: 0x40, ChangeSample: true, SampleNumber: 1})
	track.updateTick(0)
	track.position = 12
	track.beginRow(Note{ChangeNote: true, Note: 0x40, ChangeSample: true, SampleNumber: 2, ChangeEffect: true, EffectNumber: EffectPortamentoToNote, EffectParameter: 1})
	track.updateTick(0)
	if track.voice != &file.Instruments[0] || track.position != 12 || track.target != 856 {
		t.Fatalf("GUS portamento changed playing sample or used wrong tuning: position %g, target %d", track.position, track.target)
	}
}

func TestScreamTrackerCollectionAgainstFFplay(t *testing.T) {
	for _, fixture := range []struct {
		name                  string
		frames                int
		correlation, maxError float64
	}{
		{"Spummie Toy", 10400256, .99, .15},
		{"Time For Tennis", 10293882, .99, .15},
		{"Private Territory", 14128687, .98, .20},
		{"Through The Void", 9569280, .995, .15},
		{"Fort Genezis", 12950386, .995, .15},
		{"Unicorn Magic", 9319104, .98, .20},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			compareWithFFplay(t, filepath.Join("..", "t1", "Scream Tracker 3", fixture.name+".s3m"), fixture.frames, fixture.correlation, fixture.maxError)
		})
	}
}

func TestAquaAgainstFFplay(t *testing.T) {
	path := "../aqua.s3m"
	if _, err := os.Stat(path); os.IsNotExist(err) {
		path = "../data/data/aqua.s3m"
	}
	compareWithFFplay(t, path, 7299840, .99, .12)
}

func TestM01GMuzAgainstFFplay(t *testing.T) {
	// The OPL emulators differ in phase and envelope shape; allow more
	// waveform error than for the all-PCM aqua.s3m.
	compareWithFFplay(t, "../m01gmuz.s3m", 2684800, .96, .27)
}

func compareWithFFplay(t *testing.T, path string, frames int, minCorrelation, maxError float64) {
	t.Helper()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		t.Skipf("local fixture %s not available", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	module, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "demuxer=libopenmpt").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Demuxer libopenmpt")) {
		t.Skip("FFmpeg has no libopenmpt demuxer")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "libopenmpt", "-sample_rate", "48000", "-i", path, "-f", "f32le", "-")
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cancel()
			_ = cmd.Wait()
		}
	}()
	ref := bufio.NewReader(stdout)
	const rate = 48000
	ours := MakePlayer(module, rate).RenderToPCM()
	var got, want [32768]byte
	var gotPower, wantPower, dot, errorPower [2]float64
	for remaining := frames * 8; remaining > 0; {
		size := min(remaining, len(got))
		remaining -= size
		if _, err := io.ReadFull(ours, got[:size]); err != nil {
			t.Fatalf("tracker ended early: %v", err)
		}
		if _, err := io.ReadFull(ref, want[:size]); err != nil {
			t.Fatalf("reference ended early: %v", err)
		}
		for offset := 0; offset < size; offset += 4 {
			side := offset / 4 % 2
			actual := float64(math.Float32frombits(binary.LittleEndian.Uint32(got[offset:])))
			expected := float64(math.Float32frombits(binary.LittleEndian.Uint32(want[offset:])))
			gotPower[side] += actual * actual
			wantPower[side] += expected * expected
			dot[side] += actual * expected
			errorPower[side] += (actual - expected) * (actual - expected)
		}
	}
	if count, err := ours.Read(got[:]); count != 0 || err != io.EOF {
		t.Fatalf("tracker did not end after %d frames: %d bytes, %v", frames, count, err)
	}
	const maxTail = rate * 8 / 10 // libopenmpt's optional 100 ms terminal fade
	if count, err := io.Copy(io.Discard, io.LimitReader(ref, maxTail+1)); err != nil || count > maxTail {
		t.Fatalf("unexpected reference tail: %d bytes, %v", count, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	for side := range gotPower {
		corr := dot[side] / math.Sqrt(gotPower[side]*wantPower[side])
		rms := math.Sqrt(errorPower[side] / wantPower[side])
		t.Logf("side %d: %d frames, correlation %.5f, normalized RMS error %.4f", side, frames, corr, rms)
		if !(corr > minCorrelation && rms < maxError) {
			t.Errorf("side %d differs from ffplay", side)
		}
	}
}
