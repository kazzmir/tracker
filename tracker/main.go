package main

import (
    "os"
    "log"
    "fmt"
    "time"
    "io"
    "bytes"
    _ "embed"
    // for discard
    // "io/ioutil"
    "flag"
    "runtime/pprof"
    "encoding/binary"
    "image/color"

    "github.com/kazzmir/tracker/mod"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
    "github.com/hajimehoshi/ebiten/v2/audio"
    "github.com/hajimehoshi/ebiten/v2/text/v2"

    "github.com/ebitenui/ebitenui"
    "github.com/ebitenui/ebitenui/widget"
    ui_image "github.com/ebitenui/ebitenui/image"
)

//go:embed futura.ttf
var FuturaTTF []byte

type Engine struct {
    Player *mod.Player
    AudioContext *audio.Context
    UI *ebitenui.UI
}

func loadFont(size float64) (text.Face, error) {
    source, err := text.NewGoTextFaceSource(bytes.NewReader(FuturaTTF))
    if err != nil {
        return nil, err
    }

    return &text.GoTextFace{
        Source: source,
        Size: size,
    }, nil
}

func makeUI(engine *Engine) *ebitenui.UI {
    face, _ := loadFont(19)

    rootContainer := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
        )),
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255})),
    )

    rootContainer.AddChild(widget.NewContainer(
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 128, G: 128, B: 128, A: 255})),
        widget.ContainerOpts.WidgetOpts(
            widget.WidgetOpts.MinSize(1000, 100),
        ),
    ))

    channels := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionHorizontal),
            widget.RowLayoutOpts.Spacing(2),
        )),
    )

    rows := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
        )),
    )

    for i := range 64 {
        textColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
        if (i + 1) % 4 == 0 {
            textColor = color.RGBA{R: 200, G: 200, B: 0, A: 255}
        }
        rows.AddChild(widget.NewText(
            widget.TextOpts.Text(fmt.Sprintf("%02X", i), face, textColor),
        ))
    }

    channels.AddChild(rows)

    for i := range engine.Player.Channels {
        background := color.NRGBA{R: 64, G: 64, B: 64, A: 255}
        if i % 2 == 0 {
            background = color.NRGBA{R: 96, G: 96, B: 96, A: 255}
        }

        channel := widget.NewContainer(
            widget.ContainerOpts.Layout(widget.NewRowLayout(
                widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                widget.RowLayoutOpts.Spacing(2),
            )),
            widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(background)),
        )

        channel.AddChild(widget.NewText(
            widget.TextOpts.Text(fmt.Sprintf("Channel %d", i+1), face, color.White),
        ))

        channels.AddChild(channel)
    }

    rootContainer.AddChild(channels)

    ui := ebitenui.UI{
        Container: rootContainer,
    }

    return &ui
}

func MakeEngine(modPlayer *mod.Player, audioContext *audio.Context) (*Engine, error) {

    engine := &Engine{
        Player: modPlayer,
        AudioContext: audioContext,
    }

    engine.UI = makeUI(engine)

    for _, channel := range modPlayer.Channels {
        playChannel, err := audioContext.NewPlayerF32(channel)
        if err != nil {
            return nil, err
        }
        playChannel.SetBufferSize(time.Second / 8)
        playChannel.SetVolume(0.3)
        playChannel.Play()
    }

    return engine, nil
}

func (engine *Engine) Update() error {

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
            case ebiten.KeySpace:
                engine.Player.CurrentRow = 0
            case ebiten.KeyLeft:
                engine.Player.PreviousOrder()
                log.Printf("New order: %d", engine.Player.CurrentOrder)
            case ebiten.KeyRight:
                engine.Player.NextOrder()
                log.Printf("New order: %d", engine.Player.CurrentOrder)
        }
    }

    engine.Player.Update(1.0/60)
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

func main(){
    log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)

    profile := flag.Bool("profile", false, "Enable profiling")
    wav := flag.String("wav", "", "Output wav file")
    flag.Parse()

    if len(flag.Args()) == 0 {
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

    path := flag.Args()[0]
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

    sampleRate := 44100
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
        ebiten.SetWindowSize(640, 480)
        ebiten.SetWindowTitle("Mod Tracker")
        ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

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
