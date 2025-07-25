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

type Tremolo struct {
    Speed int
    Depth int
    position int
}

func (tremolo *Tremolo) Update() {
    tremolo.position += tremolo.Speed
    if tremolo.position >= 64 {
        tremolo.position -= 64
    }
}

func (tremolo *Tremolo) Apply(volume float32) float32 {
    volumeValue := float64(tremolo.Depth) / 16 * math.Sin(float64(tremolo.position) * math.Pi * 360 / 64 / 180)
    return volume + float32(volumeValue)
}

type Channel struct {
    Player *Player
    AudioBuffer *common.AudioBuffer
    ScopeBuffer *common.AudioBuffer
    Channel int
    Volume float32
    buffer []float32 // used for reading audio data
    Mute bool

    Pan int // 0-15, 8 is center, 0 is left, 15 is right

    CurrentPeriod int
    CurrentSample int
    CurrentVolume int
    CurrentEffect int
    EffectParameter int
    Vibrato Vibrato
    NoteDelay int
    UpdateDelay func()

    Tremolo Tremolo

    // replay the note every n ticks
    Retrigger int

    VolumeSlide uint8
    PortamentoToNote uint8
    PortamentoNote int

    currentRow int
    startPosition float32
}

func (channel *Channel) GetLeftPan() float32 {
    // 0 is full pan left, so return 1.0
    // 8 is center, so return 0.5
    // 0xf is full pan right, so return 0.0

    return float32(0xf - channel.Pan) / 15
}

func (channel *Channel) GetRightPan() float32 {
    return float32(channel.Pan) / 15
}

func (channel *Channel) UpdateRow() {
    channel.currentRow = channel.Player.CurrentRow

    note := channel.Player.GetRowNote(channel.Channel, channel.currentRow)

    // log.Printf("Channel %v row %v Play note %+v", channel.Channel, channel.currentRow, note)

    channel.CurrentEffect = EffectNone
    channel.EffectParameter = 0

    newPeriod := channel.CurrentPeriod
    newVolume := channel.CurrentVolume
    newSample := channel.CurrentSample
    newStartPosition := channel.startPosition

    // channel.CurrentVolume = 64
    if note.ChangeVolume {
        newVolume = note.Volume
    }

    if note.ChangeEffect {
        channel.CurrentEffect = int(note.EffectNumber)
        channel.EffectParameter = int(note.EffectParameter)
    }

    if note.ChangeNote {
        if note.Note == 255 || note.Note == 254 {
            // log.Printf("channel %v note %v", channel.Channel, note.Note)
            newSample = -1
        } else {
            newPeriod = Octaves[note.Note]
            newStartPosition = 0.0
        }
    }

    if note.ChangeSample {
        newSample = note.SampleNumber - 1
    }

    switch channel.CurrentEffect {
        case EffectNone:
        case EffectSetSpeed:
            channel.Player.Speed = channel.EffectParameter
            if channel.Player.OnChangeSpeed != nil {
                channel.Player.OnChangeSpeed(channel.Player.Speed, channel.Player.BPM)
            }
        case EffectSetTempo:
            channel.Player.BPM = channel.EffectParameter
            if channel.Player.BPM < 32 {
                channel.Player.BPM = 32
            }
        case EffectPatternJump:
            channel.Player.DoJump = true
            channel.Player.JumpOrder = channel.EffectParameter & 0x7f
            if channel.Player.JumpOrder >= len(channel.Player.S3M.Orders) {
                channel.Player.JumpOrder = 0
            }
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
            channel.Player.DoBreak = true
            channel.Player.BreakRow = channel.EffectParameter & 0x7f
        case EffectSampleOffset:
            channel.startPosition = float32(channel.EffectParameter) * 0x100
        case EffectRetriggerAndVolumeSlide:
            channel.CurrentEffect = EffectRetriggerAndVolumeSlide
            channel.VolumeSlide = note.EffectParameter >> 4
            channel.Retrigger = int(note.EffectParameter & 0xf)
        case EffectPortamentoAndVolumeSlide:
            channel.CurrentEffect = EffectPortamentoAndVolumeSlide
            if channel.EffectParameter > 0 {
                channel.VolumeSlide = note.EffectParameter >> 4
            }
        case EffectPortamentoDown, EffectPortamentoUp:
            // channel.CurrentEffect = EffectPortamentoDown

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
        case EffectTremolo:
            if note.EffectParameter > 0 {
                channel.Tremolo.Speed = int(note.EffectParameter >> 4)
                channel.Tremolo.Depth = int(note.EffectParameter & 0xf)
            }
        case EffectGlobalVolume:
            channel.Player.GlobalVolume = uint8(channel.EffectParameter & 0x3f)
            log.Printf("Set global volume to %v", channel.Player.GlobalVolume)
        case EffectSetExtra:
            kind := channel.EffectParameter >> 4
            switch kind {
                case 0x8:
                    channel.Pan = channel.EffectParameter & 0xf
                case 0xa:
                    // legacy pan, SAx but some songs use it
                    pan := channel.EffectParameter & 0xf
                    if pan >= 8 {
                        pan -= 8
                    } else {
                        pan += 8
                    }

                    channel.Pan = pan
                case 0xd:
                    // note delay

                    channel.CurrentEffect = EffectNoteDelay
                    channel.NoteDelay = channel.EffectParameter & 0xf
                    delayVolume := newVolume
                    delayPeriod := newPeriod
                    delaySample := newSample
                    delayStartPosition := 0.0
                    channel.UpdateDelay = func() {
                        channel.CurrentVolume = delayVolume
                        channel.CurrentPeriod = delayPeriod
                        channel.CurrentSample = delaySample
                        channel.startPosition = float32(delayStartPosition)
                    }

                    newVolume = channel.CurrentVolume
                    newPeriod = channel.CurrentPeriod
                    newSample = channel.CurrentSample
                    newStartPosition = channel.startPosition

                default:
                    log.Printf("Unknown extra effect %v with parameter %v", kind, channel.EffectParameter)
            }
        case EffectVolumeSlide:
            channel.CurrentEffect = EffectVolumeSlide

            if note.EffectParameter > 0 {
                channel.VolumeSlide = note.EffectParameter
            }
        default:
            log.Printf("Channel %v unknown effect %v with parameter %v", channel.Channel, channel.CurrentEffect, channel.EffectParameter)
    }

    channel.CurrentVolume = newVolume
    channel.CurrentPeriod = newPeriod
    channel.CurrentSample = newSample
    channel.startPosition = newStartPosition
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
        if slideUp > 0 {
            volumeAmount = slideUp
        } else {
            volumeAmount = -slideDown
        }
    } else if slideUp > 0 {
        // FIXME: implement fast volume slides
        // volumeAmount = slideUp * (channel.Player.Speed - 1)
        volumeAmount = slideUp
    } else if slideDown > 0 {
        // volumeAmount = -slideDown * (channel.Player.Speed - 1)
        volumeAmount = -slideDown
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
        channel.CurrentPeriod += int(channel.PortamentoToNote) * ticks * 4
        if channel.CurrentPeriod > channel.PortamentoNote {
            channel.CurrentPeriod = channel.PortamentoNote
        }
    } else if channel.CurrentPeriod > channel.PortamentoNote {
        channel.CurrentPeriod -= int(channel.PortamentoToNote) * ticks * 4
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
        case EffectNoteDelay:
            channel.NoteDelay -= ticks
            // log.Printf("channel %v note delay %v", channel.Channel, channel.NoteDelay)
            if channel.NoteDelay <= 0 {
                channel.UpdateDelay()
                channel.CurrentEffect = EffectNone
            }
        case EffectTremolo:
            channel.Tremolo.Update()
        case EffectPortamentoAndVolumeSlide:
            if !changeRow {
                channel.doVolumeSlide(changeRow)
                channel.doPortamentoToNote(ticks)
            }
        case EffectPortamentoToNote:
            if !changeRow {
                channel.doPortamentoToNote(ticks)
            }
        case EffectPortamentoDown:
            if !changeRow {
                channel.CurrentPeriod += int(channel.PortamentoNote) * ticks * 4
            }
        case EffectPortamentoUp:
            if !changeRow {
                channel.CurrentPeriod -= int(channel.PortamentoNote) * ticks * 4
                if channel.CurrentPeriod < 56 {
                    channel.CurrentSample = -1
                    // channel.CurrentPeriod = 56
                }
            }
        case EffectRetriggerAndVolumeSlide:
            if !changeRow && channel.Retrigger > 0 && ticks % channel.Retrigger == 0 {
                // log.Printf("Retriggering channel %v", channel.Channel)
                instrument := channel.Player.GetInstrument(channel.CurrentSample)
                if instrument != nil && len(instrument.Data) > 0 {
                    channel.startPosition = 0.0
                }

                switch channel.VolumeSlide {
                    case 0:
                    case 1: channel.CurrentVolume -= 1
                    case 2: channel.CurrentVolume -= 2
                    case 3: channel.CurrentVolume -= 4
                    case 4: channel.CurrentVolume -= 8
                    case 5: channel.CurrentVolume -= 16
                    case 6: channel.CurrentVolume = channel.CurrentVolume * 2 / 3
                    case 7: channel.CurrentVolume = channel.CurrentVolume / 2
                    case 8:
                    case 9: channel.CurrentVolume += 1
                    case 10: channel.CurrentVolume += 2
                    case 11: channel.CurrentVolume += 4
                    case 12: channel.CurrentVolume += 8
                    case 13: channel.CurrentVolume += 16
                    case 14: channel.CurrentVolume = channel.CurrentVolume * 3 / 2
                    case 15: channel.CurrentVolume = channel.CurrentVolume * 2
                }

                if channel.CurrentVolume < 0 {
                    channel.CurrentVolume = 0
                }
                if channel.CurrentVolume > 64 {
                    channel.CurrentVolume = 64
                }
            }
    }
}

func (channel *Channel) Update(rate float32) {
    samples := int(float32(channel.Player.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()
    channel.ScopeBuffer.Lock()

    // if channel.CurrentNote != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentFrequency > 0 && channel.Delay <= 0 {
    if channel.CurrentSample >= 0 && channel.CurrentPeriod > 0 {
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

            leftPan := channel.GetLeftPan()
            rightPan := channel.GetRightPan()

            // log.Printf("note volume %v", noteVolume)

            // log.Printf("Write sample %v at %v/%v samples %v rate %v", channel.CurrentSample.Name, channel.startPosition, len(channel.CurrentSample.Data), samples, incrementRate)

            if incrementRate > 0 {
                volume := channel.Volume * noteVolume * float32(channel.Player.GlobalVolume) / 64

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

                    sample := instrument.Data[position] * volume
                    if channel.CurrentEffect == EffectTremolo {
                        // log.Printf("tremolo %v -> %v", sample, channel.Tremolo.Apply(sample))
                        sample = channel.Tremolo.Apply(sample)
                    }

                    channel.AudioBuffer.UnsafeWrite(max(-1, min(1, sample * leftPan)))
                    channel.AudioBuffer.UnsafeWrite(max(-1, min(1, sample * rightPan)))

                    channel.ScopeBuffer.UnsafeWrite(max(-1, min(1, sample * leftPan)))
                    channel.ScopeBuffer.UnsafeWrite(max(-1, min(1, sample * rightPan)))

                    channel.startPosition += incrementRate
                    samplesWritten += 1
                }
            }
        }
    }

    // log.Printf("Channel %v wrote %v samples / %v needed", channel.Channel, samplesWritten, samples)

    for range (samples - samplesWritten) {
        channel.AudioBuffer.UnsafeWrite(0.0)
        channel.AudioBuffer.UnsafeWrite(0.0)

        channel.ScopeBuffer.UnsafeWrite(0.0)
        channel.ScopeBuffer.UnsafeWrite(0.0)
    }

    channel.AudioBuffer.Unlock()
    channel.ScopeBuffer.Unlock()

}

func (channel *Channel) Read(data []byte) (int, error) {
    if channel.Mute {
        for i := 0; i < len(data); i++ {
            data[i] = 0
        }
        channel.AudioBuffer.Clear()
        return len(data), nil
    }

    samples := len(data) / 4

    if samples > len(channel.buffer) {
        samples = len(channel.buffer)
    }

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
        data[i*4+0] = byte(bits)
        data[i*4+1] = byte(bits >> 8)
        data[i*4+2] = byte(bits >> 16)
        data[i*4+3] = byte(bits >> 24)

        /*
        data[i*8+4] = byte(bits)
        data[i*8+5] = byte(bits >> 8)
        data[i*8+6] = byte(bits >> 16)
        data[i*8+7] = byte(bits >> 24)
        */

        i += 1
    }

    i *= 4

    // in a browser we have to return something, so we generate some silence
    if i == 0 && runtime.GOOS == "js" {
        for i < 8 {
            data[i] = 0
            i += 1
        }
        return 8, nil
    } else {
        // on a normal os we can just return 0 if necessary
        // log.Printf("Channel %v return %v samples / %v", channel.Channel, floatSamples, len(data) / 4)
        return floatSamples * 4, nil
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

    DoJump bool
    JumpOrder int

    DoBreak bool
    BreakRow int

    OnChangeRow func(row int)
    OnChangeOrder func(order int, pattern int)
    OnChangeSpeed func(speed int, bpm int)
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

    // player.BPM = 30

    for channelNum, index := range file.ChannelMap {
        pan, ok := file.ChannelPanning[channelNum]
        if !ok {
            pan = 8
        }

        channels[index] = &Channel{
            Channel: channelNum,
            Player: player,
            AudioBuffer: common.MakeAudioBuffer(sampleRate * 2),
            ScopeBuffer: common.MakeAudioBuffer(sampleRate * 2 / 10),
            Pan: int(pan),
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
    player.S3M.Orders = []byte{34}
    player.S3M.SongLength = 1
    */

    // player.S3M.SongLength = 1

    return player
}

func (player *Player) GetPattern() int {
    return int(player.S3M.Orders[player.CurrentOrder])
}

func (player *Player) GetSongLength() int {
    return player.S3M.SongLength
}

func (player *Player) GetRowNoteInfo(channel int, row int) common.NoteInfo {
    return player.GetRowNote(channel, row)
}

func (player *Player) GetRowNote(channel int, row int) *Note {
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

    /*
    if newTicks - oldTicks > 1 {
        log.Printf("Too many ticks! %v", newTicks - oldTicks)
    }
    */

    if player.ticks >= float32(player.Speed) {
        player.CurrentRow += 1
        // log.Printf("Row: %v", player.CurrentRow)
        player.ticks -= float32(player.Speed)

        if player.DoBreak {
            player.DoBreak = false
            player.NextOrder()
            player.CurrentRow = player.BreakRow
        }

        if player.DoJump {
            player.DoJump = false
            player.CurrentRow = 0
            player.CurrentOrder = player.JumpOrder
            if player.OnChangeOrder != nil {
                player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
            }
        }

        if player.OnChangeRow != nil {
            player.OnChangeRow(player.CurrentRow)
        }
    }

    if player.CurrentRow > len(player.S3M.Patterns[0].Rows) - 1 {
        // player.rowPosition = 0
        player.CurrentRow = 0
        player.CurrentOrder += 1
        player.OrdersPlayed += 1
        if player.CurrentOrder >= player.S3M.SongLength {
            player.CurrentOrder = 0
        }

        if player.OnChangeOrder != nil {
            player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
        }

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

func (player *Player) SetOnChangeRow(callback func(row int)) {
    player.OnChangeRow = callback
}

func (player *Player) SetOnChangeOrder(callback func(order int, pattern int)) {
    player.OnChangeOrder = callback
}

func (player *Player) SetOnChangeSpeed(callback func(speed int, bpm int)) {
    player.OnChangeSpeed = callback
}

func (player *Player) GetChannelReaders() []io.Reader {
    readers := make([]io.Reader, len(player.Channels))
    for i, channel := range player.Channels {
        readers[i] = channel
    }
    return readers
}

func (player *Player) ToggleMuteChannel(channel int) bool {
    if channel < 0 || channel >= len(player.Channels) {
        return false
    }

    player.Channels[channel].Mute = !player.Channels[channel].Mute
    return player.Channels[channel].Mute
}

func (player *Player) NextOrder() {
    player.CurrentOrder += 1
    if player.CurrentOrder >= player.S3M.SongLength {
        player.CurrentOrder = 0
    }

    if player.OnChangeOrder != nil {
        player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
    }
}

func (player *Player) PreviousOrder() {
    player.CurrentOrder -= 1
    if player.CurrentOrder < 0 {
        player.CurrentOrder = player.S3M.SongLength - 1
    }

    if player.OnChangeOrder != nil {
        player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
    }
}

func (player *Player) GetSpeed() int {
    return player.Speed
}

func (player *Player) GetBPM() int {
    return player.BPM
}

func (player *Player) GetChannelCount() int {
    return len(player.Channels)
}

func (player *Player) GetName() string {
    return player.S3M.Name
}

func (player *Player) IsStereo() bool {
    return true
}

func (player *Player) GetChannelData(channel int, data []float32) int {
    if channel < len(player.Channels) {
        return player.Channels[channel].ScopeBuffer.Peek(data)
    }

    return 0
}

func (player *Player) ResetRow() {
    player.CurrentRow = 0
}

func (player *Player) GetCurrentOrder() int {
    return player.CurrentOrder
}

func (player *Player) RenderToPCM() io.Reader {
    // make a buffer to hold 1/100th of a second of audio data, which is 4-bytes per sample
    // and 1 samples per channel
    rate := 100
    buffer := make([]float32, player.SampleRate * 2 / rate)
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
                for i := range amount {
                    mix[i] = mix[i] + buffer[i]
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
