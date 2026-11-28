package it

import (
	"encoding/binary"
	"io"
	"math"
	"runtime"
	"sync/atomic"

	"github.com/kazzmir/tracker/common"
)

type Player struct {
	module                                *Module
	rate, order, row, tick, framesLeft    int
	speed, tempo, global                  int
	nextOrder, nextRow, delay, extraTicks int
	frameRemainder                        float64
	channels                              []channel
	onRow                                 func(int)
	onOrder                               func(int, int)
	onSpeed                               func(int, int)
}

func MakePlayer(module *Module, rate int, options ...common.LogOption) *Player {
	common.NewDiagnostics(options...).Printf("Initializing IT playback at %d Hz", rate)
	player := &Player{module: module, rate: max(1000, rate), speed: module.speed, tempo: module.tempo, global: module.globalVolume, nextOrder: -1, channels: make([]channel, module.channels)}
	for index := range player.channels {
		track := &player.channels[index]
		track.player = player
		track.buffer = common.MakeAudioBuffer(player.rate * 2)
		track.scope = common.MakeAudioBuffer(player.rate / 5)
		track.readBuffer = make([]float32, player.rate*2)
		track.volume = 64
		track.channelVolume = min(64, int(module.channelVolume[index]))
		pan := module.pan[index] & 127
		track.pan = 128
		track.surround = pan == 100
		if pan <= 64 {
			track.pan = int(pan) * 4
		}
		track.mute.Store(module.pan[index]&128 != 0)
	}
	player.order = player.findOrder(0)
	return player
}

var emptyPattern = pattern{rows: make([][maxChannels]Note, 64)}

func (player *Player) pattern() *pattern {
	index := player.GetPattern()
	if index < 0 || index >= len(player.module.patterns) {
		return &emptyPattern
	}
	return &player.module.patterns[index]
}

func (player *Player) findOrder(order int) int {
	for order < len(player.module.orders) {
		switch player.module.orders[order] {
		case 255:
			return len(player.module.orders)
		case 254:
			order++
		default:
			return order
		}
	}
	return len(player.module.orders)
}

func (player *Player) beginTick() {
	if player.tick == 0 {
		player.delay, player.extraTicks = 0, 0
		if player.onRow != nil {
			player.onRow(player.row)
		}
		row := player.pattern().rows[player.row]
		for index := range player.channels {
			player.channels[index].beginRow(row[index])
		}
	}
	for index := range player.channels {
		player.channels[index].updateTick(player.tick)
	}
	for index := range player.channels {
		player.channels[index].prepareVoices()
	}
	player.framesLeft = max(1, player.rate*5/(2*player.tempo))
}

func (player *Player) renderFrames(frames int) {
	for index := range player.channels {
		player.channels[index].render(frames)
	}
	player.framesLeft -= frames
	if player.framesLeft != 0 {
		return
	}
	player.tick++
	if player.tick < (player.speed+player.extraTicks)*(player.delay+1) {
		return
	}
	player.tick = 0
	old := player.order
	if player.nextOrder >= 0 {
		player.order, player.row = player.findOrder(player.nextOrder), player.nextRow
	} else {
		player.row++
		if player.row >= len(player.pattern().rows) {
			player.order, player.row = player.findOrder(player.order+1), 0
		}
	}
	player.nextOrder, player.nextRow = -1, 0
	if player.row >= len(player.pattern().rows) {
		player.row = 0
	}
	if old != player.order {
		for index := range player.channels {
			player.channels[index].loopCount, player.channels[index].loopRow = 0, 0
		}
		if player.order < len(player.module.orders) && player.onOrder != nil {
			player.onOrder(player.order, player.GetPattern())
		}
	}
}

func (player *Player) Update(seconds float32) {
	player.frameRemainder += float64(seconds) * float64(player.rate)
	frames := int(math.Round(player.frameRemainder))
	player.frameRemainder -= float64(frames)
	for frames > 0 {
		if player.order >= len(player.module.orders) {
			player.seek(0)
			if player.order >= len(player.module.orders) {
				return
			}
		}
		if player.framesLeft == 0 {
			player.beginTick()
		}
		chunk := min(frames, player.framesLeft)
		player.renderFrames(chunk)
		frames -= chunk
	}
}

// RenderToPCM renders one pass as interleaved little-endian stereo float32.
func (player *Player) RenderToPCM() io.Reader {
	var samples, mix []float32
	var pcm, pending []byte
	seen := make(map[[2]int]bool)
	return &common.ReaderFunc{Func: func(dst []byte) (int, error) {
		if len(dst) == 0 {
			return 0, nil
		}
		if len(pending) == 0 {
			if player.order >= len(player.module.orders) {
				return 0, io.EOF
			}
			if player.framesLeft == 0 {
				loop := false
				for index := range player.channels {
					loop = loop || player.channels[index].loopCount != 0
				}
				if player.tick == 0 && !loop {
					row := [2]int{player.order, player.row}
					if seen[row] {
						return 0, io.EOF
					}
					seen[row] = true
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
			player.renderFrames(frames)
			for index := range player.channels {
				track := &player.channels[index]
				count := track.buffer.Read(samples)
				if !track.mute.Load() {
					for index, value := range samples[:count] {
						mix[index] += value
					}
				}
			}
			for index, value := range mix {
				binary.LittleEndian.PutUint32(pcm[index*4:], math.Float32bits(value))
			}
			pending = pcm
		}
		copied := copy(dst, pending)
		pending = pending[copied:]
		return copied, nil
	}}
}

func (player *Player) GetName() string { return player.module.name }
func (player *Player) GetPattern() int {
	if player.order < 0 || player.order >= len(player.module.orders) {
		return -1
	}
	return int(player.module.orders[player.order])
}
func (player *Player) GetCurrentOrder() int    { return player.order }
func (player *Player) GetSongLength() int      { return len(player.module.orders) }
func (player *Player) GetChannelCount() int    { return len(player.channels) }
func (player *Player) GetSpeed() int           { return player.speed }
func (player *Player) GetBPM() int             { return player.tempo }
func (player *Player) IsStereo() bool          { return true }
func (player *Player) GetPatternRowCount() int { return len(player.pattern().rows) }

func (player *Player) GetRowNoteInfo(ch, row int) (common.NoteInfo, bool) {
	pat := player.pattern()
	if ch < 0 || ch >= len(player.channels) || row < 0 || row >= len(pat.rows) {
		return nil, false
	}
	return pat.rows[row][ch], true
}
func (player *Player) GetChannelReaders() []io.Reader {
	out := make([]io.Reader, len(player.channels))
	for index := range player.channels {
		out[index] = &player.channels[index]
	}
	return out
}
func (player *Player) GetChannelData(ch int, dst []float32) int {
	if ch < 0 || ch >= len(player.channels) {
		return 0
	}
	return player.channels[ch].scope.Peek(dst)
}

// GetSpectrumChannel copies recent, unmuted output as stereo sample pairs.
func (player *Player) GetSpectrumChannel(index int, dst []float32) int {
	if index < 0 || index >= len(player.channels) || player.channels[index].mute.Load() {
		return 0
	}
	return player.channels[index].scope.PeekLatest(dst[:len(dst)&^1])
}

func (player *Player) ToggleMuteChannel(ch int) bool {
	if ch < 0 || ch >= len(player.channels) {
		return false
	}
	mute := &player.channels[ch].mute
	for {
		old := mute.Load()
		if mute.CompareAndSwap(old, !old) {
			return !old
		}
	}
}
func (player *Player) SetOnChangeRow(callback func(int))        { player.onRow = callback }
func (player *Player) SetOnChangeOrder(callback func(int, int)) { player.onOrder = callback }
func (player *Player) SetOnChangeSpeed(callback func(int, int)) { player.onSpeed = callback }
func (player *Player) NextOrder()                               { player.seek(player.findOrder(player.order + 1)) }
func (player *Player) PreviousOrder() {
	order := player.order - 1
	for order > 0 && player.module.orders[order] >= 254 {
		order--
	}
	player.seek(max(0, order))
}
func (player *Player) ResetRow() { player.seek(player.order) }
func (player *Player) seek(order int) {
	old := player.order
	player.order = player.findOrder(order)
	if player.order >= len(player.module.orders) {
		player.order = player.findOrder(0)
	}
	player.row, player.tick, player.framesLeft, player.nextOrder, player.nextRow = 0, 0, 0, -1, 0
	for index := range player.channels {
		track := &player.channels[index]
		track.voice = voice{}
		track.tails = track.tails[:0]
		track.loopCount, track.loopRow = 0, 0
		track.buffer.Clear()
	}
	if old != player.order && player.onOrder != nil && player.order < len(player.module.orders) {
		player.onOrder(player.order, player.GetPattern())
	}
}

func (track *channel) Read(dst []byte) (int, error) {
	count := min(len(track.readBuffer), len(dst)/8*2)
	part := track.readBuffer[:count]
	read := track.buffer.Read(part)
	if track.mute.Load() {
		clear(part[:read])
	}
	for index, value := range part[:read] {
		binary.LittleEndian.PutUint32(dst[index*4:], math.Float32bits(value))
	}
	if read == 0 && runtime.GOOS == "js" && len(dst) >= 8 {
		clear(dst[:8])
		return 8, nil
	}
	return read * 4, nil
}

type channel struct {
	player                                                        *Player
	buffer, scope                                                 *common.AudioBuffer
	readBuffer                                                    []float32
	mute                                                          atomic.Bool
	voice                                                         voice
	tails                                                         []voice
	note                                                          Note
	instrument, volume, channelVolume, pan                        int
	surround                                                      bool
	memory                                                        [27]int
	porta, target                                                 float64
	vibratoPhase, vibratoRate, vibratoDepth, retrigger            int
	loopRow, loopCount, highOffset                                int
	panbrelloPhase, panbrelloSpeed, panbrelloDepth, panbrelloWave int
	savedPan                                                      int
	savedSurround, restorePan                                     bool
	panbrelloNoise                                                uint32
	nnaOverride                                                   int
}
