package xm

import (
    "log"
    "math"
    "runtime"
    "io"
    "bytes"
    "github.com/kazzmir/tracker/common"
)

type Channel struct {
    player *Player
    Channel int
    AudioBuffer *common.AudioBuffer
    ScopeBuffer *common.AudioBuffer
    Volume float32
    buffer []float32
    currentRow int // The row that is currently being played
    Mute bool
}

func (channel *Channel) UpdateRow() {
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
}

func (channel *Channel) Update(timeDelta float32) {
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
    Order int
    ticks float32
    CurrentRow int
    BPM int
    Speed int

    Channels []*Channel

    OnChangeRow func(row int)
    OnChangeOrder func(order int, pattern int)
    OnChangeSpeed func(speed int, bpm int)
}

/*
GetCurrentOrder() int
GetPattern() int
GetSongLength() int
GetChannelCount() int
GetRowNoteInfo(channel int, row int) common.NoteInfo
GetChannelData(channel int, data []float32) int
ToggleMuteChannel(channel int) bool
IsStereo() bool
NextOrder()
PreviousOrder()
ResetRow()
GetCurrentOrder() int
SetOnChangeRow(func(int))
SetOnChangeOrder(func(int, int))
SetOnChangeSpeed(func(int, int))
GetChannelReaders() []io.Reader
RenderToPCM() io.Reader
*/


func MakePlayer(file *XMFile, sampleRate int) *Player {

    pattern := file.Patterns[0]
    /*
    notes := pattern.ParseNotes()
    for i, note := range notes {
        log.Printf("Note: %d, %+v", i, note)
        if i > 20 {
            break
        }
    }
    */
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

    player := &Player{
        XMFile: file,
        Order: 0,
        BPM: 125,
        Speed: 6,
    }

    for channelNum := range file.Channels {
        player.Channels = append(player.Channels, &Channel{
            player: player,
            Channel: channelNum,
            AudioBuffer: common.MakeAudioBuffer(sampleRate * 2),
            ScopeBuffer: common.MakeAudioBuffer(sampleRate * 2 / 10),
            Volume: 1.0,
            buffer: make([]float32, sampleRate),
            currentRow: -1,
        })
    }

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
