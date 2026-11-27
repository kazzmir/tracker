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

func MakePlayer(m *Module, rate int) *Player {
	p := &Player{module: m, rate: max(1000, rate), speed: m.speed, tempo: m.tempo, global: m.globalVolume, nextOrder: -1, channels: make([]channel, m.channels)}
	for i := range p.channels {
		c := &p.channels[i]
		c.player = p
		c.buffer = common.MakeAudioBuffer(p.rate * 2)
		c.scope = common.MakeAudioBuffer(p.rate / 5)
		c.readBuffer = make([]float32, p.rate*2)
		c.volume = 64
		c.channelVolume = min(64, int(m.channelVolume[i]))
		pan := m.pan[i] & 127
		c.pan = 128
		c.surround = pan == 100
		if pan <= 64 {
			c.pan = int(pan) * 4
		}
		c.mute.Store(m.pan[i]&128 != 0)
	}
	p.order = p.findOrder(0)
	return p
}

var emptyPattern = pattern{rows: make([][maxChannels]Note, 64)}

func (p *Player) pattern() *pattern {
	index := p.GetPattern()
	if index < 0 || index >= len(p.module.patterns) {
		return &emptyPattern
	}
	return &p.module.patterns[index]
}

func (p *Player) findOrder(order int) int {
	for order < len(p.module.orders) {
		switch p.module.orders[order] {
		case 255:
			return len(p.module.orders)
		case 254:
			order++
		default:
			return order
		}
	}
	return len(p.module.orders)
}

func (p *Player) beginTick() {
	if p.tick == 0 {
		p.delay, p.extraTicks = 0, 0
		if p.onRow != nil {
			p.onRow(p.row)
		}
		row := p.pattern().rows[p.row]
		for i := range p.channels {
			p.channels[i].beginRow(row[i])
		}
	}
	for i := range p.channels {
		p.channels[i].updateTick(p.tick)
	}
	for i := range p.channels {
		p.channels[i].prepareVoices()
	}
	p.framesLeft = max(1, p.rate*5/(2*p.tempo))
}

func (p *Player) renderFrames(frames int) {
	for i := range p.channels {
		p.channels[i].render(frames)
	}
	p.framesLeft -= frames
	if p.framesLeft != 0 {
		return
	}
	p.tick++
	if p.tick < (p.speed+p.extraTicks)*(p.delay+1) {
		return
	}
	p.tick = 0
	old := p.order
	if p.nextOrder >= 0 {
		p.order, p.row = p.findOrder(p.nextOrder), p.nextRow
	} else {
		p.row++
		if p.row >= len(p.pattern().rows) {
			p.order, p.row = p.findOrder(p.order+1), 0
		}
	}
	p.nextOrder, p.nextRow = -1, 0
	if p.row >= len(p.pattern().rows) {
		p.row = 0
	}
	if old != p.order {
		for i := range p.channels {
			p.channels[i].loopCount, p.channels[i].loopRow = 0, 0
		}
		if p.order < len(p.module.orders) && p.onOrder != nil {
			p.onOrder(p.order, p.GetPattern())
		}
	}
}

func (p *Player) Update(seconds float32) {
	p.frameRemainder += float64(seconds) * float64(p.rate)
	frames := int(math.Round(p.frameRemainder))
	p.frameRemainder -= float64(frames)
	for frames > 0 {
		if p.order >= len(p.module.orders) {
			p.seek(0)
			if p.order >= len(p.module.orders) {
				return
			}
		}
		if p.framesLeft == 0 {
			p.beginTick()
		}
		n := min(frames, p.framesLeft)
		p.renderFrames(n)
		frames -= n
	}
}

// RenderToPCM renders one pass as interleaved little-endian stereo float32.
func (p *Player) RenderToPCM() io.Reader {
	var samples, mix []float32
	var pcm, pending []byte
	seen := make(map[[2]int]bool)
	return &common.ReaderFunc{Func: func(dst []byte) (int, error) {
		if len(dst) == 0 {
			return 0, nil
		}
		if len(pending) == 0 {
			if p.order >= len(p.module.orders) {
				return 0, io.EOF
			}
			if p.framesLeft == 0 {
				loop := false
				for i := range p.channels {
					loop = loop || p.channels[i].loopCount != 0
				}
				if p.tick == 0 && !loop {
					row := [2]int{p.order, p.row}
					if seen[row] {
						return 0, io.EOF
					}
					seen[row] = true
				}
				p.beginTick()
			}
			n := p.framesLeft
			if cap(samples) < n*2 {
				samples = make([]float32, n*2)
				mix = make([]float32, n*2)
				pcm = make([]byte, n*8)
			}
			samples, mix, pcm = samples[:n*2], mix[:n*2], pcm[:n*8]
			clear(mix)
			p.renderFrames(n)
			for i := range p.channels {
				c := &p.channels[i]
				count := c.buffer.Read(samples)
				if !c.mute.Load() {
					for i, v := range samples[:count] {
						mix[i] += v
					}
				}
			}
			for i, v := range mix {
				binary.LittleEndian.PutUint32(pcm[i*4:], math.Float32bits(v))
			}
			pending = pcm
		}
		n := copy(dst, pending)
		pending = pending[n:]
		return n, nil
	}}
}

func (p *Player) GetName() string { return p.module.name }
func (p *Player) GetPattern() int {
	if p.order < 0 || p.order >= len(p.module.orders) {
		return -1
	}
	return int(p.module.orders[p.order])
}
func (p *Player) GetCurrentOrder() int { return p.order }
func (p *Player) GetSongLength() int   { return len(p.module.orders) }
func (p *Player) GetChannelCount() int { return len(p.channels) }
func (p *Player) GetSpeed() int        { return p.speed }
func (p *Player) GetBPM() int          { return p.tempo }
func (p *Player) IsStereo() bool       { return true }
func (p *Player) GetRowNoteInfo(ch, row int) (common.NoteInfo, bool) {
	pat := p.pattern()
	if ch < 0 || ch >= len(p.channels) || row < 0 || row >= len(pat.rows) {
		return nil, false
	}
	return pat.rows[row][ch], true
}
func (p *Player) GetChannelReaders() []io.Reader {
	out := make([]io.Reader, len(p.channels))
	for i := range p.channels {
		out[i] = &p.channels[i]
	}
	return out
}
func (p *Player) GetChannelData(ch int, dst []float32) int {
	if ch < 0 || ch >= len(p.channels) {
		return 0
	}
	return p.channels[ch].scope.Peek(dst)
}
func (p *Player) ToggleMuteChannel(ch int) bool {
	if ch < 0 || ch >= len(p.channels) {
		return false
	}
	m := &p.channels[ch].mute
	for {
		old := m.Load()
		if m.CompareAndSwap(old, !old) {
			return !old
		}
	}
}
func (p *Player) SetOnChangeRow(f func(int))        { p.onRow = f }
func (p *Player) SetOnChangeOrder(f func(int, int)) { p.onOrder = f }
func (p *Player) SetOnChangeSpeed(f func(int, int)) { p.onSpeed = f }
func (p *Player) NextOrder()                        { p.seek(p.findOrder(p.order + 1)) }
func (p *Player) PreviousOrder() {
	o := p.order - 1
	for o > 0 && p.module.orders[o] >= 254 {
		o--
	}
	p.seek(max(0, o))
}
func (p *Player) ResetRow() { p.seek(p.order) }
func (p *Player) seek(o int) {
	old := p.order
	p.order = p.findOrder(o)
	if p.order >= len(p.module.orders) {
		p.order = p.findOrder(0)
	}
	p.row, p.tick, p.framesLeft, p.nextOrder, p.nextRow = 0, 0, 0, -1, 0
	for i := range p.channels {
		c := &p.channels[i]
		c.voice = voice{}
		c.tails = c.tails[:0]
		c.loopCount, c.loopRow = 0, 0
		c.buffer.Clear()
	}
	if old != p.order && p.onOrder != nil && p.order < len(p.module.orders) {
		p.onOrder(p.order, p.GetPattern())
	}
}

func (c *channel) Read(dst []byte) (int, error) {
	count := min(len(c.readBuffer), len(dst)/8*2)
	part := c.readBuffer[:count]
	n := c.buffer.Read(part)
	if c.mute.Load() {
		clear(part[:n])
	}
	for i, s := range part[:n] {
		binary.LittleEndian.PutUint32(dst[i*4:], math.Float32bits(s))
	}
	if n == 0 && runtime.GOOS == "js" && len(dst) >= 8 {
		clear(dst[:8])
		return 8, nil
	}
	return n * 4, nil
}

type channel struct {
	player                                             *Player
	buffer, scope                                      *common.AudioBuffer
	readBuffer                                         []float32
	mute                                               atomic.Bool
	voice                                              voice
	tails                                              []voice
	note                                               Note
	instrument, volume, channelVolume, pan             int
	surround                                           bool
	memory                                             [27]int
	porta, target                                      float64
	vibratoPhase, vibratoRate, vibratoDepth, retrigger int
	loopRow, loopCount, highOffset                     int
	nnaOverride                                        int
}
