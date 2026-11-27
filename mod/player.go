package mod

import (
    "log"
    "math"
    "io"
    "fmt"
    "runtime"
    "encoding/binary"
    
    "github.com/kazzmir/tracker/common"
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
    AudioBuffer *common.AudioBuffer
    ScopeBuffer *common.AudioBuffer
    ChannelNumber int

    Vibrato Vibrato
    TonePortamentoTarget int
    TonePortamentoSpeed int
    ArpeggioBase int
    ArpeggioTicks int
    delayedNote *Note
    delayTick int

    VolumeCutTick int

    SampleOffset int

    Volume float32

    Mute bool

    buffer []float32

    CurrentSample *Sample
    CurrentFrequency int

    CurrentEffect int
    CurrentEffectParameter int

    startPosition float64
    fineTune int8
}

func (channel *Channel) Read(data []byte) (int, error) {
    // log.Printf("Read %v bytes from channel %v", len(data), channel.ChannelNumber)

    if channel.Mute {
        for i := 0; i < len(data); i++ {
            data[i] = 0
        }
        channel.AudioBuffer.Clear()
        return len(data), nil
    }

    samples := min(len(data) / 8, len(channel.buffer))
    part := channel.buffer[:samples]
    floatSamples := channel.AudioBuffer.Read(part)

    left, right := channel.stereoGains()
    for i, sample := range part[:floatSamples] {
        binary.LittleEndian.PutUint32(data[i*8:], math.Float32bits(sample * left))
        binary.LittleEndian.PutUint32(data[i*8+4:], math.Float32bits(sample * right))
    }

    // In a browser the audio device needs something even on underrun.
    if floatSamples == 0 && runtime.GOOS == "js" && len(data) >= 8 {
        clear(data[:8])
        return 8, nil
    }
    return floatSamples * 8, nil
}

func (channel *Channel) stereoGains() (left, right float32) {
    // MOD channels are arranged left-right-right-left, with 25% crossfeed.
    // Reserve mix headroom instead of summing full-volume mono channels.
    gain := float32(min(128, max(32, 256 / max(1, channel.Player.ModFile.Channels)))) / 128
    if n := channel.ChannelNumber % 4; n == 0 || n == 3 {
        return 0.75 * gain, 0.25 * gain
    }
    return 0.25 * gain, 0.75 * gain
}

const palSampleClock = 3546895

func semitoneToNote(semitone int) string {
    switch semitone {
        case 0: return "C"
        case 1: return "C#"
        case 2: return "D"
        case 3: return "D#"
        case 4: return "E"
        case 5: return "F"
        case 6: return "F#"
        case 7: return "G"
        case 8: return "G#"
        case 9: return "A"
        case 10: return "A#"
        case 11: return "B"
    }

    return "?"
}

func noteValue(period uint16) int {
    c3 := 856
    c4 := 428
    c5 := 214

    cValues := map[int]int{
        c3: 3,
        c4: 4,
        c5: 5,
    }

    for frequency, octave := range cValues {
        for semitone := range 12 {
            newFrequency := addSemitones(frequency, semitone)
            diff := newFrequency - int(period)
            if diff < 0 {
                diff = -diff
            }
            if diff <= 2 {
                return semitone + octave * 12
            }
        }
    }

    return 0
}

// convert a note period to a note name.
// 428 = C-4
func ConvertToNote(period uint16) string {
    c3 := 856
    c4 := 428
    c5 := 214

    cValues := map[int]int{
        c3: 3,
        c4: 4,
        c5: 5,
    }

    for frequency, octave := range cValues {
        for semitone := range 12 {
            newFrequency := addSemitones(frequency, semitone)
            diff := newFrequency - int(period)
            if diff < 0 {
                diff = -diff
            }
            if diff <= 2 {
                name := semitoneToNote(semitone)

                if len(name) == 1 {
                    return fmt.Sprintf("%v-%v", name, octave)
                } else {
                    return fmt.Sprintf("%v%v", name, octave)
                }
            }
        }
    }

    return fmt.Sprintf("%v", period)
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

// add semitones to a period. period = 1/frequency
func addSemitones(period int, semitones int) int {
    // semitones = 12 * log_2(f2/f1)
    // f2 = f1 * 2^(semitones/12)

    if semitones == 0 {
        return period
    }

    return int(float64(period) / math.Pow(2, float64(semitones) / 12.0))
}

func (channel *Channel) UpdatePortamento(ticks int) {
    if channel.TonePortamentoTarget == 0 {
        return
    }
    direction := 1
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

func (channel *Channel) UpdateTick(tick int) {
    changeRow := tick == 0
    const ticks = 1
    if channel.delayedNote != nil && tick == channel.delayTick {
        channel.applyNote(channel.delayedNote)
        channel.delayedNote = nil
    }

    if channel.VolumeCutTick > 0 && tick == channel.VolumeCutTick {
        channel.Volume = 0
    }

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
        case EffectPortamentoAndVolumeSlide:
            if !changeRow {
                channel.UpdateVolume()
                channel.UpdatePortamento(ticks)
            }
        case EffectVibratoAndVolumeSlide:
            if !changeRow {
                channel.Vibrato.Update()
                channel.UpdateVolume()
            }
        case EffectVolumeSlide:
            if !changeRow {
                channel.UpdateVolume()
            }
        case EffectTonePortamento:
            if !changeRow {
                channel.UpdatePortamento(ticks)
            }
        case EffectVibrato:
            if !changeRow {
                channel.Vibrato.Update()
            }
        case EffectArpeggio:
            tick1 := channel.CurrentEffectParameter >> 4
            tick2 := channel.CurrentEffectParameter & 0xf

            if changeRow {
                channel.ArpeggioTicks = 0
                // channel.CurrentFrequency = channel.ArpeggioBase
            } else {
                channel.ArpeggioTicks += ticks
                switch channel.ArpeggioTicks % 3 {
                    case 0:
                        if tick1 > 0 || tick2 > 0 {
                            channel.CurrentFrequency = channel.ArpeggioBase
                        }
                    case 1:
                        if tick1 > 0 {
                            channel.CurrentFrequency = addSemitones(channel.ArpeggioBase, tick1)
                            // log.Printf("Arpeggio tick1 %v, frequency %v", tick1, channel.CurrentFrequency)
                        }
                    case 2:
                        if tick2 > 0 {
                            channel.CurrentFrequency = addSemitones(channel.ArpeggioBase, tick2)
                            // log.Printf("Arpeggio tick2 %v, frequency %v", tick2, channel.CurrentFrequency)
                        }
                }
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
    channel.VolumeCutTick = 0
    channel.delayedNote = nil
    note, _ := channel.Player.GetNote(channel.ChannelNumber)
    if note.EffectNumber == EffectExtra && note.EffectParameter >> 4 == 0xd && note.EffectParameter & 0xf != 0 {
        channel.delayedNote = note
        channel.delayTick = int(note.EffectParameter & 0xf)
        return
    }
    channel.applyNote(note)
}

func (channel *Channel) applyNote(note *Note) {
    tonePortamento := note.EffectNumber == EffectTonePortamento || note.EffectNumber == EffectPortamentoAndVolumeSlide
    playing := channel.CurrentSample != nil && channel.startPosition < float64(len(channel.CurrentSample.Data)) && channel.CurrentFrequency > 0
    if note.SampleNumber != 0 {
        sample := channel.Player.GetSample(note.SampleNumber - 1)
        if sample != nil && len(sample.Data) != 0 {
            channel.Volume = float32(min(sample.Volume, 64)) / 64
        }
        if !tonePortamento || !playing {
            channel.CurrentSample = sample
            if sample != nil {
                channel.fineTune = int8(sample.FineTune << 4) >> 4
            }
        }
    }

    newFrequency := channel.CurrentFrequency
    trigger := note.PeriodFrequency != 0 && (!tonePortamento || !playing)
    if note.PeriodFrequency != 0 {
        period := int(note.PeriodFrequency)
        if tonePortamento {
            channel.TonePortamentoTarget = period
        }
        if trigger {
            newFrequency = period
            channel.startPosition = 0
            channel.Vibrato.position = 0
        }
    }

    switch note.EffectNumber {
        case EffectSetVolume:
            volume := min(note.EffectParameter, 64)
            channel.Volume = float32(volume) / 64.0
        case EffectSetSpeed:
            if note.EffectParameter > 0 && note.EffectParameter <= 0x1f {
                channel.Player.Speed = int(note.EffectParameter)
            } else if note.EffectParameter >= 0x20 && note.EffectParameter <= 0xff {
                channel.Player.BPM = int(note.EffectParameter)
            }

            if channel.Player.OnChangeSpeed != nil {
                channel.Player.OnChangeSpeed(channel.Player.Speed, channel.Player.BPM)
            }

        case EffectArpeggio:
            if note.EffectParameter > 0 {
                channel.CurrentEffect = EffectArpeggio
                channel.CurrentEffectParameter = int(note.EffectParameter)
                channel.ArpeggioBase = newFrequency
            }
        case EffectTonePortamento:
            channel.CurrentEffect = EffectTonePortamento
            if note.EffectParameter > 0 {
                channel.CurrentEffectParameter = int(note.EffectParameter)
                channel.TonePortamentoSpeed = int(note.EffectParameter)
            }

        case EffectPortamentoUp:
            channel.CurrentEffect = EffectPortamentoUp
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectPortamentoDown:
            channel.CurrentEffect = EffectPortamentoDown
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectVolumeSlide:
            channel.CurrentEffect = EffectVolumeSlide
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectPortamentoAndVolumeSlide:
            channel.CurrentEffect = EffectPortamentoAndVolumeSlide
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectVibratoAndVolumeSlide:
            channel.CurrentEffect = EffectVibratoAndVolumeSlide
            channel.CurrentEffectParameter = int(note.EffectParameter)
        case EffectPositionJump:
            channel.Player.jumpOrder = int(note.EffectParameter)
            channel.Player.breakRow = 0
        case EffectPatternBreak:
            if channel.Player.jumpOrder < 0 {
                channel.Player.jumpOrder = channel.Player.CurrentOrder + 1
            }
            value := int(note.EffectParameter >> 4) * 10 + int(note.EffectParameter & 0xf)
            if value > 63 {
                value = 0
            }
            channel.Player.breakRow = value
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
                case 0:
                    // set hardware filter, ignore
                case 1:
                    // fine portamento up
                    newFrequency = max(1, newFrequency - int(note.EffectParameter & 0xf))
                case 2:
                    newFrequency += int(note.EffectParameter & 0xf)
                case 0xa:
                    channel.Volume = min(channel.Volume + float32(note.EffectParameter & 0xf) / 64.0, 1.0)
                case 0xb:
                    // fine volume slide down
                    channel.Volume = max(channel.Volume - float32(note.EffectParameter & 0xf) / 64.0, 0.0)
                case 0xc:
                    channel.VolumeCutTick = int(note.EffectParameter & 0xf)
                case 0xd:
                    // The delayed note is applied by UpdateTick.
                default:
                    log.Printf("Warning: channel %v unhandled extra effect %x with parameter %x", channel.ChannelNumber, note.EffectParameter >> 4, note.EffectParameter & 0xf)
            }
        case EffectSampleOffset:
            if note.EffectParameter > 0 {
                channel.SampleOffset = int(note.EffectParameter)
            }
            if trigger {
                channel.startPosition = float64(channel.SampleOffset * 0x100)
            }
        default:
            if note.EffectNumber != 0 || note.EffectParameter != 0 {
                log.Printf("Warning: channel %v unhandled effect %x with parameter %v", channel.ChannelNumber, note.EffectNumber, note.EffectParameter)
            }
    }

    channel.CurrentFrequency = newFrequency
}

func (channel *Channel) render(samples int) {
    channel.AudioBuffer.Lock()
    channel.ScopeBuffer.Lock()
    defer channel.AudioBuffer.Unlock()
    defer channel.ScopeBuffer.Unlock()

    var data []float32
    var end, loopStart int
    looping := false
    if sample := channel.CurrentSample; sample != nil && channel.CurrentFrequency > 0 {
        data = sample.Data
        end = len(data)
        loopStart = sample.LoopStart * 2
        loopEnd := min(end, loopStart + sample.LoopLength * 2)
        if sample.LoopLength > 1 && loopStart >= 0 && loopEnd - loopStart > 2 {
            end, looping = loopEnd, true
        }
    }

    period := channel.CurrentFrequency
    if channel.CurrentEffect == EffectVibrato || channel.CurrentEffect == EffectVibratoAndVolumeSlide {
        period = channel.Vibrato.Apply(period)
    }
    increment := float64(palSampleClock) / (float64(max(1, period)) * float64(channel.Player.SampleRate))
    if channel.fineTune != 0 {
        increment *= math.Exp2(float64(channel.fineTune) / 96)
    }
    position := channel.startPosition
    for range samples {
        var value float32
        i := int(position)
        if i < end {
            next := min(i + 1, end - 1)
            if looping && i + 1 == end {
                next = loopStart
            }
            value = data[i] + float32(position - float64(i)) * (data[next] - data[i])
            value *= channel.Volume
            position += increment
            if looping && position >= float64(end) {
                position = float64(loopStart) + math.Mod(position - float64(end), float64(end - loopStart))
            }
        }
        channel.AudioBuffer.UnsafeWrite(value)
        channel.ScopeBuffer.UnsafeWrite(value)
    }
    channel.startPosition = position
}

func MakeChannelVoice(channelNumber int, player *Player) *Channel {
    channel := &Channel{
        Player: player,
        ChannelNumber: channelNumber,
        AudioBuffer: common.MakeAudioBuffer(player.SampleRate),
        ScopeBuffer: common.MakeAudioBuffer(player.SampleRate / 10),
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

    OnChangeRow func(int)
    OnChangeOrder func(int, int)
    OnChangeSpeed func(int, int)

    tick int
    framesLeft int
    frameRemainder float64
    jumpOrder int
    breakRow int
}

func MakePlayer(modfile *ModFile, sampleRate int) *Player {
    player := &Player{
        ModFile: modfile,
        SampleRate: sampleRate,
        Speed: 6,
        BPM: 125,
        CurrentRow: 0,
        jumpOrder: -1,
    }

    for i := range modfile.Channels {
        player.Channels = append(player.Channels, MakeChannelVoice(i, player))
    }

    return player
}

func (player *Player) ToggleMuteChannel(channel int) bool {
    if channel < 0 || channel >= len(player.Channels) {
        return false
    }

    player.Channels[channel].Mute = !player.Channels[channel].Mute
    return player.Channels[channel].Mute
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

func (player *Player) GetSpeed() int {
    return player.Speed
}

func (player *Player) GetBPM() int {
    return player.BPM
}

func (player *Player) GetChannelCount() int {
    return len(player.Channels)
}

func (player *Player) GetSongLength() int {
    return player.ModFile.SongLength
}

func (player *Player) GetName() string {
    return player.ModFile.Name
}

func (player *Player) GetRowNoteInfo(channel int, rowNumber int) (common.NoteInfo, bool) {
    return player.GetRowNote(channel, rowNumber)
}

func (player *Player) GetRowNote(channel int, rowNumber int) (*Note, bool) {
    pattern := player.GetPattern()
    row := &player.ModFile.Patterns[pattern].Rows[rowNumber]

    if channel < len(row.Notes) {
        return &row.Notes[channel], true
    }

    return &Note{}, false
}

func (player *Player) SetOnChangeRow(callback func(int)) {
    player.OnChangeRow = callback
}

func (player *Player) SetOnChangeOrder(callback func(int, int)) {
    player.OnChangeOrder = callback
}

func (player *Player) SetOnChangeSpeed(callback func(int, int)) {
    player.OnChangeSpeed = callback
}

func (player *Player) GetChannelReaders() []io.Reader {
    readers := make([]io.Reader, len(player.Channels))
    for i, channel := range player.Channels {
        readers[i] = channel
    }
    return readers
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

func (player *Player) SetOrder(order int) {
    if order >= player.ModFile.SongLength {
        order = 0
    }

    player.CurrentRow = 0
    player.CurrentOrder = order
    player.tick = 0
    player.framesLeft = 0
    player.jumpOrder = -1
    player.breakRow = 0

    if player.OnChangeOrder != nil {
        player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
    }
}

func (player *Player) NextOrder() {
    player.SetOrder(player.CurrentOrder + 1)
}

func (player *Player) PreviousOrder() {
    player.SetOrder(max(player.CurrentOrder - 1, 0))
}

func (player *Player) ResetRow() {
    player.SetOrder(player.CurrentOrder)
}

func (player *Player) GetCurrentOrder() int {
    return player.CurrentOrder
}

func (player *Player) IsStereo() bool {
    return false
}

func (player *Player) GetChannelData(channel int, data []float32) int {
    if channel < 0 || channel >= len(player.Channels) {
        return 0
    }

    return player.Channels[channel].ScopeBuffer.Peek(data)
}

func (player *Player) beginTick() {
    if player.tick == 0 {
        if player.OnChangeRow != nil {
            player.OnChangeRow(player.CurrentRow)
        }
        for _, channel := range player.Channels {
            channel.UpdateRow()
        }
    }
    for _, channel := range player.Channels {
        channel.UpdateTick(player.tick)
    }
    // The MOD tick is 2.5 / BPM seconds. Like OpenMPT's classic tempo
    // mode, truncate each tick to a whole number of output frames.
    player.framesLeft = max(1, player.SampleRate * 5 / (2 * player.BPM))
}

func (player *Player) renderFrames(frames int) {
    for _, channel := range player.Channels {
        channel.render(frames)
    }
    player.framesLeft -= frames
    if player.framesLeft != 0 {
        return
    }
    player.tick++
    if player.tick < player.Speed {
        return
    }
    player.tick = 0

    oldOrder := player.CurrentOrder
    if player.jumpOrder >= 0 {
        player.CurrentOrder = player.jumpOrder
        player.CurrentRow = player.breakRow
        player.jumpOrder = -1
        player.breakRow = 0
    } else {
        player.CurrentRow++
        if player.CurrentRow >= len(player.ModFile.Patterns[player.GetPattern()].Rows) {
            player.CurrentRow = 0
            player.CurrentOrder++
        }
    }
    wrapped := player.CurrentOrder >= player.ModFile.SongLength
    if wrapped {
        player.CurrentOrder = 0
    }
    if (wrapped || oldOrder != player.CurrentOrder) && player.OnChangeOrder != nil {
        player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
    }
}

func (player *Player) Update(timeDelta float32) {
    player.frameRemainder += float64(timeDelta) * float64(player.SampleRate)
    // Callers supply float32 durations (e.g. 1/100), which can land
    // just below a whole frame. Keep the fractional frame between calls.
    frames := int(player.frameRemainder)
    player.frameRemainder -= float64(frames)
    for frames > 0 {
        if player.framesLeft == 0 {
            player.beginTick()
        }
        count := min(frames, player.framesLeft)
        player.renderFrames(count)
        frames -= count
    }
}

// RenderToPCM produces one pass of interleaved little-endian stereo float32.
func (player *Player) RenderToPCM() io.Reader {
    var buffer, mix []float32
    var pcm, pending []byte
    seen := make(map[[2]int]bool)
    return &common.ReaderFunc{Func: func(data []byte) (int, error) {
        if len(data) == 0 {
            return 0, nil
        }
        if len(pending) == 0 {
            if player.framesLeft == 0 {
                if player.tick == 0 {
                    row := [2]int{player.CurrentOrder, player.CurrentRow}
                    if seen[row] || player.ModFile.SongLength == 0 {
                        return 0, io.EOF
                    }
                    seen[row] = true
                }
                player.beginTick()
            }
            frames := player.framesLeft
            if cap(buffer) < frames {
                buffer = make([]float32, frames)
                mix = make([]float32, 2 * frames)
                pcm = make([]byte, 8 * frames)
            }
            buffer, mix, pcm = buffer[:frames], mix[:2*frames], pcm[:8*frames]
            clear(mix)
            player.renderFrames(frames)
            for _, channel := range player.Channels {
                amount := channel.AudioBuffer.Read(buffer)
                if !channel.Mute {
                    left, right := channel.stereoGains()
                    for i, sample := range buffer[:amount] {
                        mix[i*2] += sample * left
                        mix[i*2+1] += sample * right
                    }
                }
            }
            for i, sample := range mix {
                binary.LittleEndian.PutUint32(pcm[i*4:], math.Float32bits(max(-1, min(1, sample))))
            }
            pending = pcm
        }
        count := copy(data, pending)
        pending = pending[count:]
        return count, nil
    }}
}
