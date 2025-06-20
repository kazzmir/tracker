package s3m

import (
    "log"

    "github.com/kazzmir/tracker/common"
)

type Channel struct {
    Player *Player
    AudioBuffer *common.AudioBuffer
    Channel int
    Volume float32
    buffer []float32 // used for reading audio data

    currentRow int
}

func (channel *Channel) UpdateRow() {
    channel.currentRow = channel.Player.CurrentRow
}

func (channel *Channel) UpdateTick(changeRow bool, ticks int) {
}

func (channel *Channel) Update(rate float32) {
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

    Speed int
    BPM int

    CurrentRow int
    CurrentOrder int
    OrdersPlayed int
    ticks float32
}

func (player *Player) GetPattern() int {
    return int(player.S3M.Orders[player.CurrentOrder])
}

func (player *Player) Update(timeDelta float32) {
    oldRow := player.CurrentRow
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
        if oldRow != channel.currentRow {
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

func MakePlayer(file *S3MFile, sampleRate int) *Player {
    channels := make([]*Channel, len(file.ChannelMap))

    player := &Player{
        S3M: file,
        Speed: int(file.InitialSpeed),
        BPM: int(file.InitialTempo),
    }

    log.Printf("Channels %v", len(channels))
    for channelNum, index := range file.ChannelMap {
        log.Printf("Create channel %v", index)
        channels[index] = &Channel{
            Channel: channelNum,
            Player: player,
            AudioBuffer: common.MakeAudioBuffer(sampleRate),
            Volume: 1.0,
            buffer: make([]float32, sampleRate),
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
