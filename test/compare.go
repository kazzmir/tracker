package main

import (
    "log"
    "os"
    "io"
    "math"
    "fmt"

    tracker_lib "github.com/kazzmir/tracker/lib"
    "github.com/kazzmir/tracker/mod"
    "github.com/kazzmir/tracker/s3m"
    "github.com/kazzmir/tracker/xm"

    "github.com/go-audio/wav"
    "github.com/go-audio/audio"
    "gonum.org/v1/gonum/floats"
    "gonum.org/v1/gonum/dsp/fourier"
    "github.com/fatih/color"
)

func tryLoadS3m(path string) (*s3m.S3MFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    return s3m.Load(file, log.New(io.Discard, "", 0))
}

func tryLoadMod(path string) (*mod.ModFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    return mod.Load(file)
}

func tryLoadXM(path string) (*xm.XMFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }

    defer file.Close()

    return xm.Load(file, log.New(io.Discard, "", 0))
}

type Renderer interface {
    RenderToPCM() io.Reader
}

func TryLoad(path string, sampleRate int) (Renderer, error) {
    s3mFile, err := tryLoadS3m(path)
    if err == nil {
        return s3m.MakePlayer(s3mFile, sampleRate), nil
    }

    // log.Printf("Unable to load s3m: %v", err)

    xmFile, err := tryLoadXM(path)
    if err == nil {
        return xm.MakePlayer(xmFile, sampleRate), nil
    }

    // log.Printf("Unable to load xm: %v", err)

    modFile, err := tryLoadMod(path)
    if err != nil {
        return nil, err
    }

    return mod.MakePlayer(modFile, sampleRate), nil
}

func cosineSimilarity(wave1, wave2 []float64) float64 {
	if len(wave1) != len(wave2) {
		panic("waveforms must be same length")
	}
	dot := floats.Dot(wave1, wave2)
	norm1 := math.Sqrt(floats.Dot(wave1, wave1))
	norm2 := math.Sqrt(floats.Dot(wave2, wave2))
	if norm1 == 0 || norm2 == 0 {
		return 0
	}
	return dot / (norm1 * norm2)
}

func cmplxAbs(c complex128) float64 {
	return math.Hypot(real(c), imag(c))
}

func spectralCosineSimilarity(wave1, wave2 []float64) float64 {
	if len(wave1) != len(wave2) {
		panic("waveforms must be same length")
	}
	fft := fourier.NewFFT(len(wave1))
	spec1 := fft.Coefficients(nil, wave1)
	spec2 := fft.Coefficients(nil, wave2)

	// Compare magnitude spectra
	mag1 := make([]float64, len(spec1))
	mag2 := make([]float64, len(spec2))
	for i := range spec1 {
		mag1[i] = cmplxAbs(spec1[i])
		mag2[i] = cmplxAbs(spec2[i])
	}
	return cosineSimilarity(mag1, mag2)
}

func compareBuffers(buf1 []float64, buf2 []float64) float64 {
    /*
    cosine := cosineSimilarity(buf1.Data, buf2.Data)
    log.Printf("Cosine similarity: %f", cosine)
    */
    value := spectralCosineSimilarity(buf1, buf2)
    // log.Printf("Spectral cosine similarity: %f", value)
    return value
}

func allZeros(data []int) bool {
    for _, v := range data {
        if v != 0 {
            return false
        }
    }
    return true
}

func compare(wav1 string, wav2 string) ([]float64, error) {
    file1, err := os.Open(wav1)
    if err != nil {
        return nil, err
    }
    defer file1.Close()

    file2, err := os.Open(wav2)
    if err != nil {
        return nil, err
    }
    defer file2.Close()

    data1 := wav.NewDecoder(file1)
    data2 := wav.NewDecoder(file2)

    data1.ReadInfo()
    data2.ReadInfo()

    bytesInSecond := data1.Format().SampleRate * data1.Format().NumChannels * int(data1.SampleBitDepth()) / 8
    // log.Printf("Bytes per second: %d", bytesInSecond)

    bufferSize := bytesInSecond / 60
    seconds := 4

    var scores []float64

    for range bytesInSecond * seconds / bufferSize {
        // log.Printf("Part %d", i + 1)
        buffer1 := audio.IntBuffer{
            Format: data1.Format(),
            Data: make([]int, bufferSize),
            SourceBitDepth: int(data1.SampleBitDepth()),
        }

        _, err := data1.PCMBuffer(&buffer1)
        if err != nil {
            return nil, err
        }

        // log.Printf("Read %d samples from %s", n, wav1)

        bytesInSecond = data2.Format().SampleRate * data2.Format().NumChannels * int(data2.SampleBitDepth()) / 8

        buffer2 := audio.IntBuffer{
            Format: data2.Format(),
            Data: make([]int, bufferSize),
            SourceBitDepth: int(data2.SampleBitDepth()),
        }
        _, err = data2.PCMBuffer(&buffer2)
        if err != nil {
            return nil, err
        }

        if allZeros(buffer1.Data) && allZeros(buffer2.Data) {
            // log.Printf("Skipping part %d due to all zeros in one of the buffers", i + 1)
            break
        }

        // log.Printf("Read %d samples from %s", m, wav2)

        similar := compareBuffers(buffer1.AsFloatBuffer().Data, buffer2.AsFloatBuffer().Data)
        scores = append(scores, similar)
    }

    return scores, nil
}

func renderScores(testFile string, scores []float64) {
    good := color.New(color.FgGreen)
    ok := color.New(color.FgCyan)
    bad := color.New(color.FgRed)
    warn := color.New(color.FgHiYellow)

    fmt.Printf("%v: ", testFile)

    for _, score := range scores {
        switch {
            case score >= 0.93: good.Print("+")
            case score >= 0.85: ok.Print("o")
            case score >= 0.75: warn.Print("x")
            default: bad.Print("-")
        }
    }

    fmt.Println()
}

func runTest(testFile string, goldFile string) {
    tmpWav := "tmp.wav"

    sampleRate := 44100

    player, err := TryLoad(testFile, sampleRate)
    if err != nil {
        log.Fatalf("Error loading tracker file: %v", err)
    }

    err = tracker_lib.SaveToWav(tmpWav, player.RenderToPCM(), sampleRate, log.New(io.Discard, "", 0))
    if err != nil {
        log.Fatalf("Error saving to WAV: %v", err)
    }

    scoresGood, err := compare(tmpWav, goldFile)
    if err != nil {
        log.Fatalf("Error comparing files: %v", err)
    }

    renderScores(testFile, scoresGood)
}

func main() {
    runTest("test/test.compat.xm", "test/test.gold.good.wav")
    runTest("test/test1.xm", "test/test1.gold.wav")

    /*
    wav1 := "test.wav"
    wav2 := "test.gold.good.wav"

    scoresGood, err := compare(wav1, wav2)
    if err != nil {
        log.Fatalf("Error comparing files: %v", err)
    }

    renderScores(scoresGood)
    */
}
