package main

import (
    "io"

    "github.com/kazzmir/tracker/common"
)

type DummyPlayer struct {
}

func (player *DummyPlayer) GetBPM() int {
    return 125
}

func (player *DummyPlayer) GetSpeed() int {
    return 6
}

func (player *DummyPlayer) GetChannelCount() int {
    return 0
}

func (player *DummyPlayer) GetCurrentOrder() int {
    return 0
}

func (player *DummyPlayer) GetName() string {
    return "..."
}

func (player *DummyPlayer) GetPattern() int {
    return 0
}

func (player *DummyPlayer) GetSongLength() int {
    return 0
}

func (player *DummyPlayer) GetChannelData(channel int, data []float32) int {
    return 0
}

func (player *DummyPlayer) IsStereo() bool {
    return false
}

func (player *DummyPlayer) NextOrder() {
}

func (player *DummyPlayer) PreviousOrder() {
}

func (player *DummyPlayer) RenderToPCM() io.Reader {
    return nil
}

func (player *DummyPlayer) ResetRow() {
}

func (player *DummyPlayer) SetOnChangeRow(onChangeRow func(int)) {
}

func (player *DummyPlayer) SetOnChangeOrder(onChangeOrder func(int, int)) {
}

func (player *DummyPlayer) SetOnChangeSpeed(onChangeSpeed func(int, int)) {
}

func (player *DummyPlayer) Update(delta float32) {
}

func (player *DummyPlayer) GetRowNoteInfo(channel int, row int) common.NoteInfo {
    return nil
}

func (player *DummyPlayer) GetChannelReaders() []io.Reader {
    return nil
}
