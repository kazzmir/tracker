package main

import (
    "os"
    "os/signal"
    "log"
    "time"
    "io"
    "sync"
    "bytes"
    "io/fs"
    // for discard
    // "io/ioutil"
    "flag"
    "context"
    "runtime/pprof"

    "github.com/kazzmir/tracker/mod"
    "github.com/kazzmir/tracker/s3m"
    "github.com/kazzmir/tracker/xm"
    "github.com/kazzmir/tracker/data"
    "github.com/kazzmir/tracker/common"
    tracker_lib "github.com/kazzmir/tracker/lib"

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

    quit context.Context
}

func MakeEngine(player TrackerPlayer, audioContext *audio.Context, fps int, quit context.Context) (*Engine, error) {
    engine := &Engine{
        AudioContext: audioContext,
        fps: fps,
        volume: 0.6,
        quit: quit,
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

        loaded, err := s3m.Load(bytes.NewReader(buffer.Bytes()), log.Default())
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

    loadXm := func() (TrackerPlayer, error) {
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

        loaded, err := xm.Load(bytes.NewReader(buffer.Bytes()), log.Default())
        if err != nil {
            return nil, err
        }

        return xm.MakePlayer(loaded, engine.AudioContext.SampleRate()), nil
    }

    player, err := loadS3m()
    if err != nil {
        player, err = loadXm()
        if err != nil {
            player, err = loadMod()
        }
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
    if engine.quit.Err() != nil {
        return ebiten.Termination
    }

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
            case ebiten.KeyTab:
                if engine.UIHooks.ToggleMainView != nil {
                    engine.UIHooks.ToggleMainView()
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

func tryLoadS3m(path string) (*s3m.S3MFile, error) {
    file, err := os.Open(path)
    if err != nil {
        return nil, err
    }
    defer file.Close()

    return s3m.Load(file, log.Default())
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

    return xm.Load(file, log.Default())
}

func TryLoad(path string, sampleRate int) (TrackerPlayer, error) {
    s3mFile, err := tryLoadS3m(path)
    if err == nil {
        return s3m.MakePlayer(s3mFile, sampleRate), nil
    }

    log.Printf("Unable to load s3m: %v", err)

    xmFile, err := tryLoadXM(path)
    if err == nil {
        return xm.MakePlayer(xmFile, sampleRate), nil
    }

    log.Printf("Unable to load xm: %v", err)

    modFile, err := tryLoadMod(path)
    if err != nil {
        return nil, err
    }
    return mod.MakePlayer(modFile, sampleRate), nil
}

func runGui(player TrackerPlayer, sampleRate int, quit context.Context) error {
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

    engine, err := MakeEngine(player, audioContext, fps, quit)
    if err != nil {
        return err
    }

    if len(flag.Args()) == 0 {
        engine.LoadSongFromFilesystem(data.Data, "data/strshine.s3m")
    }

    return ebiten.RunGame(engine)
}

func main(){
    log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)

    log.Printf("Running")

    profile := flag.Bool("profile", false, "Enable profiling")
    wav := flag.String("wav", "", "Output wav file")
    cli := flag.Bool("cli", false, "Run in CLI mode without GUI")
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

    var player TrackerPlayer = &common.DummyPlayer{}
    quit, cancel := context.WithCancel(context.Background())
    defer cancel()
    signalChan := make(chan os.Signal, 2)
    go func(){
        <-signalChan
        cancel()
    }()
    signal.Notify(signalChan, os.Interrupt)

    sampleRate := 44100

    if len(flag.Args()) > 0 {
        path := flag.Args()[0]

        var err error
        player, err = TryLoad(path, sampleRate)
        if err != nil {
            log.Printf("Error loading module: %v", err)
            return
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

        err := tracker_lib.SaveToWav(*wav, player.RenderToPCM(), sampleRate, log.Default())
        if err != nil {
            log.Printf("Error saving to wav: %v", err)
            return
        }
    } else if *cli {
        if len(flag.Args()) == 0 {
            log.Printf("Give a mod or s3m file to play in CLI mode")
            return
        }
        err := runCli(player, sampleRate, quit)
        if err != nil {
            log.Printf("Error: %v", err)
        }
    } else {
        err := runGui(player, sampleRate, quit)
        if err != nil {
            log.Printf("Error: %v", err)
        }
    }

    log.Printf("Finished")
}
