package s3m

import (
	"encoding/binary"
	"io"
	"math"
	"runtime"

	"github.com/kazzmir/tracker/common"
)

type Player struct {
	S3M                                                      *S3MFile
	SampleRate, Speed, BPM, CurrentRow, CurrentOrder         int
	GlobalVolume                                             uint8
	OnChangeRow                                              func(int)
	OnChangeOrder                                            func(int, int)
	OnChangeSpeed                                            func(int, int)
	channels                                                 []channel
	tick, framesLeft, nextOrder, breakRow, delay, extraTicks int
	remainder                                                float64
	globalSlide                                              int
}

func MakePlayer(file *S3MFile, rate int) *Player {
	player := &Player{S3M: file, SampleRate: rate, Speed: int(file.InitialSpeed), BPM: int(file.InitialTempo), GlobalVolume: file.GlobalVolume, nextOrder: -1, channels: make([]channel, len(file.ChannelMap))}
	if player.Speed == 0 {
		player.Speed = 6
	}
	if player.BPM < 32 {
		player.BPM = 125
	}
	for index := range player.channels {
		player.channels[index] = channel{player: player, buffer: common.MakeAudioBuffer(rate * 2), scope: common.MakeAudioBuffer(max(2, rate/5)), readBuffer: make([]float32, rate*2), selected: -1, volume: 64, pan: file.ChannelPanning[index]}
	}
	player.CurrentOrder = player.findOrder(0)
	return player
}

func (player *Player) findOrder(order int) int {
	for order < len(player.S3M.Orders) && player.S3M.Orders[order] == 254 {
		order++
	}
	return min(order, len(player.S3M.Orders))
}

func (player *Player) beginTick() {
	if player.tick == 0 {
		player.delay, player.extraTicks = 0, 0
		if player.OnChangeRow != nil {
			player.OnChangeRow(player.CurrentRow)
		}
		for index := range player.channels {
			note, _ := player.GetRowNote(index, player.CurrentRow)
			player.channels[index].beginRow(*note)
		}
	}
	for index := range player.channels {
		player.channels[index].updateTick(player.tick)
	}
	for index := range player.channels {
		player.channels[index].prepare()
	}
	player.framesLeft = max(1, player.SampleRate*5/(2*player.BPM))
}

func (player *Player) render(frames int) {
	for index := range player.channels {
		player.channels[index].render(frames)
	}
	player.framesLeft -= frames
	if player.framesLeft != 0 {
		return
	}
	player.tick++
	if player.tick < (player.Speed+player.extraTicks)*(1+player.delay) {
		return
	}
	player.tick = 0
	old := player.CurrentOrder
	if player.nextOrder >= 0 {
		player.CurrentOrder = player.findOrder(player.nextOrder)
		player.CurrentRow = player.breakRow
	} else {
		player.CurrentRow++
		if player.CurrentRow >= 64 {
			player.CurrentOrder = player.findOrder(player.CurrentOrder + 1)
			player.CurrentRow = 0
		}
	}
	player.nextOrder, player.breakRow = -1, 0
	if old != player.CurrentOrder && player.CurrentOrder < player.GetSongLength() {
		for index := range player.channels {
			player.channels[index].loopRow, player.channels[index].loopCount = 0, 0
		}
		if player.OnChangeOrder != nil {
			player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
		}
	}
}

func (player *Player) Update(seconds float32) {
	player.remainder += float64(seconds) * float64(player.SampleRate)
	frames := int(math.Round(player.remainder))
	player.remainder -= float64(frames)
	for frames > 0 {
		if player.CurrentOrder >= player.GetSongLength() {
			player.seek(0)
			if player.CurrentOrder >= player.GetSongLength() {
				return
			}
		}
		if player.framesLeft == 0 {
			player.beginTick()
		}
		count := min(frames, player.framesLeft)
		player.render(count)
		frames -= count
	}
}

// RenderToPCM produces one pass of interleaved stereo float32 PCM.
func (player *Player) RenderToPCM() io.Reader {
	var samples, mix []float32
	var pcm, pending []byte
	seen := make(map[[2]int]bool)
	return &common.ReaderFunc{Func: func(dst []byte) (int, error) {
		if len(dst) == 0 {
			return 0, nil
		}
		if len(pending) == 0 {
			if player.CurrentOrder >= player.GetSongLength() {
				return 0, io.EOF
			}
			if player.framesLeft == 0 {
				if player.tick == 0 {
					looping := false
					for index := range player.channels {
						looping = looping || player.channels[index].loopCount != 0
					}
					if !looping {
						row := [2]int{player.CurrentOrder, player.CurrentRow}
						if seen[row] {
							return 0, io.EOF
						}
						seen[row] = true
					}
				}
				player.beginTick()
			}
			frames := player.framesLeft
			if cap(samples) < frames*2 {
				samples = make([]float32, frames*2)
				mix = make([]float32, frames*2)
				pcm = make([]byte, frames*8)
			}
			samples, mix, pcm = samples[:frames*2], mix[:frames*2], pcm[:frames*8]
			clear(mix)
			player.render(frames)
			for index := range player.channels {
				track := &player.channels[index]
				count := track.buffer.Read(samples)
				if !track.mute.Load() {
					for offset, value := range samples[:count] {
						mix[offset] += value
					}
				}
			}
			for offset, value := range mix {
				binary.LittleEndian.PutUint32(pcm[offset*4:], math.Float32bits(value))
			}
			pending = pcm
		}
		count := copy(dst, pending)
		pending = pending[count:]
		return count, nil
	}}
}

func (track *channel) Read(dst []byte) (int, error) {
	count := min(len(track.readBuffer), len(dst)/8*2)
	samples := track.readBuffer[:count]
	count = track.buffer.Read(samples)
	if track.mute.Load() {
		clear(samples[:count])
	}
	for index, value := range samples[:count] {
		binary.LittleEndian.PutUint32(dst[index*4:], math.Float32bits(value))
	}
	if count == 0 && runtime.GOOS == "js" && len(dst) >= 8 {
		clear(dst[:8])
		return 8, nil
	}
	return count * 4, nil
}

func (player *Player) GetPattern() int {
	if player.CurrentOrder >= player.GetSongLength() {
		return -1
	}
	return int(player.S3M.Orders[player.CurrentOrder])
}
func (player *Player) GetCurrentOrder() int { return player.CurrentOrder }
func (player *Player) GetSongLength() int   { return len(player.S3M.Orders) }
func (player *Player) GetChannelCount() int { return len(player.channels) }
func (player *Player) GetSpeed() int        { return player.Speed }
func (player *Player) GetBPM() int          { return player.BPM }
func (player *Player) GetName() string      { return player.S3M.Name }
func (player *Player) IsStereo() bool       { return true }
func (player *Player) GetInstrument(index int) *Instrument {
	if index < 0 || index >= len(player.S3M.Instruments) {
		return nil
	}
	return &player.S3M.Instruments[index]
}
func (player *Player) GetRowNote(index, row int) (*Note, bool) {
	pat := player.GetPattern()
	if pat < 0 || pat >= len(player.S3M.Patterns) {
		return &Note{}, false
	}
	rows := player.S3M.Patterns[pat].Rows
	if row < 0 || row >= len(rows) || index < 0 || index >= len(rows[row]) {
		return &Note{}, false
	}
	return &rows[row][index], true
}
func (player *Player) GetPatternRowCount() int { return 64 }

func (player *Player) GetRowNoteInfo(index, row int) (common.NoteInfo, bool) {
	return player.GetRowNote(index, row)
}
func (player *Player) GetChannelReaders() []io.Reader {
	out := make([]io.Reader, len(player.channels))
	for index := range out {
		out[index] = &player.channels[index]
	}
	return out
}
func (player *Player) GetChannelData(index int, dst []float32) int {
	if index < 0 || index >= len(player.channels) {
		return 0
	}
	return player.channels[index].scope.Peek(dst)
}
// GetSpectrumChannel copies recent, unmuted output as stereo sample pairs.
func (player *Player) GetSpectrumChannel(index int, dst []float32) int {
	if index < 0 || index >= len(player.channels) || player.channels[index].mute.Load() {
		return 0
	}
	return player.channels[index].scope.PeekLatest(dst[:len(dst)&^1])
}

func (player *Player) ToggleMuteChannel(index int) bool {
	if index < 0 || index >= len(player.channels) {
		return false
	}
	mute := &player.channels[index].mute
	for {
		old := mute.Load()
		if mute.CompareAndSwap(old, !old) {
			return !old
		}
	}
}
func (player *Player) SetOnChangeRow(callback func(int))        { player.OnChangeRow = callback }
func (player *Player) SetOnChangeOrder(callback func(int, int)) { player.OnChangeOrder = callback }
func (player *Player) SetOnChangeSpeed(callback func(int, int)) { player.OnChangeSpeed = callback }
func (player *Player) NextOrder()                               { player.seek(player.CurrentOrder + 1) }
func (player *Player) PreviousOrder() {
	order := player.CurrentOrder - 1
	for order > 0 && player.S3M.Orders[order] == 254 {
		order--
	}
	player.seek(max(0, order))
}
func (player *Player) ResetRow() { player.seek(player.CurrentOrder) }
func (player *Player) seek(order int) {
	old := player.CurrentOrder
	player.CurrentOrder = player.findOrder(order)
	if player.CurrentOrder >= player.GetSongLength() {
		player.CurrentOrder = player.findOrder(0)
	}
	player.CurrentRow, player.tick, player.framesLeft, player.nextOrder, player.breakRow = 0, 0, 0, -1, 0
	for index := range player.channels {
		track := &player.channels[index]
		track.loopCount, track.loopRow = 0, 0
		track.buffer.Clear()
	}
	if old != player.CurrentOrder && player.CurrentOrder < player.GetSongLength() && player.OnChangeOrder != nil {
		player.OnChangeOrder(player.CurrentOrder, player.GetPattern())
	}
}
