package s3m

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

func TestAquaAgainstFFplay(t *testing.T) {
	path := "../aqua.s3m"
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		path = "../data/data/aqua.s3m"
		file, err = os.Open(path)
	}
	if os.IsNotExist(err) {
		t.Skip("local aqua.s3m fixture not available")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	module, err := Load(file, log.New(io.Discard, "", 0))
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
	const frames = 7299840
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
		if !(corr > .99 && rms < .12) {
			t.Errorf("side %d differs from ffplay", side)
		}
	}
}
