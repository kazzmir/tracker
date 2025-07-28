package xm

import (
    "log"
    "math"
    "runtime"
    "io"
    "github.com/kazzmir/tracker/common"
)

const (
    EffectArpeggio = 0
    EffectPortamentoUp = 1
    EffectPortamentoDown = 2
    EffectTonePortamento = 3
    EffectVibrato = 4
    EffectTonePortamentoVolumeSlide = 5
    EffectVibratoVolumeSlide = 6
    EffectTremolo = 7
    EffectSetPanning = 8
    EffectSetSampleOffset = 9
    EffectVolumeSlide = 10
    EffectPositionJump = 11
    EffectSetVolume = 12
    EffectPatternBreak = 13
    EffectExtended = 14
    EffectSetSpeed = 15
    EffectSetGlobalVolume = 16
    EffectSetGlobalVolumeSlide = 17
    EffectEnvelopePosition = 21
    EffectPanningSlide = 25
    EffectMultiRetrigger = 27
    EffectTremor = 29
    EffectExtraFinePortamento = 33

    ExtendedEffectFinePortamentoUp = 0x01
    ExtendedEffectFinePortamentoDown = 0x02
    ExtendedEffectGlissandoControl = 0x03
    ExtendedEffectVibratoControl = 0x04
    ExtendedEffectSetFinetune = 0x05
    ExtendedEffectSetLoop = 0x06
    ExtendedEffectTremoloControl = 0x07
    ExtendedEffectRetriggerNote = 0x09
    ExtendedEffectFineVolumeSlideUp = 0xa
    ExtendedEffectFineVolumeSlideDown = 0xb
    ExtendedEffectNoteCut = 0xc
    ExtendedEffectNoteDelay = 0xd
    ExtendedEffectPatternDelay = 0xe
)

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

func (vibrato *Vibrato) Apply(frequency float32) float32 {
    if vibrato.Depth <= 0 || vibrato.Speed <= 0 {
        return frequency
    }

    // Amiga vibrato is a sine wave with a period of 64
    // and a depth of 8, so we scale the position to that range
    vibratoValue := float32(float64(vibrato.Depth * 40) * math.Sin(float64(vibrato.position) * math.Pi * 360 / 64 / 180))
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

func (tremolo *Tremolo) Value() float64 {
    return float64(tremolo.Depth) / 40 * math.Sin(float64(tremolo.position) * math.Pi * 360 / 64 / 180)
}

func (tremolo *Tremolo) Apply(volume float32) float32 {
    return volume + float32(tremolo.Value())
}

type Channel struct {
    player *Player
    Channel int
    AudioBuffer *common.AudioBuffer
    ScopeBuffer *common.AudioBuffer
    Volume float32
    buffer []float32
    currentRow int // The row that is currently being played
    Mute bool

    startPosition float32

    CurrentEffect int
    CurrentEffectParameter int // The parameter of the current effect
    CurrentVolume float32 // The volume of the current note
    LastVolume float32
    CurrentNote float32
    CurrentInstrument int

    PortamentoTarget float32
    VolumeSlide int

    RetriggerValue int
    RetriggerCount int

    Vibrato Vibrato
    Tremolo Tremolo
}

func (channel *Channel) GetLeftPan() float32 {
    // FIXME
    return 0.5
}

func (channel *Channel) GetRightPan() float32 {
    // FIXME
    return 0.5
}

func (channel *Channel) UpdateRow() {
    channel.currentRow = channel.player.CurrentRow

    note := channel.player.GetRowNote(channel.Channel, channel.currentRow)
    if note == nil {
        return
    }

    resetStartingPosition := false

    newNote := channel.CurrentNote
    newInstrument := channel.CurrentInstrument

    channel.CurrentEffect = -1

    /*
    if channel.Channel == 0 {
        log.Printf("Channel %v: note %+v", channel.Channel, note)
    }
    */

    if note.HasNote {
        newNote = float32(note.NoteNumber)
        if note.NoteNumber == 97 {
            newNote = 0 // No note
        }
        resetStartingPosition = true
        channel.LastVolume = 64
        channel.CurrentVolume = channel.LastVolume
    }

    if note.HasVolume {
        channel.LastVolume = float32(note.Volume) - 16
        channel.CurrentVolume = channel.LastVolume
    }

    if note.HasInstrument {
        newInstrument = int(note.Instrument - 1)
        // log.Printf("Set instrument to %v", channel.CurrentInstrument)
    }

    if note.HasEffectType {
        switch note.EffectType {
            case EffectSetSpeed:
                if note.EffectParameter <= 0x1f {
                    channel.player.Speed = int(note.EffectParameter)
                } else if note.EffectParameter >= 0x20 {
                    channel.player.BPM = int(note.EffectParameter)
                }
                if channel.player.OnChangeSpeed != nil {
                    channel.player.OnChangeSpeed(channel.player.Speed, channel.player.BPM)
                }
            case EffectPortamentoUp:
                channel.CurrentEffect = EffectPortamentoUp
                if note.EffectParameter > 0 {
                    channel.CurrentEffectParameter = int(note.EffectParameter)
                }
            case EffectPortamentoDown:
                channel.CurrentEffect = EffectPortamentoDown
                if note.EffectParameter > 0 {
                    channel.CurrentEffectParameter = int(note.EffectParameter)
                }
            case EffectSetGlobalVolume:
                channel.player.GlobalVolume = int(note.EffectParameter)
            case EffectVolumeSlide:
                channel.CurrentEffect = EffectVolumeSlide
                if note.EffectParameter > 0 {
                    channel.VolumeSlide = int(note.EffectParameter)
                }

            case EffectVibratoVolumeSlide:
                if note.EffectParameter > 0 {
                    channel.VolumeSlide = int(note.EffectParameter)
                }

                channel.CurrentEffect = EffectVibratoVolumeSlide
            case EffectTremolo:
                channel.CurrentEffect = EffectTremolo
                if note.EffectParameter > 0 {
                    channel.Tremolo.Speed = int(note.EffectParameter >> 4)
                    channel.Tremolo.Depth = int(note.EffectParameter & 0x0F)
                }
            case EffectTonePortamento:
                channel.CurrentEffect = EffectTonePortamento
                if note.EffectParameter > 0 {
                    channel.CurrentEffectParameter = int(note.EffectParameter)
                }
                if note.HasNote {
                    channel.PortamentoTarget = float32(note.NoteNumber)
                }
                newNote = channel.CurrentNote
                newInstrument = channel.CurrentInstrument
                resetStartingPosition = false
            case EffectVibrato:
                channel.CurrentEffect = EffectVibrato
                if note.EffectParameter > 0 {
                    channel.Vibrato.Speed = int(note.EffectParameter >> 4)
                    channel.Vibrato.Depth = int(note.EffectParameter & 0x0F)
                }
            case EffectMultiRetrigger:
                channel.CurrentEffect = EffectMultiRetrigger
                if note.EffectParameter > 0 {
                    channel.RetriggerValue = int(note.EffectParameter)
                }
                channel.RetriggerCount = 0
            case EffectSetSampleOffset:
                channel.startPosition = float32(note.EffectParameter) * 0x100
            case EffectExtended:
                channel.CurrentEffect = EffectExtended
                channel.CurrentEffectParameter = int(note.EffectParameter)
                // log.Printf("Channel %v: Extended effect %v with parameter %v", channel.Channel, note.EffectParameter >> 4, note.EffectParameter & 0x0F)

                switch note.EffectParameter >> 4 {
                    case ExtendedEffectFinePortamentoUp:
                        resetStartingPosition = false
                    case ExtendedEffectFinePortamentoDown:
                        resetStartingPosition = false
                    case ExtendedEffectFineVolumeSlideUp:
                    case ExtendedEffectFineVolumeSlideDown:

                    /*
                    case ExtendedEffectFinePortamentoDown:
                    case ExtendedEffectGlissandoControl:
                    case ExtendedEffectVibratoControl:
                    case ExtendedEffectSetFinetune:
                    case ExtendedEffectSetLoop:
                    case ExtendedEffectTremoloControl:
                    case ExtendedEffectRetriggerNote:
                    case ExtendedEffectFineVolumeSlideUp:
                    case ExtendedEffectFineVolumeSlideDown:
                    case ExtendedEffectNoteCut:
                    case ExtendedEffectNoteDelay:
                    case ExtendedEffectPatternDelay:
                    */
                    default: log.Printf("Channel %v: Unknown extended effect 0x%x", channel.Channel, note.EffectParameter >> 4)
                }

            default: log.Printf("Channel %v: Unknown effect type %v", channel.Channel, note.EffectType)
        }
    } else {
        channel.CurrentEffect = -1
    }

    if newInstrument != channel.CurrentInstrument || resetStartingPosition {
        channel.startPosition = 0
    }

    channel.CurrentNote = newNote
    channel.CurrentInstrument = newInstrument
}

func (channel *Channel) doVolumeSlide() {
    volumeAmount := 0

    slideUp := channel.VolumeSlide >> 4
    slideDown := channel.VolumeSlide & 0xf

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

    channel.CurrentVolume += float32(volumeAmount)
    if channel.CurrentVolume < 0 {
        channel.CurrentVolume = 0
    }
    if channel.CurrentVolume > 64 {
        channel.CurrentVolume = 64
    }
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
    const portamentoSlide = 10.2

    switch channel.CurrentEffect {
        case EffectMultiRetrigger:
            channel.RetriggerCount += ticks
            if channel.RetriggerCount >= channel.RetriggerValue {
                channel.startPosition = 0
                channel.RetriggerCount -= channel.RetriggerValue
            }
        case EffectVolumeSlide:
            channel.doVolumeSlide()
        case EffectVibrato:
            channel.Vibrato.Update()
        case EffectVibratoVolumeSlide:
            channel.doVolumeSlide()
            channel.Vibrato.Update()
        case EffectTremolo:
            channel.Tremolo.Update()
        case EffectPortamentoUp:
            channel.CurrentNote += float32(channel.CurrentEffectParameter) / portamentoSlide
        case EffectPortamentoDown:
            channel.CurrentNote -= float32(channel.CurrentEffectParameter) / portamentoSlide
        case EffectTonePortamento:
            if channel.PortamentoTarget > channel.CurrentNote {
                channel.CurrentNote += float32(channel.CurrentEffectParameter) / portamentoSlide
                if channel.CurrentNote > channel.PortamentoTarget {
                    channel.CurrentNote = channel.PortamentoTarget
                }
            } else {
                if channel.PortamentoTarget < channel.CurrentNote {
                    channel.CurrentNote -= float32(channel.CurrentEffectParameter) / portamentoSlide

                    if channel.CurrentNote < channel.PortamentoTarget {
                        channel.CurrentNote = channel.PortamentoTarget
                    }
                }
            }

            // log.Printf("Channel %v: Portamento target %v, current %v", channel.Channel, channel.PortamentoTarget, channel.CurrentNote)
        case EffectExtended:
            switch channel.CurrentEffectParameter >> 4 {
                case ExtendedEffectFinePortamentoUp:
                    channel.CurrentNote += float32(channel.CurrentEffectParameter & 0x0F) / portamentoSlide
                case ExtendedEffectFinePortamentoDown:
                    channel.CurrentNote -= float32(channel.CurrentEffectParameter & 0x0F) / portamentoSlide
                    // log.Printf("Channel fine tune %v", channel.Finetune)
                case ExtendedEffectFineVolumeSlideUp:
                    channel.CurrentVolume += float32(channel.CurrentEffectParameter & 0x0F) / 16
                    if channel.CurrentVolume > 64 {
                        channel.CurrentVolume = 64
                    }
                case ExtendedEffectFineVolumeSlideDown:
                    channel.CurrentVolume -= float32(channel.CurrentEffectParameter & 0x0F) / 16
                    if channel.CurrentVolume < 0 {
                        channel.CurrentVolume = 0
                    }
            }
    }
}

func (channel *Channel) Update(rate float32) {
    samples := int(float32(channel.player.SampleRate) * rate)
    samplesWritten := 0

    channel.AudioBuffer.Lock()
    channel.ScopeBuffer.Lock()

    // if channel.CurrentNote != nil && int(channel.startPosition) < len(channel.CurrentSample.Data) && channel.CurrentFrequency > 0 && channel.Delay <= 0 {
    if channel.CurrentInstrument >= 0 && channel.CurrentNote > 0 {
        instrument := channel.player.GetInstrument(channel.CurrentInstrument)
        if len(instrument.Samples) > 0 {
            sampleObject := &instrument.Samples[0]


            /*
            if channel.CurrentEffect == EffectVibrato || channel.CurrentEffect == EffectVibratoAndVolumeSlide {
                period = channel.Vibrato.Apply(period)
            }
            */

            period := 10 * 12 * 16 * 4 - (channel.CurrentNote + float32(sampleObject.RelativeNoteNumber) - 1) * 16 * 4 - float32(sampleObject.FineTune)/2
            frequency := float32(8373 * math.Pow(2, float64(6 * 12 * 16 * 4 - period) / (12 * 16 * 4)))

            if channel.CurrentEffect == EffectVibrato {
                // log.Printf("Channel %v: Vibrato applied to frequency %v: %v", channel.Channel, frequency, channel.Vibrato.Apply(frequency))
                frequency = channel.Vibrato.Apply(frequency)
            }

            // frequency := float32(8373 * 1712) / float32(channel.CurrentPeriod)
            // frequency := amigaFrequency / float32(period * 2)

            // ???
            // frequency /= 2

            // log.Printf("Channel %v: Note %v, Period %v, Frequency %v, Finetune %v RelativeNote %v", channel.Channel, channel.CurrentNote, period, frequency, sampleObject.FineTune, sampleObject.RelativeNoteNumber)

            incrementRate := float32(frequency) / float32(channel.player.SampleRate)

            noteVolume := channel.CurrentVolume / 64

            leftPan := channel.GetLeftPan()
            rightPan := channel.GetRightPan()

            // log.Printf("note volume %v", noteVolume)

            // log.Printf("Write sample %v at %v/%v samples %v rate %v", channel.CurrentSample.Name, channel.startPosition, len(channel.CurrentSample.Data), samples, incrementRate)

            if incrementRate > 0 {
                volume := channel.Volume * noteVolume * float32(channel.player.GlobalVolume) / 64

                if channel.CurrentEffect == EffectTremolo {
                    volume = channel.Tremolo.Apply(volume)
                }

                // log.Printf("Channel %v: Write sample %v at %v/%v samples %v rate %v volume %v", channel.Channel, instrument.Samples[0].Name, channel.startPosition, len(instrument.Samples[0].Data), samples, incrementRate, volume)
                for range samples {
                    position := int(channel.startPosition)
                    /*
                    if position >= len(channel.CurrentSample.Data) {
                        break
                    }
                    */


                    if position >= len(sampleObject.Data) || (sampleObject.LoopLength > 0 && position >= int(sampleObject.LoopStart + sampleObject.LoopLength)) {
                        // log.Printf("Position %v loop begin %v loop end %v", position, instrument.LoopBegin, instrument.LoopEnd)
                        if sampleObject.LoopLength > 0 && position >= int(sampleObject.LoopStart + sampleObject.LoopLength) {
                            channel.startPosition = float32(sampleObject.LoopStart)
                            position = int(channel.startPosition)
                        } else {
                            break
                        }
                    }

                    // noteVolume = 1

                    sample := instrument.Samples[0].Data[position] * volume

                    // log.Printf("Sample %v", sample)

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

// FIXME: maybe have a common channel object with this read method?
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
    common.DummyPlayer

    XMFile *XMFile
    SampleRate int
    Order int
    ticks float32
    CurrentRow int
    BPM int
    Speed int
    OrdersPlayed int // How many orders have been played so far

    GlobalVolume int

    Channels []*Channel

    OnChangeRow func(row int)
    OnChangeOrder func(order int, pattern int)
    OnChangeSpeed func(speed int, bpm int)
}

/*
RenderToPCM() io.Reader
*/


func MakePlayer(file *XMFile, sampleRate int) *Player {

    /*
    pattern := file.Patterns[0]
    notes := pattern.ParseNotes()
    for i, note := range notes {
        log.Printf("Note: %d, %+v", i, note)
        if i > 20 {
            break
        }
    }
    for i := range pattern.Rows {
        row := pattern.GetRow(int(i), file.Channels)
        var notes bytes.Buffer
        for noteIndex := range row {
            notes.WriteString(row[noteIndex].String())
            notes.WriteString(" ")
        }
        log.Printf("Row %02d: %s", i, notes.String())
        // log.Printf("Raw: %+v", row)
    }
    */

    player := &Player{
        XMFile: file,
        Order: 0,
        BPM: int(file.BPM),
        Speed: int(file.Tempo),
        SampleRate: sampleRate,
        GlobalVolume: 64,
    }

    for channelNum := range file.Channels {
        player.Channels = append(player.Channels, &Channel{
            player: player,
            Channel: channelNum,
            AudioBuffer: common.MakeAudioBuffer(sampleRate * 2),
            ScopeBuffer: common.MakeAudioBuffer(sampleRate * 2 / 10),
            Volume: 1.0,
            CurrentVolume: 64,
            CurrentInstrument: -1,
            buffer: make([]float32, sampleRate),
            currentRow: -1,
        })
    }

    /*
    player.Order = 0
    player.Channels = player.Channels[7:8]
    player.XMFile.Orders = player.XMFile.Orders[2:3]
    */

    /*
    for i := range player.Channels {
        player.Channels[i].Mute = true
    }
    player.Channels[6].Mute = false
    */

    return player
}

func (player *Player) Update(timeDelta float32) {
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

        if player.OnChangeRow != nil {
            player.OnChangeRow(player.CurrentRow)
        }
    }

    if player.CurrentRow >= int(player.XMFile.Patterns[0].Rows) {
        // player.rowPosition = 0
        player.CurrentRow = 0
        player.Order += 1
        player.OrdersPlayed += 1
        if player.Order >= player.GetSongLength() {
            player.Order = 0
        }

        if player.OnChangeOrder != nil {
            player.OnChangeOrder(player.Order, player.GetPattern())
        }

        // log.Printf("order %v next pattern: %v", player.Order, player.GetPattern())
    }


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
}

func (player *Player) GetChannelReaders() []io.Reader {
    var out []io.Reader
    for _, channel := range player.Channels {
        out = append(out, channel)
    }
    return out
}

func (player *Player) GetSpeed() int {
    return player.Speed
}

func (player *Player) GetBPM() int {
    return player.BPM
}

func (player *Player) GetName() string {
    if player.XMFile != nil {
        return player.XMFile.Name
    }

    return ""
}

func (player *Player) GetInstrument(instrument int) *Instrument {
    if instrument < 0 || instrument >= len(player.XMFile.Instruments) {
        return nil
    }

    return player.XMFile.Instruments[instrument]
}

func (player *Player) GetRowNote(channel int, row int) *Note {
    pattern := player.XMFile.GetPattern(player.Order)
    notes := pattern.GetRow(row, player.XMFile.Channels)
    if channel < 0 || channel >= len(notes) {
        return nil
    }

    return &notes[channel]
}

func (player *Player) GetRowNoteInfo(channel int, row int) common.NoteInfo {
    note := player.GetRowNote(channel, row)
    if note == nil {
        return nil
    }

    return note
}

func (player *Player) GetCurrentOrder() int {
    return player.Order
}

func (player *Player) SetOnChangeRow(f func(int)) {
    player.OnChangeRow = f
}

func (player *Player) SetOnChangeOrder(f func(int, int)) {
    player.OnChangeOrder = f
}

func (player *Player) SetOnChangeSpeed(f func(int, int)) {
    player.OnChangeSpeed = f
}

func (player *Player) GetChannelCount() int {
    return len(player.Channels)
}

func (player *Player) GetPattern() int {
    return int(player.XMFile.Orders[player.Order])
}

func (player *Player) GetSongLength() int {
    return len(player.XMFile.Orders)
}

func (player *Player) IsStereo() bool {
    // Assuming XM files are always stereo
    return true
}

func (player *Player) ToggleMuteChannel(channel int) bool {
    player.Channels[channel].Mute = !player.Channels[channel].Mute
    return player.Channels[channel].Mute
}

func (player *Player) NextOrder() {
    player.Order += 1
    if player.Order >= player.GetSongLength() {
        player.Order = 0
    }

    if player.OnChangeOrder != nil {
        player.OnChangeOrder(player.Order, player.GetPattern())
    }
}

func (player *Player) PreviousOrder() {
    player.Order -= 1
    if player.Order < 0 {
        player.Order = player.GetSongLength() - 1
    }

    if player.OnChangeOrder != nil {
        player.OnChangeOrder(player.Order, player.GetPattern())
    }
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


func (player *Player) RenderToPCM() io.Reader {
    // make a buffer to hold 1/100th of a second of audio data, which is 4-bytes per sample
    // and 1 samples per channel
    rate := 100
    buffer := make([]float32, player.SampleRate * 2 / rate)
    mix := make([]float32, player.SampleRate * 2 / rate)

    fillMix := func() bool {
        if player.OrdersPlayed >= player.GetSongLength() {
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

        if player.OrdersPlayed >= player.GetSongLength() {
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
