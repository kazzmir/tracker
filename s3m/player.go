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

type Channel struct {
    Player *Player
    AudioBuffer *common.AudioBuffer
    Channel int
    Volume float32
    buffer []float32 // used for reading audio data
    Mute bool

    CurrentNote int
    CurrentSample int
    CurrentVolume int

    currentRow int
    startPosition float32
}

func (channel *Channel) UpdateRow() {
    channel.currentRow = channel.Player.CurrentRow

    note := channel.Player.GetNote(channel.Channel, channel.currentRow)

    channel.CurrentVolume = 64
    if note.Volume > 0 {
        channel.CurrentVolume = note.Volume
    }

    if note.HasNote {
        channel.CurrentNote = note.Note
        channel.startPosition = 0.0
    }

    if note.SampleNumber > 0 {
        channel.CurrentSample = note.SampleNumber - 1
    }
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
}

func (channel *Channel) Update(rate float32) {

    samples := int(float32(channel.Player.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()

    // if channel.CurrentNote != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentFrequency > 0 && channel.Delay <= 0 {
    if channel.CurrentSample > 0 {
        instrument := channel.Player.GetInstrument(channel.CurrentSample)
        period := 8363 * Octaves[channel.CurrentNote] / int(instrument.MiddleC)
        frequency := 14317056 / float32(period * 2)
        // frequency := amigaFrequency / float32(period * 2)

        // ???
        // frequency /= 2

        // log.Printf("Note %v Octave %v Frequency %v MiddleC %v", channel.CurrentNote.Note, Octaves[channel.CurrentNote.Note], frequency, instrument.MiddleC)

        /*
        if channel.CurrentEffect == EffectVibrato {
            frequency = channel.Vibrato.Apply(frequency)
        }
        */

        incrementRate := frequency / float32(channel.Player.SampleRate)

        // log.Printf("Write sample %v at %v/%v samples %v rate %v", channel.CurrentSample.Name, channel.startPosition, len(channel.CurrentSample.Data), samples, incrementRate)

        if incrementRate > 0 {
            for range samples {
                position := int(channel.startPosition)
                /*
                if position >= len(channel.CurrentSample.Data) {
                    break
                }
                */
                if position >= len(instrument.Data) || (instrument.LoopBegin > 1 && position >= instrument.LoopEnd) {
                    // log.Printf("Position %v loop begin %v loop end %v", position, instrument.LoopBegin, instrument.LoopEnd)
                    if instrument.LoopBegin > 1 {
                        channel.startPosition = float32(instrument.LoopBegin)
                        position = int(channel.startPosition)
                    } else {
                        break
                    }
                }

                noteVolume := float32(1.0)
                if channel.CurrentVolume > 0 {
                    noteVolume = float32(channel.CurrentVolume) / 64
                }

                // noteVolume = 1

                channel.AudioBuffer.UnsafeWrite(instrument.Data[position] * channel.Volume * noteVolume)
                channel.startPosition += incrementRate
                samplesWritten += 1
            }
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
    }

    // player.BPM = 15

    log.Printf("Channels %v", len(channels))
    for channelNum, index := range file.ChannelMap {
        log.Printf("Create channel %v", index)
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

    player.Channels = channels

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
}

func (player *Player) PreviousOrder() {
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
