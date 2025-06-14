package mod

import (
    "log"
    "sync"
    "math"
    "io"
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

func (buffer *AudioBuffer) Clear() {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    buffer.start = 0
    buffer.end = 0
    buffer.count = 0
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
        // log.Printf("overflow in audio buffer, dropping sample %v", value)
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

type Vibrato struct {
    Speed int
    Depth int
    position int
}

func (vibrato *Vibrato) Update() {
    vibrato.position += vibrato.Speed
    if vibrato.position >= 64 {
        vibrato.position -= 64
    }
}

func (vibrato *Vibrato) Apply(frequency int) int {
    if vibrato.Depth <= 0 || vibrato.Speed <= 0 {
        return frequency
    }

    // Amiga vibrato is a sine wave with a period of 64
    // and a depth of 8, so we scale the position to that range
    vibratoValue := int(float64(vibrato.Depth * 2) * math.Sin(float64(vibrato.position) * math.Pi * 360 / 64 / 180))
    return frequency + vibratoValue
}

type Channel struct {
    Player *Player
    AudioBuffer *AudioBuffer
    ChannelNumber int

    Vibrato Vibrato
    TonePortamentoTarget int
    TonePortamentoSpeed int

    Volume float32

    Mute bool

    buffer []float32

    CurrentSample *Sample
    CurrentFrequency int

    CurrentEffect int
    CurrentEffectParameter int

    // CurrentNote *Note
    currentRow int
    // endPosition int
    startPosition float32
}

func (channel *Channel) Read(data []byte) (int, error) {

    if channel.Mute {
        for i := 0; i < len(data); i++ {
            data[i] = 0
        }
        channel.AudioBuffer.Clear()
        return len(data), nil
    }

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

func computeAmigaFrequency(frequency int) float32 {
    return 7159090.5 / float32(frequency * 2)
}

func (channel *Channel) UpdateVolume() {
    up := channel.CurrentEffectParameter >> 4
    down := channel.CurrentEffectParameter & 0xf

    if up > 0 {
        channel.Volume = min(channel.Volume + float32(up) / 64.0, 1.0)
    } else if down > 0 {
        channel.Volume = max(channel.Volume - float32(down) / 64.0, 0.0)
    }
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
    switch channel.CurrentEffect {
        case EffectPortamentoUp:
            if !changeRow {
                channel.CurrentFrequency -= ticks * channel.CurrentEffectParameter
                channel.CurrentFrequency = max(channel.CurrentFrequency, 1)
            }
        case EffectPortamentoDown:
            if !changeRow {
                channel.CurrentFrequency += ticks * channel.CurrentEffectParameter
                channel.CurrentFrequency = min(channel.CurrentFrequency, 2000)
            }
        case EffectVibratoAndVolumeSlide:
            if !changeRow {
                channel.Vibrato.Update()
                channel.UpdateVolume()
            }
        case EffectVolumeSlide:
            channel.UpdateVolume()
        case EffectTonePortamento:
            if !changeRow {
                direction := 1
                // log.Printf("channel %v Portamento target %v current %v", channel.ChannelNumber, channel.TonePortamentoTarget, channel.CurrentFrequency)
                if channel.TonePortamentoTarget < channel.CurrentFrequency {
                    direction = -1
                }
                channel.CurrentFrequency += ticks * channel.TonePortamentoSpeed * direction
                if direction == -1 && channel.CurrentFrequency < channel.TonePortamentoTarget {
                    channel.CurrentFrequency = channel.TonePortamentoTarget
                } else if direction == 1 && channel.CurrentFrequency > channel.TonePortamentoTarget {
                    channel.CurrentFrequency = channel.TonePortamentoTarget
                }
            }
        case EffectVibrato:
            if !changeRow {
                channel.Vibrato.Update()
            }
            /*
            speed := channel.CurrentEffectParameter >> 4
            depth := channel.CurrentEffectParameter & 0xf
            */
    }
}

func (channel *Channel) UpdateRow() {
    channel.CurrentEffect = 0
    channel.CurrentEffectParameter = 0
    // FIXME: the default waveform is sine retrig, which should reset the position on each row
    // but most players don't seem to do this, instead just letting the position be whatever it was
    // on the last row
    // channel.Vibrato.position = 0

    note, row := channel.Player.GetNote(channel.ChannelNumber)
    if note.SampleNumber != 0 {
        log.Printf("Channel %v playing note %v", channel.ChannelNumber, note)
    }

    newFrequency := channel.CurrentFrequency

    if note.PeriodFrequency != 0 {
        newFrequency = int(note.PeriodFrequency)
    }

    // var sample *mod.Sample

    // log.Printf("new row %v", row)
    channel.currentRow = row
    if note.SampleNumber != 0 {
        channel.CurrentSample = channel.Player.GetSample(note.SampleNumber-1)
        // channel.CurrentNote = note
        channel.startPosition = 0
        channel.Volume = 1.0
    }

    switch note.EffectNumber {
        case EffectSetVolume:
            volume := min(note.EffectParameter, 64)
            channel.Volume = float32(volume) / 64.0
        case EffectSetSpeed:
            if note.EffectParameter >= 0 && note.EffectParameter <= 0x1f {
                channel.Player.Speed = int(note.EffectParameter)
            } else if note.EffectParameter >= 0x20 && note.EffectParameter <= 0xff {
                channel.Player.BPM = int(note.EffectParameter)
            }
        case EffectTonePortamento:
            channel.CurrentEffect = EffectTonePortamento
            if note.EffectParameter > 0 {
                channel.CurrentEffectParameter = int(note.EffectParameter)
                if note.PeriodFrequency != 0 {
                    channel.TonePortamentoTarget = int(note.PeriodFrequency)
                }
                channel.TonePortamentoSpeed = int(note.EffectParameter)
            }

            // log.Printf("channel %v row Portamento target %v current %v speed %v", channel.ChannelNumber, channel.TonePortamentoTarget, channel.CurrentFrequency, channel.TonePortamentoSpeed)

            newFrequency = channel.CurrentFrequency
        case EffectPortamentoUp:
            channel.CurrentEffect = EffectPortamentoUp
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectPortamentoDown:
            channel.CurrentEffect = EffectPortamentoDown
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectVolumeSlide:
            channel.CurrentEffect = EffectVolumeSlide
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectVibratoAndVolumeSlide:
            channel.CurrentEffect = EffectVibratoAndVolumeSlide
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectVibrato:
            channel.CurrentEffect = EffectVibrato
            channel.CurrentEffectParameter = int(note.EffectParameter)

            speed := note.EffectParameter >> 4
            depth := note.EffectParameter & 0xf

            if speed > 0 {
                channel.Vibrato.Speed = int(speed)
            }

            if depth > 0 {
                channel.Vibrato.Depth = int(depth)
            }
        case EffectExtra:
            switch note.EffectParameter >> 4 {
                // fine volume slide down
                case 0xb:
                    channel.Volume = max(channel.Volume - float32(note.EffectParameter & 0xf) / 64.0, 0.0)
                default:
                    log.Printf("Warning: channel %v unhandled extra effect %x with parameter %x", channel.ChannelNumber, note.EffectParameter >> 4, note.EffectParameter & 0xf)
            }
        default:
            if note.EffectNumber != 0 || note.EffectParameter != 0 {
                log.Printf("Warning: channel %v unhandled effect %x with parameter %v", channel.ChannelNumber, note.EffectNumber, note.EffectParameter)
            }
    }

    channel.CurrentFrequency = newFrequency
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

    samples := int(float32(channel.Player.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()

    if channel.CurrentSample != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentFrequency > 0 {
        frequency := channel.CurrentFrequency
        if channel.CurrentEffect == EffectVibrato {
            frequency = channel.Vibrato.Apply(frequency)
        }
        incrementRate := computeAmigaFrequency(frequency) / float32(channel.Player.SampleRate)

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

func MakeChannelVoice(channelNumber int, player *Player) *Channel {
    channel := &Channel{
        Player: player,
        ChannelNumber: channelNumber,
        AudioBuffer: MakeAudioBuffer(player.SampleRate),
        Volume: 1.0,
        buffer: make([]float32, player.SampleRate),
        // currentRow: -1,
    }

    return channel
}

type Player struct {
    ModFile *ModFile
    Channels []*Channel
    SampleRate int

    Speed int
    // beats per minute
    BPM int
    CurrentOrder int
    CurrentRow int

    // count of the orders played
    OrdersPlayed int

    ticks float32
    // rowPosition float32
}

func MakePlayer(modfile *ModFile, sampleRate int) *Player {
    player := &Player{
        ModFile: modfile,
        SampleRate: sampleRate,
        Speed: 6,
        BPM: 125,
        CurrentRow: -1,
        // CurrentOrder: 0xa,
    }

    for i := range modfile.Channels {
        player.Channels = append(player.Channels, MakeChannelVoice(i, player))
    }

    return player
}

func (player *Player) GetSample(sampleNumber byte) *Sample {
    if sampleNumber < 0 || int(sampleNumber) >= len(player.ModFile.Samples) {
        return nil
    }
    return &player.ModFile.Samples[sampleNumber]
}

func (player *Player) GetPattern() int {
    if player.CurrentOrder < 0 || player.CurrentOrder >= len(player.ModFile.Orders) {
        return 0
    }

    return int(player.ModFile.Orders[player.CurrentOrder])
}

func (player *Player) GetNote(channel int) (*Note, int) {
    pattern := player.GetPattern()
    row := &player.ModFile.Patterns[pattern].Rows[player.CurrentRow]

    if channel < len(row.Notes) {
        return &row.Notes[channel], player.CurrentRow
    } else {
        return &Note{}, player.CurrentRow
    }
}

func (player *Player) NextOrder() {
    player.CurrentOrder += 1
    if player.CurrentOrder >= player.ModFile.SongLength {
        player.CurrentOrder = 0
    }
    player.CurrentRow = 0
}

func (player *Player) PreviousOrder() {
    player.CurrentOrder -= 1
    if player.CurrentOrder < 0 {
        player.CurrentOrder = 0
    }
    player.CurrentRow = 0
}

func (player *Player) Update(timeDelta float32) {
    oldRow := player.CurrentRow
    oldTicks := int(player.ticks)

    if player.CurrentRow < 0 {
        player.CurrentRow = 0
    }

    player.ticks += timeDelta * float32(player.BPM) * 2 / 5
    newTicks := int(player.ticks)

    if player.ticks >= float32(player.Speed) {
        player.CurrentRow += 1
        player.ticks -= float32(player.Speed)
    }

    if player.CurrentRow > len(player.ModFile.Patterns[0].Rows) - 1 {
        // player.rowPosition = 0
        player.CurrentRow = 0
        player.CurrentOrder += 1
        player.OrdersPlayed += 1
        if player.CurrentOrder >= player.ModFile.SongLength {
            player.CurrentOrder = 0
        }

        log.Printf("order %v next pattern: %v", player.CurrentOrder, player.GetPattern())
    }

    for _, channel := range player.Channels {
        changeRow := false
        if oldRow != channel.currentRow {
            channel.UpdateRow()
            changeRow = true
        }

        if newTicks != oldTicks {
            channel.UpdateTick(changeRow, newTicks - oldTicks)
        }

        channel.Update(timeDelta)
    }
}

type ReaderFunc struct {
    Func func(data []byte) (int, error)
}

func (reader *ReaderFunc) Read(data []byte) (int, error) {
    if reader.Func == nil {
        return 0, io.EOF
    }
    return reader.Func(data)
}

func copyFloat32(dst []byte, src []float32) int {
    maxBytes := min(len(dst), len(src) * 4)

    for i := range src {
        if i * 4 >= maxBytes {
            return maxBytes
        }

        bits := math.Float32bits(src[i])
        dst[i*4+0] = byte(bits)
        dst[i*4+1] = byte(bits >> 8)
        dst[i*4+2] = byte(bits >> 16)
        dst[i*4+3] = byte(bits >> 24)
    }

    return maxBytes
}

// produce a PCM stream of stereo samples
func (player *Player) RenderToPCM() io.Reader {
    // make a buffer to hold 1/60th of a second of audio data, which is 4-bytes per sample
    // and 1 samples per channel
    buffer := make([]float32, player.SampleRate / 60)
    mix := make([]float32, player.SampleRate * 2 / 60)
    readMusic := make(chan bool)
    produceMusic := make(chan bool)

    go func(){
        for player.OrdersPlayed < player.ModFile.SongLength {
            <-produceMusic

            player.Update(1.0 / 60)

            for i := range mix {
                mix[i] = 0
            }

            for chNumber, channel := range player.Channels {
                amount := channel.AudioBuffer.Read(buffer)

                // log.Printf("Channel %v produced %v samples", chNumber, amount)

                if amount > 0 {
                    // copy the samples into the mix buffer
                    for i := range amount {
                        mix[i*2+0] += buffer[i]
                        mix[i*2+1] += buffer[i]
                    }
                }
            }

            readMusic <- true
        }
    }()

    mixPosition := len(mix)
    reader := func(data []byte) (int, error) {
        if len(data) == 0 {
            return 0, nil
        }

        if player.OrdersPlayed >= player.ModFile.SongLength {
            return 0, io.EOF
        }

        // wait for the music to be produced
        if mixPosition < len(mix) {
            part := mix[mixPosition:]

            amount := copyFloat32(data, part)

            /*
            amount := min(len(data), len(part))
            copy(data, part[:amount])
            */
            mixPosition += amount
            return amount * 4, nil
        }

        produceMusic <- true
        mixPosition = 0

        // wait for the music to be read
        <-readMusic

        // copy the mix into the data buffer
        amount := copyFloat32(data, mix)
        mixPosition += amount

        return amount * 4, nil
    }

    return &ReaderFunc{
        Func: reader,
    }
}
