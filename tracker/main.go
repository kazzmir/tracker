package main

import (
    "os"
    "log"
    "time"
    "io"
    "sync"
    "bytes"
    "io/fs"
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
    UIPlayer

    Update(float32)
    NextOrder()
    PreviousOrder()
    ResetRow()
    GetCurrentOrder() int
    SetOnChangeRow(func(int))
    SetOnChangeOrder(func(int, int))
    SetOnChangeSpeed(func(int, int))
    GetChannelReaders() []io.Reader
    RenderToPCM() io.Reader
}

type System struct {
    engine *Engine
}

func (system *System) LoadSong(path string) {
    system.engine.LoadSongFromFilesystem(data.Data, "data/" + path)
}

func (system *System) GetGlobalVolume() int {
    return int(system.engine.volume * 100)
}

func (system *System) SetGlobalVolume(volume int) {
    system.engine.SetVolume(float64(volume) / 100.0)
}

func (system *System) GetFiles() []string {
    return data.ListFiles()
}

func (system *System) GetSampleRate() int {
    return system.engine.AudioContext.SampleRate()
}

func (system *System) DoPause() {
    system.engine.DoPause()
}

type Engine struct {
    Player TrackerPlayer

    AudioContext *audio.Context
    UI *ebitenui.UI
    UIHooks UIHooks

    volume float64
    fps int

    Players []*audio.Player
    Start sync.Once
    updates uint64
    Paused bool
}

func MakeEngine(player TrackerPlayer, audioContext *audio.Context, fps int) (*Engine, error) {
    engine := &Engine{
        AudioContext: audioContext,
        fps: fps,
        volume: 0.6,
    }

    engine.Initialize(player)

    return engine, nil
}

func (engine *Engine) LoadSongFromFilesystem(filesystem fs.FS, path string) {
    loadS3m := func() (TrackerPlayer, error) {
        file, err := filesystem.Open(path)
        if err != nil {
            return nil, err
        }
        defer file.Close()

        // FIXME: use a custom seekable buffering object that only loads sections
        // of the file into memory as needed so that we abort loading a file if it
        // is not an s3m
        var buffer bytes.Buffer
        _, err = io.Copy(&buffer, file)
        if err != nil {
            return nil, err
        }

        loaded, err := s3m.Load(bytes.NewReader(buffer.Bytes()))
        if err != nil {
            return nil, err
        }

        return s3m.MakePlayer(loaded, engine.AudioContext.SampleRate()), nil
    }

    loadMod := func() (TrackerPlayer, error) {
        file, err := filesystem.Open(path)

        if err != nil {
            return nil, err
        }
        defer file.Close()

        loaded, err := mod.Load(file)
        if err != nil {
            return nil, err
        }

        return mod.MakePlayer(loaded, engine.AudioContext.SampleRate()), nil
    }

    player, err := loadS3m()
    if err != nil {
        player, err = loadMod()
    }

    if err != nil {
        log.Printf("Not an s3m or mod file %v: %v", path, err)
        return
    }

    engine.Initialize(player)
}

func (engine *Engine) Initialize(player TrackerPlayer) {
    engine.UI, engine.UIHooks = makeUI(player, &System{engine: engine})

    player.SetOnChangeRow(engine.UIHooks.UpdateRow)
    player.SetOnChangeOrder(engine.UIHooks.UpdateOrder)
    player.SetOnChangeSpeed(engine.UIHooks.UpdateSpeed)

    for _, channel := range engine.Players {
        channel.Pause()
        channel.Close()
    }

    engine.Players = nil

    for _, channel := range player.GetChannelReaders() {
        playChannel, err := engine.AudioContext.NewPlayerF32(channel)
        if err != nil {
            log.Printf("Could not create audio player for channel: %v", err)
            continue
        }
        playChannel.SetBufferSize(time.Second / 20)
        playChannel.SetVolume(engine.volume)
        engine.Players = append(engine.Players, playChannel)
        // playChannel.Play()
    }

    engine.Start = sync.Once{}
    engine.Player = player

}

func (engine *Engine) SetVolume(volume float64) {
    engine.volume = volume
    for _, player := range engine.Players {
        player.SetVolume(volume)
    }
}

func (engine *Engine) LoadDroppedFiles() {
    dropped := ebiten.DroppedFiles()
    if dropped == nil {
        return
    }
    entries, err := fs.ReadDir(dropped, ".")
    if err == nil {
        for _, file := range entries {
            if file.IsDir() {
                continue
            }
            path := file.Name()
            log.Printf("Loading dropped file: %v", path)
            engine.LoadSongFromFilesystem(dropped, path)
            break
        }
    } else {
        log.Printf("Error listing files: %v", err)
    }
}

func (engine *Engine) DoPause() {
    engine.Paused = !engine.Paused
    for _, player := range engine.Players {
        if engine.Paused {
            player.Pause()
        } else {
            player.Play()
        }
    }
}

func (engine *Engine) Update() error {
    engine.updates += 1

    engine.LoadDroppedFiles()

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
            case ebiten.KeyL:
                if engine.UIHooks.LoadSong != nil {
                    engine.UIHooks.LoadSong()
                }
            case ebiten.KeyP:
                if engine.UIHooks.Pause != nil {
                    engine.UIHooks.Pause()
                }
            case ebiten.KeyT:
                if engine.UIHooks.ToggleOscilloscopes != nil {
                    engine.UIHooks.ToggleOscilloscopes()
                }
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
        if !engine.Paused {

            for range 60 / engine.fps {
                engine.Player.Update(1.0/60)
            }
        }
    }

    engine.UI.Update()

    return nil
}

func (engine *Engine) Draw(screen *ebiten.Image) {
    if engine.UIHooks.RenderScopes != nil {
        engine.UIHooks.RenderScopes()
    }

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

    var player TrackerPlayer = &DummyPlayer{}

    sampleRate := 44100

    if len(flag.Args()) > 0 {
        path := flag.Args()[0]

        s3mFile, err := tryLoadS3m(path)
        if err != nil {
            log.Printf("Unable to load s3m: %v", err)

            modFile, err := tryLoadMod(path)
            if err != nil {
                log.Printf("Error loading %v: %v", path, err)
                return
            } else {
                player = mod.MakePlayer(modFile, sampleRate)
                log.Printf("Successfully loaded %v", path)
                log.Printf("Mod name: '%v'", modFile.Name)
            }
        } else {
            player = s3m.MakePlayer(s3mFile, sampleRate)
        }
    } else {
        /*
        dataFile, name, err := data.FindMod()
        if err != nil {
            log.Printf("Error finding mod file: %v", err)
            return
        }

        modFile, err := mod.Load(dataFile)
        if err != nil {
            log.Printf("Error loading mod file: %v", err)
            return
        } else {
            log.Printf("Successfully loaded %v", name)
            log.Printf("Mod name: '%v'", modFile.Name)
            player = mod.MakePlayer(modFile, sampleRate)
        }
        dataFile.Close()
        */
    }

    if *wav != "" {
        log.Printf("Rendering to %v", *wav)

        err := saveToWav(*wav, player.RenderToPCM(), sampleRate)
        if err != nil {
            log.Printf("Error saving to wav: %v", err)
            return
        }
    } else {

        fps := 30

        ebiten.SetTPS(fps)
        ebiten.SetWindowSize(1200, 800)
        ebiten.SetWindowTitle("Mod Tracker")
        ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

        audioContext := audio.NewContext(sampleRate)

        /*
        modPlayer.CurrentOrder = 0x10
        modPlayer.Channels[0].Mute = true
        modPlayer.Channels[1].Mute = true
        modPlayer.Channels[3].Mute = true
        */

        engine, err := MakeEngine(player, audioContext, fps)
        if err != nil {
            log.Printf("Error creating engine: %v", err)
            return
        }

        if len(flag.Args()) == 0 {
            engine.LoadSongFromFilesystem(data.Data, "data/strshine.s3m")
        }

        err = ebiten.RunGame(engine)
        if err != nil {
            log.Printf("Error: %v", err)
        }
    }

    log.Printf("Finished")
}
