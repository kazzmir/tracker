package xm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestPatternLoopAndEmptyOrder(t *testing.T) {
	file := &XMFile{Channels: 1, Tempo: 1, BPM: 125, Orders: []byte{0, 1}, Patterns: []Pattern{
		{Rows: 3, PatternData: []byte{0x98, 14, 0x60, 0x98, 14, 0x62, 0x80}},
		{Rows: 2},
	}}
	p := MakePlayer(file, 48000)
	// Pattern 0 plays rows 0 and 1 three times, then row 2 once.
	n, err := io.Copy(io.Discard, p.RenderToPCM())
	if err != nil || n != 9*960*8 {
		t.Fatalf("rendered %d bytes, error %v", n, err)
	}
}

func TestSampleSelectionAndSustain(t *testing.T) {
	instrument := &Instrument{Samples: []Sample{{Volume: 12}, {Volume: 50, Panning: 255, Data: []float32{1}}}, VolumeEnvelope: Envelope{
		Count: 3, Flags: 3, Sustain: 1, Points: [24]uint16{0, 0, 2, 64, 4, 0},
	}}
	instrument.Keymap[48] = 1
	p := MakePlayer(&XMFile{Channels: 1, LinearPeriods: true, Instruments: []*Instrument{instrument}}, 48000)
	c := p.Channels[0]
	c.note = Note{NoteNumber: 49, Instrument: 1}
	c.updateTick(0)
	if c.sample != &instrument.Samples[1] || c.volume != 50 || c.period != 4608 || c.targetLeft > .02 {
		t.Fatalf("wrong voice selected: sample %p, volume %d, period %v", c.sample, c.volume, c.period)
	}
	c.note = Note{}
	for tick := 1; tick < 5; tick++ {
		c.updateTick(tick)
	}
	if c.volumeTick != 2 || c.targetRight < .28 {
		t.Fatalf("envelope didn't hold: tick %d gain %v", c.volumeTick, c.targetRight)
	}
	c.note = Note{NoteNumber: 97}
	c.updateTick(0)
	c.updateTick(1)
	if c.targetRight > .15 {
		t.Fatalf("envelope didn't release: gain %v", c.targetRight)
	}
}

func TestDelayedNoteVolume(t *testing.T) {
	inst := &Instrument{Samples: []Sample{{Volume: 64, Data: []float32{1}}}}
	p := MakePlayer(&XMFile{Channels: 1, LinearPeriods: true, Instruments: []*Instrument{inst}}, 48000)
	c := p.Channels[0]
	c.note = Note{NoteNumber: 49, Instrument: 1}
	c.updateTick(0)
	c.note = Note{NoteNumber: 50, Instrument: 1, Volume: 0x20, EffectType: EffectExtended, EffectParameter: 0xd2}
	c.updateTick(0)
	c.updateTick(1)
	if c.volume != 64 || c.period != 4608 {
		t.Fatal("delayed row changed the playing voice before its trigger")
	}
	c.updateTick(2)
	if c.volume != 16 || c.period != 4544 {
		t.Fatalf("delayed trigger: volume %d, period %v", c.volume, c.period)
	}
}

func TestT1XMAgainstFFplay(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "demuxer=libopenmpt").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Demuxer libopenmpt")) {
		t.Skip("FFmpeg has no libopenmpt demuxer")
	}
	fixtures := []struct {
		name   string
		frames int
	}{
		{"song1.xm", 6981120},
		{"song2.xm", 12752160},
		{"song3.xm", 8914688},
		{"song4.xm", 11519040},
		{"song5.xm", 14378496},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			compareWithFFplay(t, ffmpeg, filepath.Join("..", "t1", fixture.name), fixture.frames)
		})
	}
}

func compareWithFFplay(t *testing.T, ffmpeg, path string, wantFrames int) {
	t.Helper()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		t.Skip("local XM fixture not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	module, err := Load(f, log.New(io.Discard, "", 0))
	if err != nil {
		t.Fatal(err)
	}
	const rate = 48000
	pcm := MakePlayer(module, rate).RenderToPCM()
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
	var got, want [32768]byte
	var xx, yy, xy [2]float64
	var frames int
	for {
		n, err := io.ReadFull(pcm, got[:])
		if err == io.EOF {
			break
		}
		if (err != nil && err != io.ErrUnexpectedEOF) || n%8 != 0 {
			t.Fatalf("tracker read: %d bytes, %v", n, err)
		}
		if _, err := io.ReadFull(ref, want[:n]); err != nil {
			t.Fatalf("reference ended early at frame %d: %v", frames, err)
		}
		frames += n / 8
		for i := 0; i < n; i += 4 {
			ch := i / 4 % 2
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(got[i:])))
			y := float64(math.Float32frombits(binary.LittleEndian.Uint32(want[i:])))
			xx[ch] += x * x
			yy[ch] += y * y
			xy[ch] += x * y
		}
	}
	// libopenmpt may append a 100 ms terminal fade. The musical
	// part must have exactly the expected number of frames.
	const maxTail = rate / 10 * 8
	tail, err := io.Copy(io.Discard, io.LimitReader(ref, maxTail+1))
	if err != nil || tail > maxTail || frames != wantFrames {
		t.Fatalf("duration mismatch: %d frames, %d reference tail bytes, %v", frames, tail, err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	for ch := range xx {
		correlation := xy[ch] / math.Sqrt(xx[ch]*yy[ch])
		rms := math.Sqrt((xx[ch] + yy[ch] - 2*xy[ch]) / yy[ch])
		t.Logf("channel %d: %d frames, correlation %.5f, normalized RMS error %.4f", ch, frames, correlation, rms)
		if !(correlation > .99 && rms < .15) {
			t.Errorf("channel %d differs from ffplay", ch)
		}
	}
}
