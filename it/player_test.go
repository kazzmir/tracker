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

func TestRenderPatternBreak(t *testing.T) {
	m := &Module{orders: []byte{0, 254, 1, 255}, speed: 2, tempo: 125, channels: 1, globalVolume: 128, mixVolume: 128, separation: 128, flags: 1}
	m.channelVolume[0] = 64
	m.pan[0] = 0
	m.samples = []sample{{left: []float32{1, 1}, c5: 8363, volume: 64, globalVolume: 64, loop: sampleLoop{end: 2}}}
	m.patterns = []pattern{{rows: make([][maxChannels]Note, 2)}, {rows: make([][maxChannels]Note, 3)}}
	m.patterns[0].rows[0][0] = Note{pitch: 60, instrument: 1, command: effect('C'), param: 2, fields: hasPitch | hasInstrument | hasCommand}
	m.patterns[1].rows[2][0] = Note{pitch: 254, fields: hasPitch}
	p := MakePlayer(m, 48000)
	var rows []int
	p.SetOnChangeRow(func(row int) { rows = append(rows, row) })
	// Read with an unaligned buffer to verify byte-stream behavior as well.
	var out bytes.Buffer
	pcm := p.RenderToPCM()
	n, err := io.CopyBuffer(struct{ io.Writer }{&out}, pcm, make([]byte, 137))
	if err != nil || n != 3840*8 || len(rows) != 2 || rows[0] != 0 || rows[1] != 2 {
		t.Fatalf("render: %d bytes, %v, rows %v", n, err, rows)
	}
	read := func(frame, side int) float32 {
		return math.Float32frombits(binary.LittleEndian.Uint32(out.Bytes()[frame*8+side*4:]))
	}
	if read(100, 0) != 1 || read(100, 1) != 0 || read(2000, 0) != 0 {
		t.Fatal("incorrect note trigger, panning or cut")
	}
}

func TestBlueFlameAgainstFFplay(t *testing.T) {
	const path = "../t1/chris31b.it"
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		t.Skip("local IT fixture not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := Load(f)
	if err != nil {
		t.Fatal(err)
	}
	const rate = 48000
	const frames = 19134239
	pcm := MakePlayer(m, rate).RenderToPCM()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg is not installed")
	}
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "demuxer=libopenmpt").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Demuxer libopenmpt")) {
		t.Skip("FFmpeg has no libopenmpt demuxer")
	}
	// FFmpeg is only the test oracle. Playback does not call it or libopenmpt.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "libopenmpt", "-sample_rate", "48000", "-i", path, "-f", "f32le", "-")
	cmd.Stderr = os.Stderr
	ref, err := cmd.StdoutPipe()
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
	var got, want [32768]byte
	var xx, yy, xy, ee [2]float64
	remaining := frames * 8
	for remaining > 0 {
		n := min(remaining, len(got))
		remaining -= n
		if _, err := io.ReadFull(pcm, got[:n]); err != nil {
			t.Fatalf("tracker ended early: %v", err)
		}
		if _, err := io.ReadFull(ref, want[:n]); err != nil {
			t.Fatalf("reference ended early: %v", err)
		}
		for i := 0; i < n; i += 4 {
			side := i / 4 % 2
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(got[i:])))
			y := float64(math.Float32frombits(binary.LittleEndian.Uint32(want[i:])))
			xx[side] += x * x
			yy[side] += y * y
			xy[side] += x * y
			ee[side] += (x - y) * (x - y)
		}
	}
	if n, err := pcm.Read(got[:]); n != 0 || err != io.EOF {
		t.Fatalf("tracker did not end at %d frames: %d bytes, %v", frames, n, err)
	}
	// libopenmpt adds up to 100 ms of terminal fade after the musical frames.
	const maxTail = rate * 8 / 10
	if n, err := io.Copy(io.Discard, io.LimitReader(ref, maxTail+1)); err != nil || n > maxTail {
		t.Fatalf("unexpected reference tail: %d bytes, %v", n, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	for side := range xx {
		corr := xy[side] / math.Sqrt(xx[side]*yy[side])
		rms := math.Sqrt(ee[side] / yy[side])
		t.Logf("side %d: %d frames, correlation %.5f, normalized RMS error %.4f", side, frames, corr, rms)
		if !(corr > .99 && rms < .15) {
			t.Errorf("side %d differs from ffplay", side)
		}
	}
}
