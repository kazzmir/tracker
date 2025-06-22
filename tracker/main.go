package main

import (
    "os"
    "log"
    "time"
    "io"
    "sync"
    // for discard
    // "io/ioutil"
    "flag"
    "runtime/pprof"
    "encoding/binary"

    "github.com/kazzmir/tracker/mod"
    "github.com/kazzmir/tracker/s3m"
    "github.com/kazzmir/tracker/data"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
    "github.com/hajimehoshi/ebiten/v2/audio"

    "github.com/ebitenui/ebitenui"
)

type TrackerPlayer interface {
    Update(float32)
    NextOrder()
    PreviousOrder()
    ResetRow()
    GetCurrentOrder() int
}

type Engine struct {
    Player TrackerPlayer

    AudioContext *audio.Context
    UI *ebitenui.UI
    UIHooks UIHooks

    Players []*audio.Player
    Start sync.Once
    updates uint64
}

func MakeEngine(modPlayer *mod.Player, audioContext *audio.Context) (*Engine, error) {
    engine := &Engine{
        Player: modPlayer,
        AudioContext: audioContext,
    }

    engine.UI, engine.UIHooks = makeUI(modPlayer)

    modPlayer.OnChangeRow = func(row int) {
        engine.UIHooks.UpdateRow(row)
    }

    modPlayer.OnChangeOrder = func(order int, pattern int) {
        engine.UIHooks.UpdateOrder(order,pattern)
    }

    modPlayer.OnChangeSpeed = func(speed int, bpm int) {
        engine.UIHooks.UpdateSpeed(speed, bpm)
    }

    for _, channel := range modPlayer.Channels {
        playChannel, err := audioContext.NewPlayerF32(channel)
        if err != nil {
            return nil, err
        }
        playChannel.SetBufferSize(time.Second / 20)
        playChannel.SetVolume(0.3)
        engine.Players = append(engine.Players, playChannel)
        // playChannel.Play()
    }

    return engine, nil
}

func MakeS3MEngine(s3mPlayer *s3m.Player, audioContext *audio.Context) (*Engine, error) {
    engine := &Engine{
        Player: s3mPlayer,
        AudioContext: audioContext,
    }

    engine.UI, engine.UIHooks = makeUI(s3mPlayer)

    s3mPlayer.OnChangeRow = func(row int) {
        engine.UIHooks.UpdateRow(row)
    }

    s3mPlayer.OnChangeOrder = func(order int, pattern int) {
        engine.UIHooks.UpdateOrder(order,pattern)
    }

    s3mPlayer.OnChangeSpeed = func(speed int, bpm int) {
        engine.UIHooks.UpdateSpeed(speed, bpm)
    }

    for _, channel := range s3mPlayer.Channels {
        playChannel, err := audioContext.NewPlayerF32(channel)
        if err != nil {
            return nil, err
        }
        playChannel.SetBufferSize(time.Second / 20)
        playChannel.SetVolume(0.5)
        engine.Players = append(engine.Players, playChannel)
        // playChannel.Play()
    }

    return engine, nil

}

func (engine *Engine) Update() error {
    engine.updates += 1

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
            case ebiten.KeySpace:
                engine.Player.ResetRow()
            case ebiten.KeyLeft:
                engine.Player.PreviousOrder()
                log.Printf("New order: %d", engine.Player.GetCurrentOrder())
            case ebiten.KeyRight:
                engine.Player.NextOrder()
                log.Printf("New order: %d", engine.Player.GetCurrentOrder())
        }
    }

    if engine.AudioContext.IsReady() {
        if engine.updates >= 0 {
            engine.Start.Do(func() {
                for _, player := range engine.Players {
                    player.Play()
                }
            })
        }
        engine.Player.Update(1.0/60)
    }

    engine.UI.Update()

    return nil
}

func (engine *Engine) Draw(screen *ebiten.Image) {
    engine.UI.Draw(screen)
}

func (engine *Engine) Layout(outsideWidth, outsideHeight int) (int, int) {
    return outsideWidth, outsideHeight
}

func saveToWav(path string, reader io.Reader, sampleRate int) error {
    outputFile, err := os.Create(path)
    if err != nil {
        return err
    }
    defer outputFile.Close()

    dataLength := int64(0)
    bitsPerSample := 32
    bytePerBloc := 2 * bitsPerSample / 8
    bytePerSec := sampleRate * bytePerBloc // 2 channels, 32 bits per sample

    binary.Write(outputFile, binary.LittleEndian, []byte("RIFF"))
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength + 36))
    binary.Write(outputFile, binary.LittleEndian, []byte("WAVE"))
    binary.Write(outputFile, binary.LittleEndian, []byte("fmt "))
    binary.Write(outputFile, binary.LittleEndian, uint32(16))  // BlocSize
    binary.Write(outputFile, binary.LittleEndian, uint16(3))   // AudioFormat, IEEE float
    binary.Write(outputFile, binary.LittleEndian, uint16(2))
    binary.Write(outputFile, binary.LittleEndian, uint32(sampleRate))
    binary.Write(outputFile, binary.LittleEndian, uint32(bytePerSec))
    binary.Write(outputFile, binary.LittleEndian, uint16(bytePerBloc))
    binary.Write(outputFile, binary.LittleEndian, uint16(bitsPerSample))
    binary.Write(outputFile, binary.LittleEndian, []byte("data"))
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength))
    dataLength, err = io.Copy(outputFile, reader)

    // now that we know the data length, we can go back and write it in the header
    outputFile.Seek(4, io.SeekStart)
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength + 36))
    outputFile.Seek(40, io.SeekStart)
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength))

    log.Printf("Copied %v bytes to %v", dataLength, path)

    return err
}

func tryLoadS3m(path string) (*s3m.S3MFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    return s3m.Load(file)
}

func tryLoadMod(path string) (*mod.ModFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    return mod.Load(file)
}

func main(){
    log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)

    log.Printf("Running")

    profile := flag.Bool("profile", false, "Enable profiling")
    wav := flag.String("wav", "", "Output wav file")
    flag.Parse()

    if len(flag.Args()) == 0 && *wav != "" {
        log.Println("Usage: tracker [-wav <output-path>] <path to mod file>")
        return
    }

    /*
    if *profile {
        log.Println("Profiling enabled")
        f, err := os.Create("profile.out")
        if err != nil {
            log.Printf("Error creating profile file: %v", err)
            return
        }
        defer f.Close()
        pprof.StartCPUProfile(f)
        defer pprof.StopCPUProfile()
    }
    */

    var modFile *mod.ModFile
    var s3mFile *s3m.S3MFile

    if len(flag.Args()) > 0 {
        path := flag.Args()[0]
        var err error

        s3mFile, err = tryLoadS3m(path)
        if err != nil {
            log.Printf("Unable to load s3m: %v", err)

            modFile, err = tryLoadMod(path)
            if err != nil {
                log.Printf("Error loading %v: %v", path, err)
                return
            } else {
                log.Printf("Successfully loaded %v", path)
                log.Printf("Mod name: '%v'", modFile.Name)
            }
        }
    } else {
        dataFile, name, err := data.FindMod()
        if err != nil {
            log.Printf("Error finding mod file: %v", err)
            return
        }

        modFile, err = mod.Load(dataFile)
        if err != nil {
            log.Printf("Error loading mod file: %v", err)
            return
        } else {
            log.Printf("Successfully loaded %v", name)
            log.Printf("Mod name: '%v'", modFile.Name)
        }
        dataFile.Close()
    }

    sampleRate := 44100

    if s3mFile != nil {
        s3mPlayer := s3m.MakePlayer(s3mFile, sampleRate)

        if *wav != "" {
            log.Printf("Rendering to %v", *wav)

            err := saveToWav(*wav, s3mPlayer.RenderToPCM(), sampleRate)
            if err != nil {
                log.Printf("Error saving to wav: %v", err)
                return
            }
        } else {

            ebiten.SetTPS(60)
            ebiten.SetWindowSize(1000, 800)
            ebiten.SetWindowTitle("Mod Tracker")
            // ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

            audioContext := audio.NewContext(sampleRate)

            /*
            modPlayer.CurrentOrder = 0x10
            modPlayer.Channels[0].Mute = true
            modPlayer.Channels[1].Mute = true
            modPlayer.Channels[3].Mute = true
            */

            engine, err := MakeS3MEngine(s3mPlayer, audioContext)
            if err != nil {
                log.Printf("Error creating engine: %v", err)
                return
            }

            err = ebiten.RunGame(engine)
            if err != nil {
                log.Printf("Error: %v", err)
            }
        }
    } else if modFile != nil {

        modPlayer := mod.MakePlayer(modFile, sampleRate)

        if *wav != "" {
            log.Printf("Rendering to %v", *wav)

            err := saveToWav(*wav, modPlayer.RenderToPCM(), sampleRate)
            if err != nil {
                log.Printf("Error saving to wav: %v", err)
                return
            }

            /*
            reader := modPlayer.RenderToPCM()
            out, err := os.Create(*wav)
            if err != nil {
                log.Printf("Error creating output file: %v", err)
                return
            }
            io.Copy(out, reader)
            out.Close()
            */

            /*
            for range 10 {
                modPlayer = mod.MakePlayer(modFile, sampleRate)
                io.Copy(ioutil.Discard, modPlayer.RenderToPCM())
            }
            */

            log.Printf("Done rendering")

            if *profile {
                out, err := os.Create("profile.mem")
                if err != nil {
                    log.Printf("Could not create heap profile: %v", err)
                } else {
                    pprof.WriteHeapProfile(out)
                    out.Close()
                }
            }

        } else {
            ebiten.SetTPS(60)
            ebiten.SetWindowSize(1000, 800)
            ebiten.SetWindowTitle("Mod Tracker")
            // ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

            audioContext := audio.NewContext(sampleRate)

            /*
            modPlayer.CurrentOrder = 0x10
            modPlayer.Channels[0].Mute = true
            modPlayer.Channels[1].Mute = true
            modPlayer.Channels[3].Mute = true
            */

            engine, err := MakeEngine(modPlayer, audioContext)
            if err != nil {
                log.Printf("Error creating engine: %v", err)
                return
            }

            err = ebiten.RunGame(engine)
            if err != nil {
                log.Printf("Error: %v", err)
            }
        }
    }

    log.Printf("Finished")
}
