package main

import (
    "os"
    "log"
    "time"
    "math"

    "github.com/kazzmir/tracker/mod"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
    "github.com/hajimehoshi/ebiten/v2/audio"
)

type Engine struct {
    ModFile *mod.ModFile
    SampleRate int
    AudioContext *audio.Context

    SampleIndex int
}

func MakeEngine(modFile *mod.ModFile, sampleRate int, audioContext *audio.Context) (*Engine, error) {

    engine := &Engine{
        ModFile: modFile,
        SampleRate: sampleRate,
        AudioContext: audioContext,
    }

    player, err := audioContext.NewPlayerF32(engine)
    if err != nil {
        return nil, err
    }
    player.SetBufferSize(time.Second / 2)
    player.Play()

    return engine, nil
}

func (engine *Engine) Read(data []byte) (int, error) {
    sample := &engine.ModFile.Samples[12]

    log.Printf("Read %v samples", len(data) / 4 / 2)

    for i := range data {
        data[i] = 0
    }

    for i := 0; i < len(data) / 4 / 2; i += 1 {

        if engine.SampleIndex >= len(sample.Data) {
            log.Printf("break at Sample index %v i %v", engine.SampleIndex, i)
            break
        }

        value := (float32(sample.Data[engine.SampleIndex])) / 128
        bits := math.Float32bits(value)
        data[i*8+0] = byte(bits)
        data[i*8+1] = byte(bits >> 8)
        data[i*8+2] = byte(bits >> 16)
        data[i*8+3] = byte(bits >> 24)

        data[i*8+4] = byte(bits)
        data[i*8+5] = byte(bits >> 8)
        data[i*8+6] = byte(bits >> 16)
        data[i*8+7] = byte(bits >> 24)

        if i % 5 == 0 {
            // engine.SampleIndex = (engine.SampleIndex + 1) % len(sample.Data)
            engine.SampleIndex += 1
        }
    }
    log.Printf("  sample index: %v", engine.SampleIndex)

    return len(data), nil
}

func (engine *Engine) Update() error {

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
        }
    }

    return nil
}

func (engine *Engine) Draw(screen *ebiten.Image) {
}

func (engine *Engine) Layout(outsideWidth, outsideHeight int) (int, int) {
    return outsideWidth, outsideHeight
}

func main(){
    log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
    if len(os.Args) < 2 {
        log.Println("Usage: tracker <path to mod file>")
        return
    }
    path := os.Args[1]
    file, err := os.Open(path)
    if err != nil {
        log.Printf("Error opening %v: %v", path, err)
    }

    modFile, err := mod.Load(file)
    if err != nil {
        log.Printf("Error loading %v: %v", path, err)
        return
    } else {
        log.Printf("Successfully loaded %v", path)
        log.Printf("Mod name: '%v'", modFile.Name)
    }

    ebiten.SetWindowSize(640, 480)
    ebiten.SetWindowTitle("Mod Tracker")
    ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

    sampleRate := 44100
    audioContext := audio.NewContext(sampleRate)

    engine, err := MakeEngine(modFile, sampleRate, audioContext)
    if err != nil {
        log.Printf("Error creating engine: %v", err)
        return
    }

    err = ebiten.RunGame(engine)
    if err != nil {
        log.Printf("Error: %v", err)
    }
}
