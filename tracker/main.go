package main

import (
    "os"
    "log"
    "time"

    "github.com/kazzmir/tracker/mod"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
    "github.com/hajimehoshi/ebiten/v2/audio"
)

type Engine struct {
    Player *mod.Player
    AudioContext *audio.Context
}

func MakeEngine(modPlayer *mod.Player, audioContext *audio.Context) (*Engine, error) {

    engine := &Engine{
        Player: modPlayer,
        AudioContext: audioContext,
        // CurrentOrder: 2,
    }

    for i, channel := range modPlayer.Channels {
        /*
        if i > 0 {
            break
        }
        */

        if true || i == 3 {

            playChannel, err := audioContext.NewPlayerF32(channel)
            if err != nil {
                return nil, err
            }
            playChannel.SetBufferSize(time.Second / 8)
            playChannel.SetVolume(0.3)
            playChannel.Play()
        }

    }

    /*
    player, err := audioContext.NewPlayerF32(engine)
    if err != nil {
        return nil, err
    }
    player.SetBufferSize(time.Second / 2)
    player.Play()
    */

    return engine, nil
}

func (engine *Engine) Update() error {

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
        }
    }

    engine.Player.Update(1.0/60)

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

    /*
    for i := range modFile.Patterns[0].Rows {
        modFile.Patterns[0].Rows[i].Notes = []mod.Note{mod.Note{}, mod.Note{}}
    }

    modFile.Patterns[0].Rows[0].Notes = []mod.Note{mod.Note{}, mod.Note{SampleNumber: 0xd, PeriodFrequency: 400}}
    // modFile.Patterns[1].Rows[4].Notes = []mod.Note{mod.Note{SampleNumber: 0xd}}
    */

    ebiten.SetTPS(60)
    ebiten.SetWindowSize(640, 480)
    ebiten.SetWindowTitle("Mod Tracker")
    ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

    sampleRate := 44100
    audioContext := audio.NewContext(sampleRate)

    modPlayer := mod.MakePlayer(modFile, sampleRate)

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
