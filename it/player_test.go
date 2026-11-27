package it

import (
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

func TestRenderPatternBreak(test *testing.T) {
	module := &Module{orders: []byte{0, 254, 1, 255}, speed: 2, tempo: 125, channels: 1, globalVolume: 128, mixVolume: 128, flags: 1}
	module.channelVolume[0] = 64
	module.pan[0] = 0
	module.samples = []sample{{left: []float32{1, 1}, c5: 8363, volume: 64, globalVolume: 64, loop: sampleLoop{end: 2}}}
	module.patterns = []pattern{{rows: make([][maxChannels]Note, 2)}, {rows: make([][maxChannels]Note, 3)}}
	module.patterns[0].rows[0][0] = Note{pitch: 60, instrument: 1, command: effect('C'), param: 2, fields: hasPitch | hasInstrument | hasCommand}
	module.patterns[1].rows[2][0] = Note{pitch: 254, fields: hasPitch}
	player := MakePlayer(module, 48000)
	var rows []int
	player.SetOnChangeRow(func(row int) { rows = append(rows, row) })
	// Read with an unaligned buffer to verify byte-stream behavior as well.
	var out bytes.Buffer
	pcm := player.RenderToPCM()
	written, err := io.CopyBuffer(struct{ io.Writer }{&out}, pcm, make([]byte, 137))
	if err != nil || written != 3840*8 || len(rows) != 2 || rows[0] != 0 || rows[1] != 2 {
		test.Fatalf("render: %d bytes, %v, rows %v", written, err, rows)
	}
	read := func(frame, side int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(out.Bytes()[frame*8+side*4:]))
	}
	if read(100, 0) != 1 || read(100, 1) != 0 || read(2000, 0) != 0 {
		test.Fatal("incorrect note trigger, panning or cut")
	}
}

func TestSamplePanRestoresChannelPanOnNextNote(test *testing.T) {
	module := &Module{channels: 1, speed: 6, tempo: 125, flags: 1, samples: []sample{
		{left: []float32{1}, c5: 8363, pan: 128 | 32},
		{left: []float32{1}, c5: 8363},
	}}
	module.pan[0] = 0 // hard left
	player := MakePlayer(module, 48000)
	track := &player.channels[0]
	play := func(instrument byte) {
		track.beginRow(Note{pitch: 60, instrument: instrument, fields: hasPitch | hasInstrument})
		track.updateTick(0)
	}
	play(1)
	if track.pan != 128 {
		test.Fatalf("sample pan = %d, want center", track.pan)
	}
	play(2)
	if track.pan != 0 {
		test.Fatalf("channel pan = %d, want hard left", track.pan)
	}
	play(1)
	track.beginRow(Note{command: effect('X'), param: 192, fields: hasCommand})
	track.updateTick(0)
	play(2)
	if track.pan != 192 {
		test.Fatalf("explicit pan = %d, want 192", track.pan)
	}
}

func TestPanbrelloKeepsBasePan(test *testing.T) {
	player := &Player{module: &Module{flags: 9}, speed: 6}
	track := channel{player: player, pan: 128, panbrelloPhase: 64}
	track.beginRow(Note{command: effect('Y'), param: 0x84, fields: hasCommand})
	track.updateTick(0)
	if track.pan != 128 || track.voice.panOffset != 32 || track.panbrelloPhase != 72 {
		test.Fatalf("Y84: base pan %d, offset %d, phase %d", track.pan, track.voice.panOffset, track.panbrelloPhase)
	}
	track.beginRow(Note{command: effect('Y'), fields: hasCommand})
	track.updateTick(0)
	if track.voice.panOffset == 0 || track.panbrelloPhase != 80 {
		test.Fatal("Y00 did not recall panbrello speed and depth")
	}
}

func TestAmigaSlideTruncatesEachTick(test *testing.T) {
	track := channel{player: &Player{module: &Module{}}, porta: 8363}
	track.pitchSlide(-4)
	if track.porta != 8343 {
		test.Fatalf("slide frequency %v, want 8343 Hz", track.porta)
	}
	track.pitchSlide(0)
	if track.porta != 8343 {
		test.Fatal("zero slide changed pitch")
	}
}

func TestSampleVibratoSweepsPerVoice(test *testing.T) {
	wave := &sample{vibratoRate: 64, vibratoDepth: 2, vibratoSweep: 128}
	active := voice{sample: wave}
	other := voice{sample: wave}
	const base = 8363.0
	for tick, amount := range []int{0, 1, 0, -2, 0, 2} {
		want := base * math.Exp2(float64(amount)/768)
		if got := active.applySampleVibrato(base); got != want {
			test.Fatalf("tick %d: frequency %v, want %v", tick, got, want)
		}
	}
	if other.applySampleVibrato(base) != base {
		test.Fatal("a new voice inherited another voice's vibrato")
	}
}

func TestBlueFlameAgainstFFplay(test *testing.T) {
	compareWithFFplay(test, "../t1/chris31b.it", 19134239, .99, .15)
}

func TestITCollectionAgainstFFplay(test *testing.T) {
	for _, fixture := range []struct {
		name                     string
		frames                   int
		minCorrelation, maxError float64
	}{
		{"Beyond Pluto.it", 7376064, .99, .10},       // IT 1.x instrument headers
		{"FF1 - Battle Remix.it", 2093790, .98, .21}, // mix volume 128
		{"Chippie Cousin.it", 2303616, .99, .16},
		{"Wakki To Um Yop.it", 5013632, .98, .21},
		{"Serenade Of A Squirrel.it", 11704320, .99, .15},    // sample pan restores channel pan
		{"Screaming Cuica.it", 24883200, .99, .15},           // legacy stereo separation
		{"Dingelding Schwing Schwing.it", 4124990, .99, .15}, // Amiga pitch slides // sample auto-vibrato
	} {
		test.Run(fixture.name, func(test *testing.T) {
			compareWithFFplay(test, filepath.Join("..", "t1", "Impulse Tracker", fixture.name), fixture.frames, fixture.minCorrelation, fixture.maxError)
		})
	}
}

func compareWithFFplay(test *testing.T, path string, frames int, minCorrelation, maxError float64) {
	test.Helper()
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		test.Skip("local IT fixture not available")
	}
	if err != nil {
		test.Fatal(err)
	}
	defer file.Close()
	module, err := Load(file)
	if err != nil {
		test.Fatal(err)
	}
	const rate = 48000
	pcm := MakePlayer(module, rate).RenderToPCM()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		test.Skip("FFmpeg is not installed")
	}
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "demuxer=libopenmpt").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Demuxer libopenmpt")) {
		test.Skip("FFmpeg has no libopenmpt demuxer")
	}
	// FFmpeg is only the test oracle. Playback does not call it or libopenmpt.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "libopenmpt", "-sample_rate", "48000", "-i", path, "-f", "f32le", "-")
	cmd.Stderr = os.Stderr
	ref, err := cmd.StdoutPipe()
	if err != nil {
		test.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		test.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			cancel()
			_ = cmd.Wait()
		}
	}()
	var got, want [32768]byte
	var gotPower, wantPower, dot, errorPower [2]float64
	remaining := frames * 8
	for remaining > 0 {
		size := min(remaining, len(got))
		remaining -= size
		if _, err := io.ReadFull(pcm, got[:size]); err != nil {
			test.Fatalf("tracker ended early: %v", err)
		}
		if _, err := io.ReadFull(ref, want[:size]); err != nil {
			test.Fatalf("reference ended early: %v", err)
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
	if extra, err := pcm.Read(got[:]); extra != 0 || err != io.EOF {
		test.Fatalf("tracker did not end at %d frames: %d bytes, %v", frames, extra, err)
	}
	// libopenmpt adds up to 100 ms of terminal fade after the musical frames.
	const maxTail = rate * 8 / 10
	if tail, err := io.Copy(io.Discard, io.LimitReader(ref, maxTail+1)); err != nil || tail > maxTail {
		test.Fatalf("unexpected reference tail: %d bytes, %v", tail, err)
	}
	if err := cmd.Wait(); err != nil {
		test.Fatal(err)
	}
	for side := range gotPower {
		corr := dot[side] / math.Sqrt(gotPower[side]*wantPower[side])
		rms := math.Sqrt(errorPower[side] / wantPower[side])
		test.Logf("side %d: %d frames, correlation %.5f, normalized RMS error %.4f", side, frames, corr, rms)
		if !(corr > minCorrelation && rms < maxError) {
			test.Errorf("side %d differs from ffplay", side)
		}
	}
}
