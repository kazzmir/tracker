package xm

import (
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
    player := &Player{
    }

    return player
}
