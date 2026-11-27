package xm

import (
	"math"

	"github.com/kazzmir/tracker/common"
)

type Channel struct {
	player                   *Player
	Channel                  int
	AudioBuffer, ScopeBuffer *common.AudioBuffer
	buffer                   []float32
	Mute                     bool

	note                                            Note
	instrument, voice                               *Instrument
	sample                                          *Sample
	position, period, target                        float64
	fineTune                                        int
	volume, pan                                     int
	released                                        bool
	fade, volumeTick, panTick, autoPhase, autoDepth int

	slideUp, slideDown, tone, slideVolume, sampleOffset, panSlide, retrig int
	vibratoSpeed, vibratoDepth, vibratoPhase                              int
	tremoloSpeed, tremoloDepth, tremoloPhase                              int
	loopRow, loopCount                                                    int

	increment                            float64
	left, right, targetLeft, targetRight float32
	ramp                                 int
}

func (e Envelope) enabled() bool { return e.Count > 0 && e.Flags&1 != 0 }
func (e Envelope) value(tick, fallback int) float64 {
	if !e.enabled() {
		return float64(fallback)
	}
	for i := 1; i < int(e.Count); i++ {
		a, b := int(e.Points[2*i-2]), int(e.Points[2*i])
		if tick < b {
			va, vb := float64(e.Points[2*i-1]), float64(e.Points[2*i+1])
			if tick <= a {
				return va
			}
			return va + (vb-va)*float64(tick-a)/float64(b-a)
		}
	}
	return float64(e.Points[2*int(e.Count)-1])
}
func (e Envelope) advance(tick int, released bool) int {
	if !e.enabled() {
		return tick
	}
	next := tick + 1
	sustain := e.Flags&2 != 0 && e.Sustain < e.Count
	if e.Flags&4 != 0 && e.LoopStart <= e.LoopEnd && e.LoopEnd < e.Count &&
		next == int(e.Points[2*e.LoopEnd]) && !(released && sustain && e.Sustain == e.LoopEnd) {
		next = int(e.Points[2*e.LoopStart])
	}
	if sustain && !released && next == int(e.Points[2*e.Sustain])+1 {
		return next - 1
	}
	return min(next, int(e.Points[2*int(e.Count)-2]))
}

func (c *Channel) notePeriod(note, relative, tune int) float64 {
	n := note - 1 + relative
	if c.player.XMFile.LinearPeriods {
		return float64(7680 - 64*n - tune/2)
	}
	return float64(amigaPeriod(n, tune))
}

func (c *Channel) release() {
	c.released = true
	if c.voice == nil || !c.voice.VolumeEnvelope.enabled() {
		c.volume = 0
	}
}

func (c *Channel) trigger() {
	n := c.note
	porta := n.EffectType == EffectTonePortamento || n.EffectType == EffectTonePortamentoVolumeSlide || n.Volume>>4 == 15
	if n.Instrument > 0 {
		c.instrument = c.player.GetInstrument(int(n.Instrument) - 1)
	}
	if n.NoteNumber == 97 {
		c.release()
		return
	}
	if n.NoteNumber > 0 && n.NoteNumber <= 96 && c.instrument != nil && len(c.instrument.Samples) > 0 {
		sampleIndex := int(c.instrument.Keymap[n.NoteNumber-1])
		if sampleIndex >= len(c.instrument.Samples) {
			sampleIndex = 0
		}
		s := &c.instrument.Samples[sampleIndex]
		if porta && c.sample != nil {
			s = c.sample
		}
		tune := int(s.FineTune) &^ 7 // FastTracker truncates the low three bits.
		if n.EffectType == EffectExtended && n.EffectParameter>>4 == ExtendedEffectSetFinetune {
			tune = (int(n.EffectParameter&15) - 8) * 16
		}
		period := c.notePeriod(int(n.NoteNumber), int(s.RelativeNoteNumber), tune)
		if porta && c.sample != nil {
			c.target = period
		} else {
			c.sample, c.voice = s, c.instrument
			c.period, c.target = period, 0
			c.fineTune = tune
			c.position = 0
			c.vibratoPhase, c.tremoloPhase = 0, 0
			c.left, c.right = 0, 0
			if n.EffectType == EffectSetSampleOffset {
				offset := c.sampleOffset * 256
				// XM offsets count decoded sample frames, including for 16-bit data.
				c.position = float64(offset)
				if offset >= len(s.Data) {
					c.sample = nil
				}
			}
		}
	}
	if n.Instrument > 0 && c.sample != nil {
		c.volume, c.pan = int(min(c.sample.Volume, 64)), int(c.sample.Panning)
	}
	if c.sample != nil && (n.Instrument > 0 || (n.NoteNumber > 0 && !porta)) {
		c.fade, c.volumeTick, c.panTick = 65536, 0, 0
		c.autoPhase, c.autoDepth = 0, 0
		c.released = false
	}
}

func remember(dst *int, value int) {
	if value != 0 {
		*dst = value
	}
}
func (c *Channel) updateRow() {
	p, n := c.player, c.note
	x := int(n.EffectParameter)
	switch n.EffectType {
	case EffectPortamentoUp:
		remember(&c.slideUp, x)
	case EffectPortamentoDown:
		remember(&c.slideDown, x)
	case EffectTonePortamento:
		remember(&c.tone, x)
	case EffectVibrato:
		remember(&c.vibratoSpeed, x>>4)
		remember(&c.vibratoDepth, x&15)
	case EffectTonePortamentoVolumeSlide, EffectVibratoVolumeSlide, EffectVolumeSlide:
		remember(&c.slideVolume, x)
	case EffectTremolo:
		remember(&c.tremoloSpeed, x>>4)
		remember(&c.tremoloDepth, x&15)
	case EffectSetSampleOffset:
		remember(&c.sampleOffset, x)
	case EffectPositionJump:
		p.nextOrder = x
		p.breakRow = 0
	case EffectPatternBreak:
		if p.nextOrder < 0 {
			p.nextOrder = p.Order + 1
		}
		p.breakRow = (x>>4)*10 + (x & 15)
	case EffectExtended:
		switch x >> 4 {
		case ExtendedEffectSetLoop:
			if x&15 == 0 {
				c.loopRow = p.CurrentRow
			} else {
				if c.loopCount == 0 {
					c.loopCount = x & 15
				} else {
					c.loopCount--
					if c.loopCount == 0 {
						break
					}
				}
				p.nextOrder, p.breakRow = p.Order, c.loopRow
			}
		case ExtendedEffectPatternDelay:
			p.rowDelay = x & 15
		}
	case EffectSetSpeed:
		if x > 0 {
			if x < 32 {
				p.Speed = x
			} else {
				p.BPM = x
			}
			if p.OnChangeSpeed != nil {
				p.OnChangeSpeed(p.Speed, p.BPM)
			}
		}
	case EffectSetGlobalVolume:
		p.GlobalVolume = min(x, 64)
	case EffectPanningSlide:
		remember(&c.panSlide, x)
	case EffectMultiRetrigger:
		remember(&c.retrig, x)
	}
}

// FastTracker's integer waveform rounds differently from a floating-point sine.
var vibratoSine = [...]int{0, 12, 25, 37, 49, 60, 71, 81, 90, 98, 106, 112, 117, 122, 125, 126, 127, 126, 125, 122, 117, 112, 106, 98, 90, 81, 71, 60, 49, 37, 25, 12}

func vibratoDelta(phase int) int {
	if phase&32 != 0 {
		return -vibratoSine[phase&31]
	}
	return vibratoSine[phase&31]
}

func (c *Channel) updateTick(tick int) {
	n := c.note
	x, v := int(n.EffectParameter), int(n.Volume)
	delay := 0
	if n.EffectType == EffectExtended && x>>4 == ExtendedEffectNoteDelay {
		delay = x & 15
	}
	if tick == delay && delay < c.player.Speed {
		c.trigger()
		// Direct volume and pan belong to the delayed note, not to the
		// still-playing voice on the first tick of the row.
		switch {
		case v >= 0x10 && v <= 0x50:
			c.volume = v - 16
		case v>>4 == 12:
			c.pan = (v & 15) * 16
		}
	}
	if tick == 0 {
		switch {
		case v>>4 == 8:
			c.volume -= v & 15
		case v>>4 == 9:
			c.volume += v & 15
		case v>>4 == 10:
			remember(&c.vibratoSpeed, v&15)
		case v>>4 == 11:
			remember(&c.vibratoDepth, v&15)
		case v>>4 == 15:
			remember(&c.tone, (v&15)*16)
		}
		switch n.EffectType {
		case EffectSetPanning:
			c.pan = x
		case EffectSetVolume:
			c.volume = x
		case EffectKeyOff:
			if x == 0 {
				c.release()
			}
		case EffectEnvelopePosition:
			c.volumeTick = x
			c.panTick = x
		case EffectExtraFinePortamento:
			switch x >> 4 {
			case 1:
				c.period -= float64(x & 15)
			case 2:
				c.period += float64(x & 15)
			}
		case EffectExtended:
			switch x >> 4 {
			case ExtendedEffectFinePortamentoUp:
				c.period -= float64((x & 15) * 4)
			case ExtendedEffectFinePortamentoDown:
				c.period += float64((x & 15) * 4)
			case ExtendedEffectFineVolumeSlideUp:
				c.volume += x & 15
			case ExtendedEffectFineVolumeSlideDown:
				c.volume -= x & 15
			}
		}
	} else {
		switch v >> 4 {
		case 6:
			c.volume -= v & 15
		case 7:
			c.volume += v & 15
		case 13:
			c.pan -= v & 15
		case 14:
			c.pan += v & 15
		}
		if n.EffectType == EffectTonePortamento || n.EffectType == EffectTonePortamentoVolumeSlide || v>>4 == 15 {
			if c.target > 0 && c.period > 0 {
				step := float64(c.tone * 4)
				c.period += max(-step, min(step, c.target-c.period))
			}
		}
		switch n.EffectType {
		case EffectPortamentoUp:
			c.period -= float64(4 * c.slideUp)
		case EffectPortamentoDown:
			c.period += float64(4 * c.slideDown)
		case EffectTonePortamentoVolumeSlide, EffectVibratoVolumeSlide, EffectVolumeSlide:
			if c.slideVolume>>4 > 0 {
				c.volume += c.slideVolume >> 4
			} else {
				c.volume -= c.slideVolume & 15
			}
		case EffectSetGlobalVolumeSlide:
			c.player.GlobalVolume = max(0, min(64, c.player.GlobalVolume+(x>>4)-(x&15)))
		case EffectPanningSlide:
			c.pan += (c.panSlide >> 4) - (c.panSlide & 15)
		}
	}
	if n.EffectType == EffectKeyOff && x == tick && tick > 0 {
		c.release()
	}
	if n.EffectType == EffectExtended {
		if x>>4 == ExtendedEffectNoteCut && x&15 == tick {
			c.volume = 0
		}
		if x>>4 == ExtendedEffectRetriggerNote && x&15 != 0 && tick > 0 && tick%(x&15) == 0 {
			c.position = 0
			c.left, c.right = 0, 0
		}
	}
	if n.EffectType == EffectMultiRetrigger && c.retrig&15 != 0 && tick > 0 && tick%(c.retrig&15) == 0 {
		c.position = 0
		switch c.retrig >> 4 {
		case 1, 2, 3, 4, 5:
			c.volume -= 1 << ((c.retrig >> 4) - 1)
		case 6:
			c.volume = c.volume * 2 / 3
		case 7:
			c.volume /= 2
		case 9, 10, 11, 12, 13:
			c.volume += 1 << ((c.retrig >> 4) - 9)
		case 14:
			c.volume = c.volume * 3 / 2
		case 15:
			c.volume *= 2
		}
	}
	c.volume = max(0, min(64, c.volume))
	c.pan = max(0, min(255, c.pan))
	if c.sample == nil || c.voice == nil {
		return
	}
	c.period = max(1, c.period)
	period := c.period
	if n.EffectType == EffectVibrato || n.EffectType == EffectVibratoVolumeSlide || v>>4 == 11 {
		period += float64(vibratoDelta(c.vibratoPhase) * c.vibratoDepth / 16)
		if tick > 0 {
			c.vibratoPhase = (c.vibratoPhase + c.vibratoSpeed) & 63
		}
	}
	if n.EffectType == EffectArpeggio && x != 0 {
		semitone := 0
		// FT2 indexes the arpeggio table with the row's tick countdown.
		arp := 0
		if tick != 0 {
			arp = c.player.Speed - tick%c.player.Speed
			if arp > 16 {
				arp = 2
			} else if arp == 16 {
				arp = 0
			}
		}
		switch arp % 3 {
		case 1:
			semitone = x >> 4
		case 2:
			semitone = x & 15
		}
		if arp%3 != 0 {
			// FT2 finds the nearest note before adding the arpeggio offset.
			// This matters when a preceding slide left a fractional semitone.
			low, high := 0, 118
			for low < high {
				mid := (low + high) / 2
				if c.notePeriod(mid+1, 0, c.fineTune+64) > period {
					low = mid + 1
				} else {
					high = mid
				}
			}
			period = c.notePeriod(min(low+semitone, 118)+1, 0, c.fineTune)
			if low+semitone >= 96 {
				period = max(period, c.notePeriod(97, 0, 0))
			}
		}
	}
	inst := c.voice
	if inst.VibratoDepth > 0 {
		period += float64(c.autoVibrato(inst))
	}
	freq := 8363 * 1712 / max(1, period)
	if c.player.XMFile.LinearPeriods {
		freq = 8363 * math.Exp2((4608-period)/768)
	}
	c.increment = freq / float64(c.player.SampleRate)
	vol := float64(c.volume)
	if n.EffectType == EffectTremolo && tick > 0 {
		vol += math.Sin(float64(c.tremoloPhase)*math.Pi/32) * float64(c.tremoloDepth) * 2
		c.tremoloPhase = (c.tremoloPhase + c.tremoloSpeed) & 63
	}
	vol = max(0, min(64, vol)) / 64 * inst.VolumeEnvelope.value(c.volumeTick, 64) / 64 * float64(c.fade) / 65536 * float64(c.player.GlobalVolume) / 64
	pan := float64(c.pan) + (inst.PanEnvelope.value(c.panTick, 32)-32)*(128-math.Abs(float64(c.pan)-128))/32
	pan = max(0, min(255, pan)) / 256
	left, right := 1-pan, pan
	if !c.player.XMFile.linearPanning {
		left, right = math.Sqrt(left), math.Sqrt(right)
	}
	const gain = 3.0 / 8 // headroom for the channel sum
	c.targetLeft, c.targetRight = float32(vol*left*gain), float32(vol*right*gain)
	if c.targetLeft != c.left || c.targetRight != c.right {
		c.ramp = max(1, c.player.SampleRate/200)
	}
	c.volumeTick = inst.VolumeEnvelope.advance(c.volumeTick, c.released)
	c.panTick = inst.PanEnvelope.advance(c.panTick, c.released)
	if c.released {
		c.fade = max(0, c.fade-int(inst.Fadeout)*2)
	}
}

// FastTracker's instrument vibrato sweeps in with 8-bit fractional depth.
func (c *Channel) autoVibrato(inst *Instrument) int {
	fullDepth := int(inst.VibratoDepth) * 256
	depth := fullDepth
	if sweep := int(inst.VibratoSweep); sweep > 0 {
		step := fullDepth / sweep
		if !c.released {
			c.autoDepth += step
		}
		depth = min(c.autoDepth, fullDepth)
		// Releasing before the sweep completes leaves only its first step.
		if c.released && c.autoDepth <= fullDepth {
			depth = step
		}
	}
	c.autoPhase = (c.autoPhase + int(inst.VibratoRate)) & 255
	phase := c.autoPhase
	var wave int
	switch inst.VibratoType {
	case 1:
		wave = -64
		if phase >= 128 {
			wave = 64
		}
	case 2:
		wave = ((64 + phase/2) & 127) - 64
	case 3:
		wave = ((64 - phase/2) & 127) - 64
	default:
		wave = -int(math.Round(64 * math.Sin(float64(phase)*math.Pi/128)))
	}
	return wave * depth / (64 * 256)
}

func (c *Channel) render(frames int) {
	c.AudioBuffer.Lock()
	defer c.AudioBuffer.Unlock()
	c.ScopeBuffer.Lock()
	defer c.ScopeBuffer.Unlock()
	var data []float32
	var kind, start, end int
	if s := c.sample; s != nil {
		data = s.Data
		end = len(data)
		start = int(s.LoopStart)
		loopEnd := min(end, start+int(s.LoopLength))
		if s.Type&3 != 0 && start >= 0 && loopEnd-start >= 2 {
			end, kind = loopEnd, int(s.Type&3)
		}
	}
	for range frames {
		if c.ramp > 0 {
			c.left += (c.targetLeft - c.left) / float32(c.ramp)
			c.right += (c.targetRight - c.right) / float32(c.ramp)
			c.ramp--
		}
		var value float32
		if end > 0 && (kind == 2 || c.position < float64(end)) {
			pos := c.position
			if kind == 2 && pos >= float64(end-1) {
				pos = min(float64(end-1), float64(2*end)-pos)
			}
			i := int(pos)
			next := min(i+1, end-1)
			if kind == 1 && i == end-1 {
				next = start
			}
			value = data[i] + float32(pos-float64(i))*(data[next]-data[i])
		}
		if end > 0 {
			c.position += c.increment
			if kind == 1 && c.position >= float64(end) {
				length := float64(end - start)
				c.position = float64(start) + math.Mod(c.position-float64(start), length)
			} else if kind == 2 {
				cycle := float64(2 * (end - start))
				if c.position >= float64(start)+cycle {
					c.position = float64(start) + math.Mod(c.position-float64(start), cycle)
				}
			}
		}
		l, r := value*c.left, value*c.right
		c.AudioBuffer.UnsafeWrite(l)
		c.AudioBuffer.UnsafeWrite(r)
		c.ScopeBuffer.UnsafeWrite(l)
		c.ScopeBuffer.UnsafeWrite(r)
	}
}
