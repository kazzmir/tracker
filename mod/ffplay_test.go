package mod

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestIntroAgainstFFplay(t *testing.T) {
	compareMODWithFFplay(t, filepath.Join("..", "t1", "intro.mod"), true)
}

func TestProTrackerCollectionAgainstFFplay(t *testing.T) {
	for _, song := range []string{"Azzura", "Safe Sex. Comp", "Lady Madonna", "P.F.D. Guitars", "Network South East 34", "Anthem Of Techno"} {
		t.Run(song, func(t *testing.T) {
			compareMODWithFFplay(t, filepath.Join("..", "t1", "ProTracker", song+".mod"), false)
		})
	}
}

func compareMODWithFFplay(t *testing.T, path string, silentTail bool) {
	t.Helper()
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		t.Skipf("local fixture %s not available", path)
	}
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("FFmpeg not installed")
	}
	if out, err := exec.Command(ffmpeg, "-hide_banner", "-h", "demuxer=libopenmpt").CombinedOutput(); err != nil || !bytes.Contains(out, []byte("Demuxer libopenmpt")) {
		t.Skip("FFmpeg does not have the libopenmpt demuxer")
	}

	module, err := Load(file)
	if err != nil {
		t.Fatal(err)
	}
	const rate = 48000
	const frameBytes = 8 // two float32 samples
	ours := MakePlayer(module, rate).RenderToPCM()

	// ffplay and ffmpeg use the same libavformat module demuxer. Compare
	// float PCM before device resampling, volume controls, or clipping.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-nostdin", "-v", "error", "-f", "libopenmpt", "-i", path, "-f", "f32le", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
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

	var got, want [8192]byte
	var sumErr, sumRef, sumGot, dot [2]float64
	frames := 0
	for {
		n, err := io.ReadFull(ours, got[:])
		if err == io.EOF {
			break
		}
		if (err != nil && err != io.ErrUnexpectedEOF) || n%frameBytes != 0 {
			t.Fatalf("tracker read: %d bytes, %v", n, err)
		}
		if _, err := io.ReadFull(ref, want[:n]); err != nil {
			t.Fatalf("FFmpeg audio ended early at frame %d: %v", frames, err)
		}
		for i := 0; i < n; i += 4 {
			ch := (i / 4) % 2
			x := float64(math.Float32frombits(binary.LittleEndian.Uint32(got[i:])))
			y := float64(math.Float32frombits(binary.LittleEndian.Uint32(want[i:])))
			sumErr[ch] += (x - y) * (x - y)
			sumRef[ch] += y * y
			sumGot[ch] += x * x
			dot[ch] += x * y
		}
		frames += n / frameBytes
	}

	// OpenMPT may append a short end fade.
	maxTail := rate * frameBytes / 10
	tail, err := io.ReadAll(io.LimitReader(ref, int64(maxTail+1)))
	if err != nil || len(tail) > maxTail || len(tail)%frameBytes != 0 {
		t.Fatalf("unexpected reference tail: %d bytes, %v", len(tail), err)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("FFmpeg: %v: %s", err, stderr.String())
	}
	if silentTail {
		for i := 0; i < len(tail); i += 4 {
			if sample := math.Float32frombits(binary.LittleEndian.Uint32(tail[i:])); !(math.Abs(float64(sample)) < 1e-4) {
				t.Fatalf("reference has audible data after tracker EOF: %v", sample)
			}
		}
	}
	for ch := range sumRef {
		rms := math.Sqrt(sumErr[ch] / sumRef[ch])
		correlation := dot[ch] / math.Sqrt(sumRef[ch]*sumGot[ch])
		t.Logf("channel %d: %d frames, normalized RMS error %.4f, correlation %.5f", ch, frames, rms, correlation)
		if !(rms < 0.06 && correlation > 0.998) {
			t.Errorf("channel %d differs from ffplay", ch)
		}
	}
}
