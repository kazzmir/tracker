package xm

import (
    "log"
    "bytes"
    "github.com/kazzmir/tracker/common"
)

type Player struct {
    common.DummyPlayer
}

/*
GetName() string
GetCurrentOrder() int
GetPattern() int
GetSongLength() int
GetSpeed() int
GetBPM() int
GetChannelCount() int
GetRowNoteInfo(channel int, row int) common.NoteInfo
GetChannelData(channel int, data []float32) int
ToggleMuteChannel(channel int) bool
IsStereo() bool
Update(float32)
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
    }

    return player
}
