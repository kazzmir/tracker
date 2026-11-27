package it

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestRenderPatternBreak(test *testing.T) {
	module := &Module{orders: []byte{0, 254, 1, 255}, speed: 2, tempo: 125, channels: 1, globalVolume: 128, mixVolume: 128, separation: 128, flags: 1}
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

func TestBlueFlameAgainstFFplay(test *testing.T) {
	const path = "../t1/chris31b.it"
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
	const frames = 19134239
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
		if !(corr > .99 && rms < .15) {
			test.Errorf("side %d differs from ffplay", side)
		}
	}
}
