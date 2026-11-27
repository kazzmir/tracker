package xm

import (
	"encoding/binary"
	"io"
	"math"
	"runtime"

	"github.com/kazzmir/tracker/common"
)

type Player struct {
	XMFile                                    *XMFile
	SampleRate, Order, CurrentRow, BPM, Speed int
	GlobalVolume                              int
	Channels                                  []*Channel
	OnChangeRow                               func(int)
	OnChangeOrder                             func(int, int)
	OnChangeSpeed                             func(int, int)
	tick, framesLeft                          int
	frameRemainder                            float64
	nextOrder, breakRow, rowDelay             int
}

func MakePlayer(file *XMFile, sampleRate int) *Player {
	p := &Player{XMFile: file, SampleRate: sampleRate, BPM: int(file.BPM), Speed: int(file.Tempo), GlobalVolume: 64, nextOrder: -1}
	if p.BPM == 0 {
		p.BPM = 125
	}
	if p.Speed == 0 {
		p.Speed = 6
	}
	for i := range file.Channels {
		p.Channels = append(p.Channels, &Channel{player: p, Channel: i, AudioBuffer: common.MakeAudioBuffer(sampleRate * 2), ScopeBuffer: common.MakeAudioBuffer(max(2, sampleRate/5)), buffer: make([]float32, sampleRate), pan: 128, fade: 65536})
	}
	return p
}

func (p *Player) beginTick() {
	if p.tick == 0 {
		p.rowDelay = 0
		if p.OnChangeRow != nil {
			p.OnChangeRow(p.CurrentRow)
		}
		for _, c := range p.Channels {
			n, _ := p.GetRowNote(c.Channel, p.CurrentRow)
			c.note = Note{}
			if n != nil {
				c.note = *n
			}
			c.updateRow()
		}
	}
	for _, c := range p.Channels {
		c.updateTick(p.tick)
	}
	p.framesLeft = max(1, p.SampleRate*5/(2*p.BPM))
}

func (p *Player) renderFrames(frames int) {
	for _, c := range p.Channels {
		c.render(frames)
	}
	p.framesLeft -= frames
	if p.framesLeft != 0 {
		return
	}
	p.tick++
	if p.tick < p.Speed*(1+p.rowDelay) {
		return
	}
	p.tick = 0
	old := p.Order
	if p.nextOrder >= 0 {
		p.Order, p.CurrentRow = p.nextOrder, p.breakRow
	} else {
		p.CurrentRow++
		if p.CurrentRow >= int(p.XMFile.GetPattern(p.Order).Rows) {
			p.Order++
			p.CurrentRow = 0
		}
	}
	p.nextOrder, p.breakRow = -1, 0
	wrapped := p.Order >= p.GetSongLength()
	if wrapped {
		p.Order = 0
	}
	if p.CurrentRow >= int(p.XMFile.GetPattern(p.Order).Rows) {
		p.CurrentRow = 0
	}
	if wrapped || old != p.Order {
		for _, c := range p.Channels {
			c.loopRow, c.loopCount = 0, 0
		}
		if p.OnChangeOrder != nil {
			p.OnChangeOrder(p.Order, p.GetPattern())
		}
	}
}

func (p *Player) Update(seconds float32) {
	if p.GetSongLength() == 0 {
		return
	}
	p.frameRemainder += float64(seconds) * float64(p.SampleRate)
	frames := int(math.Round(p.frameRemainder))
	p.frameRemainder -= float64(frames)
	for frames > 0 {
		if p.framesLeft == 0 {
			p.beginTick()
		}
		count := min(frames, p.framesLeft)
		p.renderFrames(count)
		frames -= count
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
			if p.GetSongLength() == 0 {
				return 0, io.EOF
			}
			if p.framesLeft == 0 {
				if p.tick == 0 && !p.inPatternLoop() {
					row := [2]int{p.Order, p.CurrentRow}
					if seen[row] {
						return 0, io.EOF
					}
					seen[row] = true
				}
				p.beginTick()
			}
			frames := p.framesLeft
			if cap(samples) < 2*frames {
				samples = make([]float32, 2*frames)
				mix = make([]float32, 2*frames)
				pcm = make([]byte, 8*frames)
			}
			samples, mix, pcm = samples[:2*frames], mix[:2*frames], pcm[:8*frames]
			clear(mix)
			p.renderFrames(frames)
			for _, c := range p.Channels {
				n := c.AudioBuffer.Read(samples)
				if !c.Mute {
					for i, s := range samples[:n] {
						mix[i] += s
					}
				}
			}
			for i, s := range mix {
				binary.LittleEndian.PutUint32(pcm[4*i:], math.Float32bits(s))
			}
			pending = pcm
		}
		n := copy(dst, pending)
		pending = pending[n:]
		return n, nil
	}}
}

func (p *Player) inPatternLoop() bool {
	for _, c := range p.Channels {
		if c.loopCount != 0 {
			return true
		}
	}
	return false
}

func (c *Channel) Read(dst []byte) (int, error) {
	if c.Mute {
		clear(dst)
		c.AudioBuffer.Clear()
		return len(dst), nil
	}
	n := c.AudioBuffer.Read(c.buffer[:min(len(c.buffer), len(dst)/8*2)])
	for i, s := range c.buffer[:n] {
		binary.LittleEndian.PutUint32(dst[4*i:], math.Float32bits(s))
	}
	if n == 0 && runtime.GOOS == "js" && len(dst) >= 8 {
		clear(dst[:8])
		return 8, nil
	}
	return n * 4, nil
}

func (p *Player) GetChannelReaders() []io.Reader {
	readers := make([]io.Reader, len(p.Channels))
	for i, c := range p.Channels {
		readers[i] = c
	}
	return readers
}
func (p *Player) GetSpeed() int   { return p.Speed }
func (p *Player) GetBPM() int     { return p.BPM }
func (p *Player) GetName() string { return p.XMFile.Name }
func (p *Player) GetInstrument(i int) *Instrument {
	if i < 0 || i >= len(p.XMFile.Instruments) {
		return nil
	}
	return p.XMFile.Instruments[i]
}
func (p *Player) GetRowNote(channel, row int) (*Note, bool) {
	pattern := p.XMFile.GetPattern(p.Order)
	if pattern == nil {
		return nil, false
	}
	notes := pattern.GetRow(row, p.XMFile.Channels)
	if channel < 0 || channel >= len(notes) {
		return nil, false
	}
	return &notes[channel], true
}
func (p *Player) GetRowNoteInfo(channel, row int) (common.NoteInfo, bool) {
	return p.GetRowNote(channel, row)
}
func (p *Player) GetCurrentOrder() int              { return p.Order }
func (p *Player) SetOnChangeRow(f func(int))        { p.OnChangeRow = f }
func (p *Player) SetOnChangeOrder(f func(int, int)) { p.OnChangeOrder = f }
func (p *Player) SetOnChangeSpeed(f func(int, int)) { p.OnChangeSpeed = f }
func (p *Player) GetChannelCount() int              { return len(p.Channels) }
func (p *Player) GetPattern() int {
	if p.GetSongLength() == 0 {
		return 0
	}
	return int(p.XMFile.Orders[p.Order])
}
func (p *Player) GetSongLength() int { return len(p.XMFile.Orders) }
func (p *Player) IsStereo() bool     { return true }
func (p *Player) ToggleMuteChannel(i int) bool {
	if i < 0 || i >= len(p.Channels) {
		return false
	}
	c := p.Channels[i]
	c.Mute = !c.Mute
	return c.Mute
}
func (p *Player) NextOrder() {
	if p.GetSongLength() > 0 {
		p.seek((p.Order + 1) % p.GetSongLength())
	}
}
func (p *Player) PreviousOrder() {
	if p.GetSongLength() > 0 {
		p.seek((p.Order + p.GetSongLength() - 1) % p.GetSongLength())
	}
}
func (p *Player) seek(order int) {
	p.Order = order
	p.ResetRow()
	if p.OnChangeOrder != nil {
		p.OnChangeOrder(order, p.GetPattern())
	}
}
func (p *Player) GetChannelData(i int, dst []float32) int {
	if i < 0 || i >= len(p.Channels) {
		return 0
	}
	return p.Channels[i].ScopeBuffer.Peek(dst)
}
func (p *Player) ResetRow() {
	p.CurrentRow = 0
	p.tick = 0
	p.framesLeft = 0
	p.nextOrder = -1
	p.breakRow = 0
}
