package s3m

import (
    "io"
    "log"
    "math"
    "runtime"

    "github.com/kazzmir/tracker/common"
)

// FIXME: what are the extra 4 values at the end of each octave for?
// C-0 is definitely the first element in row 0, and B-0 is the 12th element.
// C-1 is index 16, so between B-0 and C-1 are 4 values that are not used.
var Octaves []int = []int{
    27392,25856,24384,23040,21696,20480,19328,18240,17216,16256,15360,14496, 0, 0, 0, 0,
    13696,12928,12192,11520,10848,10240, 9664, 9120, 8608, 8128, 7680, 7248, 0, 0, 0, 0,
    6848, 6464, 6096, 5760, 5424, 5120, 4832, 4560, 4304, 4064, 3840, 3624, 0, 0, 0, 0,
    3424, 3232, 3048, 2880, 2712, 2560, 2416, 2280, 2152, 2032, 1920, 1812, 0, 0, 0, 0,
    1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016,  960,  906, 0, 0, 0, 0,
    856,  808,  762,  720,  678,  640,  604,  570,  538,  508,  480,  453, 0, 0, 0, 0,
    428,  404,  381,  360,  339,  320,  302,  285,  269,  254,  240,  226, 0, 0, 0, 0,
    214,  202,  190,  180,  170,  160,  151,  143,  135,  127,  120,  113, 0, 0, 0, 0,
    107,  101,   95,   90,   85,   80,   75,   71,   67,   63,   60,   56, 0, 0, 0, 0,
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
    vibratoValue := int(float64(vibrato.Depth * 6) * math.Sin(float64(vibrato.position) * math.Pi * 360 / 64 / 180))
    return frequency + vibratoValue
}

type Channel struct {
    Player *Player
    AudioBuffer *common.AudioBuffer
    Channel int
    Volume float32
    buffer []float32 // used for reading audio data
    Mute bool

    CurrentPeriod int
    CurrentSample int
    CurrentVolume int
    CurrentEffect int
    EffectParameter int
    Vibrato Vibrato

    VolumeSlide uint8
    PortamentoToNote uint8
    PortamentoNote int

    currentRow int
    startPosition float32
}

func (channel *Channel) UpdateRow() {
    channel.currentRow = channel.Player.CurrentRow

    note := channel.Player.GetNote(channel.Channel, channel.currentRow)

    // log.Printf("Channel %v Play note %+v", channel.Channel, note)

    channel.CurrentEffect = EffectNone
    channel.EffectParameter = 0

    newPeriod := channel.CurrentPeriod

    // channel.CurrentVolume = 64
    if note.ChangeVolume {
        channel.CurrentVolume = note.Volume
    }

    if note.ChangeEffect {
        channel.CurrentEffect = int(note.EffectNumber)
        channel.EffectParameter = int(note.EffectParameter)
    }

    if note.ChangeNote {
        newPeriod = Octaves[note.Note]
        channel.startPosition = 0.0
    }

    if note.ChangeSample {
        channel.CurrentSample = note.SampleNumber - 1
    }

    switch channel.CurrentEffect {
        case EffectNone:
        case EffectSetSpeed:
            channel.Player.Speed = channel.EffectParameter
        case EffectSetTempo:
            channel.Player.BPM = channel.EffectParameter
        case EffectPortamentoToNote:
            channel.CurrentEffect = EffectPortamentoToNote
            if note.EffectParameter > 0 {
                channel.PortamentoToNote = note.EffectParameter
            }

            if note.ChangeNote {
                channel.PortamentoNote = Octaves[note.Note]
            }

            newPeriod = channel.CurrentPeriod
        case EffectPatternBreak:
            channel.Player.NextOrder()
            channel.Player.CurrentRow = channel.EffectParameter
        case EffectSampleOffset:
            channel.startPosition = float32(channel.EffectParameter) * 0x100
        case EffectPortamentoDown:
            channel.CurrentEffect = EffectPortamentoDown

            if note.EffectParameter > 0 {
                channel.PortamentoNote = int(note.EffectParameter)
            }

            if channel.PortamentoNote >> 4 == 0xf {
                newPeriod += int(channel.PortamentoNote & 0xf) * 4
                channel.CurrentEffect = EffectNone
            } else if channel.PortamentoNote >> 4 == 0xe {
                newPeriod += int(channel.PortamentoNote & 0xf)
            }

        case EffectVibrato:
            if note.EffectParameter > 0 {
                channel.Vibrato.Speed = int(note.EffectParameter >> 4)
                channel.Vibrato.Depth = int(note.EffectParameter & 0xf)
            }
            channel.CurrentEffect = EffectVibrato
        case EffectVibratoAndVolumeSlide:
            if note.EffectParameter > 0 {
                channel.VolumeSlide = note.EffectParameter
            }

            channel.CurrentEffect = EffectVibratoAndVolumeSlide
        case EffectGlobalVolume:
            channel.Player.GlobalVolume = uint8(channel.EffectParameter & 0x3f)
            log.Printf("Set global volume to %v", channel.Player.GlobalVolume)
        case EffectVolumeSlide:
            channel.CurrentEffect = EffectVolumeSlide

            if note.EffectParameter > 0 {
                channel.VolumeSlide = note.EffectParameter
            }
        default:
            log.Printf("Channel %v unknown effect %v with parameter %v", channel.Channel, channel.CurrentEffect, channel.EffectParameter)
    }

    channel.CurrentPeriod = newPeriod
}

func (channel *Channel) doVolumeSlide(changeRow bool) {
    volumeAmount := 0

    slideUp := int(channel.VolumeSlide >> 4)
    slideDown := int(channel.VolumeSlide & 0xf)

    if slideUp == 0xf {
        volumeAmount = -slideDown
        if slideDown == 0xf {
            volumeAmount = slideUp
        }
    } else if slideDown == 0xf {
        volumeAmount = slideUp
    } else if slideUp > 0 {
        volumeAmount = slideUp * (channel.Player.Speed - 1)
    } else if slideDown > 0 {
        volumeAmount = -slideDown * (channel.Player.Speed - 1)
    }

    channel.CurrentVolume += volumeAmount
    if channel.CurrentVolume < 0 {
        channel.CurrentVolume = 0
    }
    if channel.CurrentVolume > 64 {
        channel.CurrentVolume = 64
    }
}

func (channel *Channel) doPortamentoToNote(ticks int) {
    // FIXME: the rate of portamento is not correct. The docs say portamento*4, but that is too slow

    // log.Printf("portamento from %v to %v by %v", channel.CurrentPeriod, channel.PortamentoNote, int(channel.PortamentoToNote) * 4 * 2)
    if channel.CurrentPeriod < channel.PortamentoNote {
        channel.CurrentPeriod += int(channel.PortamentoToNote) * ticks * 4 * 2
        if channel.CurrentPeriod > channel.PortamentoNote {
            channel.CurrentPeriod = channel.PortamentoNote
        }
    } else if channel.CurrentPeriod > channel.PortamentoNote {
        channel.CurrentPeriod -= int(channel.PortamentoToNote) * ticks * 4 * 2
        if channel.CurrentPeriod < channel.PortamentoNote {
            channel.CurrentPeriod = channel.PortamentoNote
        }
    }
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
    switch channel.CurrentEffect {
        case EffectVolumeSlide:
            channel.doVolumeSlide(changeRow)
        case EffectVibratoAndVolumeSlide:
            channel.doVolumeSlide(changeRow)
            channel.Vibrato.Update()
        case EffectVibrato:
            channel.Vibrato.Update()
        case EffectPortamentoToNote:
            if !changeRow {
                channel.doPortamentoToNote(ticks)
            }
        case EffectPortamentoDown:
            if !changeRow {
                channel.CurrentPeriod += int(channel.PortamentoNote) * ticks * 4
            }
    }
}

func (channel *Channel) Update(rate float32) {

    samples := int(float32(channel.Player.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()

    // if channel.CurrentNote != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentFrequency > 0 && channel.Delay <= 0 {
    if channel.CurrentSample > 0 && channel.CurrentPeriod > 0 {
        instrument := channel.Player.GetInstrument(channel.CurrentSample)
        if instrument.MiddleC > 0 {

            period := 8363 * channel.CurrentPeriod / int(instrument.MiddleC)

            if channel.CurrentEffect == EffectVibrato || channel.CurrentEffect == EffectVibratoAndVolumeSlide {
                period = channel.Vibrato.Apply(period)
            }

            frequency := 14317056 / float32(period)
            // frequency := amigaFrequency / float32(period * 2)

            // ???
            // frequency /= 2

            // log.Printf("Note %v Octave %v Frequency %v MiddleC %v", channel.CurrentNote.Note, Octaves[channel.CurrentNote.Note], frequency, instrument.MiddleC)


            incrementRate := frequency / float32(channel.Player.SampleRate)

            noteVolume := float32(channel.CurrentVolume) / 64
            // log.Printf("note volume %v", noteVolume)

            // log.Printf("Write sample %v at %v/%v samples %v rate %v", channel.CurrentSample.Name, channel.startPosition, len(channel.CurrentSample.Data), samples, incrementRate)

            if incrementRate > 0 {
                for range samples {
                    position := int(channel.startPosition)
                    /*
                    if position >= len(channel.CurrentSample.Data) {
                        break
                    }
                    */
                    if position >= len(instrument.Data) || (instrument.Loop && position >= instrument.LoopEnd) {
                        // log.Printf("Position %v loop begin %v loop end %v", position, instrument.LoopBegin, instrument.LoopEnd)
                        if instrument.Loop && position >= instrument.LoopEnd {
                            channel.startPosition = float32(instrument.LoopBegin)
                            position = int(channel.startPosition)
                        } else {
                            break
                        }
                    }

                    // noteVolume = 1

                    channel.AudioBuffer.UnsafeWrite(instrument.Data[position] * channel.Volume * noteVolume * float32(channel.Player.GlobalVolume) / 64)
                    channel.startPosition += incrementRate
                    samplesWritten += 1
                }
            }
        }
    }

    for range (samples - samplesWritten) {
        channel.AudioBuffer.UnsafeWrite(0.0)
    }

    channel.AudioBuffer.Unlock()

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

    // in a browser we have to return something, so we generate some silence
    if i == 0 && runtime.GOOS == "js" {
        for i < 8 {
            data[i] = 0
            i += 1
        }
        return 8, nil
    } else {
        // on a normal os we can just return 0 if necessary
        return floatSamples * 8, nil
    }
}

type Player struct {
    Channels []*Channel
    S3M *S3MFile
    SampleRate int

    Speed int
    BPM int

    GlobalVolume uint8
    CurrentRow int
    CurrentOrder int
    OrdersPlayed int
    ticks float32
}

func MakePlayer(file *S3MFile, sampleRate int) *Player {
    channels := make([]*Channel, len(file.ChannelMap))

    player := &Player{
        S3M: file,
        Speed: int(file.InitialSpeed),
        BPM: int(file.InitialTempo),
        SampleRate: sampleRate,
        GlobalVolume: file.GlobalVolume,
    }

    // player.BPM = 40

    for channelNum, index := range file.ChannelMap {
        channels[index] = &Channel{
            Channel: channelNum,
            Player: player,
            AudioBuffer: common.MakeAudioBuffer(sampleRate),
            Volume: 1.0,
            buffer: make([]float32, sampleRate),
            currentRow: -1,
        }
    }

    for i, channel := range channels {
        if channel == nil {
            log.Printf("Did not create a channel %v", i)
        }
    }

    player.Channels = channels[:]

    /*
    player.S3M.Orders = []byte{21}
    player.S3M.SongLength = 1
    */

    // player.S3M.SongLength = 1

    return player
}

func (player *Player) GetPattern() int {
    return int(player.S3M.Orders[player.CurrentOrder])
}

func (player *Player) GetNote(channel int, row int) *Note {
    if player.GetPattern() >= len(player.S3M.Patterns) {
        return &Note{}
    }

    pattern := &player.S3M.Patterns[player.GetPattern()]
    return &pattern.Rows[row][player.S3M.ChannelMap[channel]]
}

func (player *Player) GetInstrument(index int) *Instrument {
    if index < 0 || index >= len(player.S3M.Instruments) {
        return nil
    }
    return &player.S3M.Instruments[index]
}

func (player *Player) Update(timeDelta float32) {
    // oldRow := player.CurrentRow
    oldTicks := int(player.ticks)

    if player.CurrentRow < 0 {
        player.CurrentRow = 0
    }

    player.ticks += timeDelta * float32(player.BPM) * 2 / 5
    newTicks := int(player.ticks)

    if player.ticks >= float32(player.Speed) {
        player.CurrentRow += 1
        // log.Printf("Row: %v", player.CurrentRow)
        player.ticks -= float32(player.Speed)
    }

    if player.CurrentRow > len(player.S3M.Patterns[0].Rows) - 1 {
        // player.rowPosition = 0
        player.CurrentRow = 0
        player.CurrentOrder += 1
        player.OrdersPlayed += 1
        if player.CurrentOrder >= player.S3M.SongLength {
            player.CurrentOrder = 0
        }

        /*
        if player.OnChangeOrder != nil {
            player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
        }
        */

        log.Printf("order %v next pattern: %v", player.CurrentOrder, player.GetPattern())
    }

    /*
    if oldRow != player.CurrentRow {
        if player.OnChangeRow != nil {
            player.OnChangeRow(player.CurrentRow)
        }
    }
    */

    for _, channel := range player.Channels {
        changeRow := false
        if player.CurrentRow != channel.currentRow {
            channel.UpdateRow()
            changeRow = true
        }

        if newTicks != oldTicks {
            channel.UpdateTick(changeRow, newTicks - oldTicks)
        }

        channel.Update(timeDelta)
    }

    /*
    // FIXME: we could possibly just call this when the speed/bpm changes
    if player.OnChangeSpeed != nil {
        player.OnChangeSpeed(player.Speed, player.BPM)
    }
    */
}

func (player *Player) NextOrder() {
    player.CurrentOrder += 1
    if player.CurrentOrder >= player.S3M.SongLength {
        player.CurrentOrder = 0
    }
}

func (player *Player) PreviousOrder() {
    player.CurrentOrder -= 1
    if player.CurrentOrder < 0 {
        player.CurrentOrder = player.S3M.SongLength - 1
    }
}

func (player *Player) ResetRow() {
}

func (player *Player) GetCurrentOrder() int {
    return 0
}

func (player *Player) RenderToPCM() io.Reader {
    // make a buffer to hold 1/100th of a second of audio data, which is 4-bytes per sample
    // and 1 samples per channel
    rate := 100
    buffer := make([]float32, player.SampleRate / rate)
    mix := make([]float32, player.SampleRate * 2 / rate)

    fillMix := func() bool {
        if player.OrdersPlayed >= player.S3M.SongLength {
            return false
        }

        player.Update(1.0 / float32(rate))

        for i := range mix {
            mix[i] = 0
        }

        for _, channel := range player.Channels {
            amount := channel.AudioBuffer.Read(buffer)

            // log.Printf("Channel %v produced %v samples", chNumber, amount)

            if amount > 0 {
                // copy the samples into the mix buffer
                normalizer := float32(len(player.Channels))
                for i := range amount {
                    // mono to stereo
                    mixed := mix[i*2+0] + buffer[i] / normalizer
                    mix[i*2+0] = mixed
                    mix[i*2+1] = mixed
                }
            }
        }

        for i := range mix {
            mix[i] = max(min(mix[i], 1), -1)
            // mix[i] = float32(math.Tanh(float64(mix[i])))
        }

        return true
    }

    mixPosition := len(mix)
    reader := func(data []byte) (int, error) {
        if len(data) == 0 {
            return 0, nil
        }

        if player.OrdersPlayed >= player.S3M.SongLength {
            return 0, io.EOF
        }

        // wait for the music to be produced
        if mixPosition < len(mix) {
            part := mix[mixPosition:]

            // log.Printf("Partial Copying %v bytes of audio data to %v", (len(mix) - mixPosition) * 4, len(data))

            amount := common.CopyFloat32(data, part)

            /*
            amount := min(len(data), len(part))
            copy(data, part[:amount])
            */
            mixPosition += amount
            return amount * 4, nil
        }

        mixPosition = 0

        more := fillMix()
        if !more {
            return 0, io.EOF
        }

        // copy the mix into the data buffer
        // log.Printf("Copying %v bytes of audio data to %v", (len(mix) - mixPosition) * 4, len(data))
        amount := common.CopyFloat32(data, mix)
        mixPosition += amount

        return amount * 4, nil
    }

    return &common.ReaderFunc{
        Func: reader,
    }
}
