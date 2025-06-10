package main

import (
    "os"
    "log"
    "time"
    "math"
    "sync"

    "github.com/kazzmir/tracker/mod"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
    "github.com/hajimehoshi/ebiten/v2/audio"
)

type AudioBuffer struct {
    // mono channel buffer of samples
    Buffer []float32
    lock sync.Mutex

    start int
    end int
    count int
}

func (buffer *AudioBuffer) Lock() {
    buffer.lock.Lock()
}

func (buffer *AudioBuffer) Unlock() {
    buffer.lock.Unlock()
}

func (buffer *AudioBuffer) Read(data []float32) int {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    total := 0

    if buffer.count == 0 {
        return total
    }

    for i := 0; i < len(data); i++ {
        if buffer.count == 0 {
            break
        }
        data[i] = buffer.Buffer[buffer.start]
        buffer.start = (buffer.start + 1) % len(buffer.Buffer)
        buffer.count -= 1
        total += 1
    }

    return total
}

func (buffer *AudioBuffer) UnsafeWrite(value float32) {
    if buffer.count < len(buffer.Buffer) {
        buffer.count += 1
        buffer.Buffer[buffer.end] = value
        buffer.end = (buffer.end + 1) % len(buffer.Buffer)
    } else {
        log.Printf("overflow in audio buffer, dropping sample %v", value)
    }
}

func (buffer *AudioBuffer) Write(data []float32, rate float32) {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    var index float32
    for int(index) < len(data) {
        value := data[int(index)]
        index += rate
        if buffer.count >= len(buffer.Buffer) {
            break
        }

        buffer.count += 1
        buffer.Buffer[buffer.end] = value
        buffer.end = (buffer.end + 1) % len(buffer.Buffer)
    }
}

func MakeAudioBuffer(sampleRate int) *AudioBuffer {
    return &AudioBuffer{
        // one full second worth of buffering
        Buffer: make([]float32, sampleRate),
    }
}

type Channel struct {
    Engine *Engine
    AudioBuffer *AudioBuffer
    ChannelNumber int

    Volume float32

    buffer []float32

    CurrentSample *mod.Sample
    CurrentNote *mod.Note
    currentRow int
    // endPosition int
    startPosition float32
}

func (channel *Channel) Read(data []byte) (int, error) {

    samples := len(data) / 4 / 2

    // sampleFrequency := 22050 / 2
    // samples = (samples * sampleFrequency) / channel.Engine.SampleRate

    // rate := float32(sampleFrequency) / float32(channel.Engine.SampleRate)

    // part := channel.buffer[:samples]
    part := channel.buffer[:samples]
    floatSamples := channel.AudioBuffer.Read(part)

    // log.Printf("Emit %v samples", floatSamples)

    i := 0
    for sampleIndex := range floatSamples {
        value := part[sampleIndex]
        bits := math.Float32bits(value)
        data[i*8+0] = byte(bits)
        data[i*8+1] = byte(bits >> 8)
        data[i*8+2] = byte(bits >> 16)
        data[i*8+3] = byte(bits >> 24)

        data[i*8+4] = byte(bits)
        data[i*8+5] = byte(bits >> 8)
        data[i*8+6] = byte(bits >> 16)
        data[i*8+7] = byte(bits >> 24)

        i += 1
    }

    i *= 8

    // log.Printf("Empty sample data %v / %v", len(data) - i, len(data))

    /*
    i := 0
    if channel.CurrentSample != nil {
        sample := channel.CurrentSample
        for samplePosition := channel.startPosition; samplePosition < channel.endPosition; samplePosition++ {
            value := (float32(sample.Data[samplePosition])) / 128
            bits := math.Float32bits(value)
            data[i*8+0] = byte(bits)
            data[i*8+1] = byte(bits >> 8)
            data[i*8+2] = byte(bits >> 16)
            data[i*8+3] = byte(bits >> 24)

            data[i*8+4] = byte(bits)
            data[i*8+5] = byte(bits >> 8)
            data[i*8+6] = byte(bits >> 16)
            data[i*8+7] = byte(bits >> 24)
        }
    }
    */

    return floatSamples * 4 * 2, nil

    /*
    for i < len(data) {
        data[i] = 0
        i += 1
    }

    return len(data), nil
    */
}

func (channel *Channel) UpdateRow() {
    note, row := channel.Engine.GetNote(channel.ChannelNumber)
    if note.SampleNumber != 0 {
        log.Printf("Channel %v playing note %v", channel.ChannelNumber, note)
    }

    // var sample *mod.Sample

    // log.Printf("new row %v", row)
    channel.currentRow = row
    if note.SampleNumber != 0 {
        channel.CurrentSample = channel.Engine.GetSample(note.SampleNumber-1)
        channel.CurrentNote = note
        channel.startPosition = 0
    }

    switch note.EffectNumber {
        case mod.EffectSetVolume:
            volume := min(note.EffectParameter, 64)
            channel.Volume = float32(volume) / 64.0
    }
}

func (channel *Channel) Update(rate float32) error {
    /*
    if note.SampleNumber > 0 {
        sample = channel.Engine.GetSample(note.SampleNumber-1)
    }
    */

    // assume C-4 is 400
    // noteRate := float32(note.PeriodFrequency) / 400.0
    // noteRate := 7159090.5 / (float32(note.PeriodFrequency) * 2)
    // noteRate := 261.63 / float32(note.PeriodFrequency)

    /*
    if sample != nil && sample != channel.CurrentSample {
        channel.CurrentSample = sample
        // channel.endPosition = 0
        channel.startPosition = 0
        log.Printf("Channel %v switched to sample %v", channel.ChannelNumber, sample.Name)
    } else if channel.CurrentSample != nil {
        // channel.startPosition = channel.endPosition
        // channel.endPosition += int(rate * float32(channel.Engine.SampleRate) * 4000 / noteRate)
        / *
        channel.endPosition += int(rate * noteRate)
        if channel.endPosition >= len(channel.CurrentSample.Data) {
            channel.endPosition = len(channel.CurrentSample.Data)
        }
        * /

        / *
        if channel.startPosition == channel.endPosition {
            channel.CurrentSample = nil
        }
        * /
    }
    */

    samples := int(float32(channel.Engine.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()

    if channel.CurrentSample != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentNote.PeriodFrequency > 0 {
        incrementRate := (7159090.5 / float32(channel.CurrentNote.PeriodFrequency * 2)) / float32(channel.Engine.SampleRate)

        // log.Printf("Write sample %v at %v/%v samples %v rate %v", channel.CurrentSample.Name, channel.startPosition, len(channel.CurrentSample.Data), samples, incrementRate)


        for range samples {
            position := int(channel.startPosition)
            if position < 0 {
                break
            }
            if position >= len(channel.CurrentSample.Data) || (channel.CurrentSample.LoopLength > 1 && position >= (channel.CurrentSample.LoopStart + channel.CurrentSample.LoopLength) * 2) {
                if channel.CurrentSample.LoopLength > 1 {
                    channel.startPosition = float32(channel.CurrentSample.LoopStart * 2)
                    position = int(channel.startPosition)
                } else {
                    break
                }
            }
            channel.AudioBuffer.UnsafeWrite(channel.CurrentSample.Data[position] * channel.Volume)
            channel.startPosition += incrementRate
            samplesWritten += 1
        }

        /*
        part := channel.CurrentSample.Data[channel.startPosition:channel.endPosition]
        if len(part) > 0 {
            // channel.AudioBuffer.Write(part, noteRate)
            // middle-C
            channel.AudioBuffer.Write(part, 261.63 / float32(note.PeriodFrequency))
        }
        */
    }

    for range (samples - samplesWritten) {
        channel.AudioBuffer.UnsafeWrite(0.0)
    }

    channel.AudioBuffer.Unlock()

    return nil
}

type Engine struct {
    ModFile *mod.ModFile
    SampleRate int
    AudioContext *audio.Context

    Speed int

    SampleIndex int
    Channels []*Channel

    CurrentRow int
    CurrentOrder int
    rowPosition float32
}

func MakeEngine(modFile *mod.ModFile, sampleRate int, audioContext *audio.Context) (*Engine, error) {

    engine := &Engine{
        ModFile: modFile,
        SampleRate: sampleRate,
        AudioContext: audioContext,
        Speed: 6,
        CurrentRow: -1,
        // CurrentOrder: 2,
    }

    for i := range modFile.Channels {
        /*
        if i > 0 {
            break
        }
        */

        if true || i == 3 {

            channel0 := engine.MakeChannelVoice(i)

            playChannel0, err := audioContext.NewPlayerF32(channel0)
            if err != nil {
                return nil, err
            }
            playChannel0.SetBufferSize(time.Second / 8)
            playChannel0.SetVolume(0.3)

            engine.Channels = append(engine.Channels, channel0)

            playChannel0.Play()
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

func (engine *Engine) MakeChannelVoice(channelNumber int) *Channel {
    channel := &Channel{
        Engine: engine,
        ChannelNumber: channelNumber,
        AudioBuffer: MakeAudioBuffer(engine.SampleRate),
        Volume: 1.0,
        buffer: make([]float32, engine.SampleRate),
        currentRow: -1,
    }
    return channel
}

func (engine *Engine) GetSample(sampleNumber byte) *mod.Sample {
    if sampleNumber < 0 || int(sampleNumber) >= len(engine.ModFile.Samples) {
        return nil
    }
    return &engine.ModFile.Samples[sampleNumber]
}

func (engine *Engine) GetPattern() int {
    if engine.CurrentOrder < 0 || engine.CurrentOrder >= len(engine.ModFile.Orders) {
        return 0
    }

    return int(engine.ModFile.Orders[engine.CurrentOrder])
}

func (engine *Engine) GetNote(channel int) (*mod.Note, int) {
    pattern := engine.GetPattern()
    row := &engine.ModFile.Patterns[pattern].Rows[engine.CurrentRow]

    if channel < len(row.Notes) {
        return &row.Notes[channel], engine.CurrentRow
    } else {
        return &mod.Note{}, engine.CurrentRow
    }
}

func (engine *Engine) Update() error {

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
        }
    }

    oldRow := engine.CurrentRow

    engine.rowPosition += float32(engine.Speed) * 1.0 / 60.0 * 2
    engine.CurrentRow = int(engine.rowPosition)
    if engine.CurrentRow > len(engine.ModFile.Patterns[0].Rows) - 1 {
        engine.rowPosition = 0
        engine.CurrentRow = 0
        engine.CurrentOrder += 1
        if engine.CurrentOrder >= engine.ModFile.SongLength {
            engine.CurrentOrder = 0
        }

        log.Printf("next pattern: %v", engine.GetPattern())
    }

    for _, channel := range engine.Channels {
        if oldRow != channel.currentRow {
            channel.UpdateRow()
        }

        channel.Update(1.0/60)
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
