package s3m

import (
    "github.com/kazzmir/tracker/common"
)

type Channel struct {
    AudioBuffer *common.AudioBuffer
}

func (channel *Channel) Read(data []byte) (int, error) {
    for i := range data {
        data[i] = 0
    }

    return len(data), nil
}

type Player struct {
    Channels []*Channel
    S3M *S3MFile
}

func (player *Player) Update(ticks float32) {
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

func MakePlayer(file *S3MFile, sampleRate int) *Player {
    return &Player{
    }
}
