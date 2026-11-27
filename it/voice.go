package it

import "math"

type voice struct {
	sample                                       *sample
	instrument                                   *instrument
	key, mapped, volume, pan, channelVolume      int
	surround, released, fading, backward         bool
	position, frequency, step                    float64
	envPos                                       [3]int
	envEnabled                                   byte
	fade                                         int
	leftGain, rightGain, leftTarget, rightTarget float32
	ramp                                         int
}

func (e envelope) enabled() bool { return e.flags&1 != 0 && e.count > 0 }
func (e envelope) value(tick int) float64 {
	prev := e.points[0]
	for i := 1; i < e.count; i++ {
		next := e.points[i]
		if tick < next.tick {
			if tick <= prev.tick {
				return float64(prev.value)
			}
			return float64(prev.value) + float64((next.value-prev.value)*(tick-prev.tick))/float64(next.tick-prev.tick)
		}
		prev = next
	}
	return float64(prev.value)
}
func (e envelope) advance(tick int, released bool) int {
	next := tick + 1
	if !released && e.flags&4 != 0 && e.susStart <= e.susEnd && e.susEnd < e.count {
		if next > e.points[e.susEnd].tick {
			return e.points[e.susStart].tick
		}
		return next
	}
	if e.flags&2 != 0 && e.loopStart <= e.loopEnd && e.loopEnd < e.count && next > e.points[e.loopEnd].tick {
		return e.points[e.loopStart].tick
	}
	return min(next, e.points[e.count-1].tick)
}

func (v *voice) resetEnvelopes() {
	v.envPos = [3]int{}
	v.envEnabled = 0
	if i := v.instrument; i != nil {
		for n, e := range [...]envelope{i.volume, i.panning, i.pitch} {
			if e.enabled() {
				v.envEnabled |= 1 << n
			}
		}
	}
}

func (v *voice) release() {
	v.released = true
	if v.instrument == nil || v.envEnabled&1 == 0 || v.instrument.volume.flags&2 != 0 {
		v.fading = true
	}
}
func (v *voice) action(action int) {
	switch action {
	case 0:
		v.sample = nil
	case 2:
		v.release()
	case 3:
		v.fading = true
	}
}

func (c *channel) trigger(portamento bool) {
	n := c.note
	if n.fields&hasPitch != 0 && n.pitch >= 120 {
		switch n.pitch {
		case 254:
			c.voice.action(0)
		case 255:
			c.voice.release()
		default:
			c.voice.fading = true
		}
		return
	}
	if n.fields&hasInstrument != 0 && n.instrument != 0 {
		c.instrument = int(n.instrument)
	}
	key := c.voice.key
	if n.fields&hasPitch != 0 {
		key = int(n.pitch)
	}
	if c.instrument == 0 || key < 0 || key >= 120 {
		return
	}
	var inst *instrument
	mapped, index := key, c.instrument
	m := c.player.module
	if m.flags&4 != 0 {
		if c.instrument > len(m.instruments) {
			return
		}
		inst = &m.instruments[c.instrument-1]
		mapped, index = int(inst.notes[key]), int(inst.samples[key])
	}
	if index < 1 || index > len(m.samples) || mapped >= 120 {
		return
	}
	s := &m.samples[index-1]
	if len(s.left) == 0 {
		return
	}
	if n.fields&hasInstrument != 0 && n.instrument != 0 {
		c.volume = s.volume
	}
	if n.fields&hasPitch == 0 {
		return
	}
	// IT's note table stores semitone ratios in 16.16 fixed point.
	ratio := math.Round(65536 * math.Exp2(float64(mapped%12)/12))
	freq := math.Floor(float64(s.c5) * math.Ldexp(ratio/65536, mapped/12-5))
	if portamento && c.voice.sample != nil {
		c.target = freq
		if n.fields&hasInstrument != 0 && inst != c.voice.instrument {
			c.voice.instrument = inst
			c.voice.resetEnvelopes()
		}
		return
	}
	if inst != nil && inst.dct != 0 {
		for i := range c.tails {
			checkDuplicate(&c.tails[i], inst, s, mapped)
		}
		checkDuplicate(&c.voice, inst, s, mapped)
	}
	if c.voice.sample != nil && c.voice.instrument != nil {
		action := c.voice.instrument.nna
		if c.nnaOverride != 0 {
			action = c.nnaOverride - 1
		}
		if action != 0 {
			tail := c.voice
			tail.frequency = c.porta
			tail.action(action)
			if len(c.tails) >= 32 {
				copy(c.tails, c.tails[1:])
				c.tails = c.tails[:31]
			}
			c.tails = append(c.tails, tail)
		}
	}
	if inst != nil && inst.pan&128 == 0 {
		c.pan = min(256, (inst.pan&127)*4)
		c.surround = false
	}
	if s.pan&128 != 0 {
		c.pan = min(256, (s.pan&127)*4)
		c.surround = false
	}
	c.voice = voice{sample: s, instrument: inst, key: key, mapped: mapped, fade: 65536, frequency: freq}
	c.voice.resetEnvelopes()
	c.porta, c.target = freq, 0
	c.vibratoPhase, c.retrigger = 0, 0
	c.nnaOverride = 0
	if n.command == effect('O') {
		offset := c.highOffset*65536 + c.memory[effect('O')]*256
		if offset < len(s.left) {
			c.voice.position = float64(offset)
		} else if m.flags&16 != 0 {
			c.voice.position = float64(len(s.left) - 1)
		} else {
			c.voice.sample = nil
		}
	}
}

func checkDuplicate(v *voice, inst *instrument, s *sample, mapped int) {
	if v.sample == nil || v.instrument != inst {
		return
	}
	if (inst.dct == 1 && v.mapped == mapped) || (inst.dct == 2 && v.sample == s) || inst.dct == 3 {
		v.action([...]int{0, 2, 3}[min(2, inst.dca)])
	}
}

func (v *voice) setup(p *Player) {
	if v.sample == nil {
		return
	}
	vol := float64(v.volume*v.channelVolume*v.sample.globalVolume) / (64 * 64 * 64)
	pan := float64(v.pan)
	frequency := v.frequency
	if inst := v.instrument; inst != nil {
		vol *= float64(inst.globalVolume) / 128
		for i, e := range [...]envelope{inst.volume, inst.panning, inst.pitch} {
			if e.count == 0 || v.envEnabled&(1<<i) == 0 {
				continue
			}
			value := e.value(v.envPos[i])
			next := e.advance(v.envPos[i], v.released)
			switch i {
			case 0:
				vol *= max(0, min(64, value)) / 64
				if v.envPos[i] == e.points[e.count-1].tick && next == v.envPos[i] {
					v.fading = true
					if value == 0 {
						v.sample = nil
						return
					}
				}
			case 1:
				pan += value * (128 - math.Abs(pan-128)) / 32
			case 2:
				if e.flags&128 == 0 {
					frequency *= math.Exp2(value / 24)
				}
			}
			v.envPos[i] = next
		}
		if v.fading {
			v.fade = max(0, v.fade-inst.fadeout*64)
		}
	}
	if v.fade == 0 {
		v.sample = nil
		return
	}
	vol *= float64(v.fade) / 65536 * float64(p.global) / 128 * float64(p.module.mixVolume) / 128
	pan = 128 + (pan-128)*float64(p.module.separation)/128
	pan = max(0, min(256, pan)) / 256
	if p.module.flags&1 == 0 {
		pan = .5
	}
	v.leftTarget, v.rightTarget = float32(vol*(1-pan)), float32(vol*pan)
	if v.surround && p.module.flags&1 != 0 {
		v.leftTarget = float32(vol * .5)
		v.rightTarget = -v.leftTarget
	}
	v.step = min(65536, max(0, frequency/float64(p.rate)))
	if v.leftGain != v.leftTarget || v.rightGain != v.rightTarget {
		v.ramp = max(1, p.rate/1000)
	}
}

func (v *voice) frame() (float32, float32) {
	s := v.sample
	if s == nil {
		return 0, 0
	}
	loop := s.loop
	if !v.released && s.sustain.end > 0 {
		loop = s.sustain
	}
	end := len(s.left)
	if loop.end > 0 {
		end = loop.end
	}
	if v.position >= float64(end) || v.position < 0 {
		if loop.end == 0 {
			v.sample = nil
			return 0, 0
		}
		v.position = float64(loop.start)
	}
	i := int(v.position)
	next := i + 1
	if next >= end {
		next = i
		if loop.end > 0 && !loop.pingPong {
			next = loop.start
		}
	}
	frac := float32(v.position - float64(i))
	l := s.left[i] + (s.left[next]-s.left[i])*frac
	r := l
	if s.right != nil {
		r = s.right[i] + (s.right[next]-s.right[i])*frac
	}
	if v.ramp > 0 {
		v.leftGain += (v.leftTarget - v.leftGain) / float32(v.ramp)
		v.rightGain += (v.rightTarget - v.rightGain) / float32(v.ramp)
		v.ramp--
	}
	if loop.end == 0 || !loop.pingPong {
		v.backward = false
		v.position += v.step
		if loop.end > 0 && v.position >= float64(end) {
			v.position = float64(loop.start) + math.Mod(v.position-float64(loop.start), float64(end-loop.start))
		}
	} else {
		low, high := float64(loop.start), float64(end-1)
		if v.backward {
			v.position -= v.step
		} else {
			v.position += v.step
		}
		if v.position > high || v.backward && v.position < low {
			length := high - low
			phase := v.position - low
			if v.backward {
				phase = -phase
			}
			phase = math.Mod(phase, 2*length)
			if phase < 0 {
				phase += 2 * length
			}
			v.backward = phase >= length
			if v.backward {
				phase = 2*length - phase
			}
			v.position = low + phase
		}
	}
	return l * v.leftGain, r * v.rightGain
}

func (c *channel) render(n int) {
	c.buffer.Lock()
	defer c.buffer.Unlock()
	c.scope.Lock()
	defer c.scope.Unlock()
	for range n {
		l, r := c.voice.frame()
		for i := range c.tails {
			a, b := c.tails[i].frame()
			l += a
			r += b
		}
		c.buffer.UnsafeWrite(l)
		c.buffer.UnsafeWrite(r)
		c.scope.UnsafeWrite(l)
		c.scope.UnsafeWrite(r)
	}
}
