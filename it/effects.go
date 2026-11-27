package it

import "math"

func effect(letter byte) byte { return letter - 'A' + 1 }

func (c *channel) beginRow(note Note) {
	c.note = note
	fx := note.command
	if fx < 1 || fx > 26 {
		return
	}
	x := int(note.param)
	switch fx {
	case effect('D'), effect('K'), effect('L'):
		c.remember(effect('D'), x)
	case effect('E'), effect('F'), effect('G'):
		// Compatible Gxx keeps its own memory; otherwise E/F/G share it.
		if fx != effect('G') || c.player.module.flags&32 == 0 {
			fx = effect('E')
		}
		c.remember(fx, x)
	case effect('H'), effect('U'):
		if x>>4 != 0 {
			c.vibratoRate = x >> 4
		}
		if x&15 != 0 {
			c.vibratoDepth = x & 15
		}
	case effect('N'), effect('O'), effect('P'), effect('Q'), effect('S'), effect('T'), effect('W'), effect('J'):
		c.remember(fx, x)
	}
}
func (c *channel) remember(fx byte, x int) {
	if x != 0 {
		c.memory[fx] = x
	}
}

func volumeSlide(x int, first bool) int {
	hi, lo := x>>4, x&15
	switch {
	case lo == 15 && hi != 0:
		if first {
			return hi
		}
	case hi == 15 && lo != 0:
		if first {
			return -lo
		}
	case lo == 0:
		if !first || hi == 15 {
			return hi
		}
	case hi == 0:
		if !first || lo == 15 {
			return -lo
		}
	}
	return 0
}

func linearSlide(frequency float64, amount int) float64 {
	if frequency <= 0 || amount == 0 {
		return frequency
	}
	if amount >= 16 || amount <= -16 {
		amount = amount / 4 * 4
	}
	ratio := math.Round(65536 * math.Exp2(float64(amount)/768))
	result := math.Round(frequency * ratio / 65536)
	if result == frequency {
		if amount > 0 {
			return frequency + 1
		}
		return max(1, frequency-1)
	}
	return result
}

func (c *channel) pitchSlide(amount float64) {
	if c.porta <= 0 {
		return
	}
	if c.player.module.flags&8 != 0 {
		c.porta = linearSlide(c.porta, int(amount))
	} else {
		c.porta = 14317456 / max(1, 14317456/c.porta-amount)
	}
	c.porta = max(1, min(1500000, c.porta))
}

func (c *channel) updateTick(tick int) {
	p, n := c.player, c.note
	fx, x := n.command, int(n.param)
	first := tick == 0
	delay := 0
	s := c.memory[effect('S')]
	if fx == effect('S') && s>>4 == 13 {
		delay = max(1, s&15)
	}
	porta := fx == effect('G') || fx == effect('L') || n.fields&hasVolume != 0 && n.volume >= 193 && n.volume <= 202
	if tick == delay && delay < p.speed+p.extraTicks {
		c.trigger(porta)
		if n.fields&hasVolume != 0 {
			switch {
			case n.volume <= 64:
				c.volume = int(n.volume)
			case n.volume >= 128 && n.volume <= 192:
				c.pan = (int(n.volume) - 128) * 4
				c.surround = false
			}
		}
	}
	if first {
		switch fx {
		case effect('A'):
			if x > 0 {
				p.speed = x
				if p.onSpeed != nil {
					p.onSpeed(p.speed, p.tempo)
				}
			}
		case effect('B'):
			p.nextOrder = x
			p.nextRow = 0
		case effect('C'):
			if p.nextOrder < 0 {
				p.nextOrder = p.order + 1
			}
			p.nextRow = x
		case effect('M'):
			if x <= 64 {
				c.channelVolume = x
			}
		case effect('S'):
			c.special(s)
		case effect('V'):
			if x <= 128 {
				p.global = x
			}
		case effect('X'):
			c.pan = x
			c.surround = false
		}
	}
	switch fx {
	case effect('D'), effect('K'), effect('L'):
		c.volume += volumeSlide(c.memory[effect('D')], first)
	case effect('N'):
		c.channelVolume += volumeSlide(c.memory[fx], first)
	case effect('P'):
		c.pan -= 4 * volumeSlide(c.memory[fx], first)
		c.surround = false
	case effect('W'):
		p.global = max(0, min(128, p.global+volumeSlide(c.memory[fx], first)))
	case effect('E'), effect('F'):
		v := c.memory[effect('E')]
		amount := 0
		switch {
		case v >= 240 && first:
			amount = (v & 15) * 4
		case v >= 224 && v < 240 && first:
			amount = v & 15
		case v < 224 && !first:
			amount = v * 4
		}
		if fx == effect('E') {
			amount = -amount
		}
		c.pitchSlide(float64(amount))
	case effect('Q'):
		v := c.memory[fx]
		if v&15 != 0 {
			if c.retrigger >= v&15 && c.voice.sample != nil {
				c.voice.position = 0
				c.voice.backward = false
				c.retrigger = 0
				c.volume = retriggerVolume(c.volume, v>>4)
			}
			c.retrigger++
		}
	case effect('S'):
		if s>>4 == 12 && tick == max(1, s&15) {
			c.voice.action(0)
		}
	case effect('T'):
		x = c.memory[fx]
		old := p.tempo
		if x >= 32 && first {
			p.tempo = x
		} else if x < 32 && !first {
			if x >= 16 {
				p.tempo = min(255, p.tempo+(x&15))
			} else {
				p.tempo = max(32, p.tempo-x)
			}
		}
		if old != p.tempo && p.onSpeed != nil {
			p.onSpeed(p.speed, p.tempo)
		}
	}
	if n.fields&hasVolume != 0 {
		v := int(n.volume)
		switch {
		case v >= 65 && v <= 74:
			if first {
				c.remember(0, v-65)
				c.volume += c.memory[0]
			}
		case v >= 75 && v <= 84:
			if first {
				c.remember(0, v-75)
				c.volume -= c.memory[0]
			}
		case v >= 85 && v <= 94:
			if first {
				c.remember(0, v-85)
			} else {
				c.volume += c.memory[0]
			}
		case v >= 95 && v <= 104:
			if first {
				c.remember(0, v-95)
			} else {
				c.volume -= c.memory[0]
			}
		case v >= 105 && v <= 114:
			if first {
				c.remember(effect('E'), v-105)
			} else {
				c.pitchSlide(-float64(c.memory[effect('E')] * 16))
			}
		case v >= 115 && v <= 124:
			if first {
				c.remember(effect('E'), v-115)
			} else {
				c.pitchSlide(float64(c.memory[effect('E')] * 16))
			}
		case v >= 193 && v <= 202:
			if first && v > 193 {
				key := effect('E')
				if p.module.flags&32 != 0 {
					key = effect('G')
				}
				c.memory[key] = [...]int{0, 1, 4, 8, 16, 32, 64, 96, 128, 255}[v-193]
			}
		case v > 203 && v <= 212:
			if first {
				c.vibratoDepth = v - 203
			}
		}
	}
	if porta && !first && c.target > 0 && c.porta > 0 {
		key := effect('E')
		if p.module.flags&32 != 0 {
			key = effect('G')
		}
		amount := float64(c.memory[key] * 4)
		if c.porta < c.target {
			c.pitchSlide(amount)
			c.porta = min(c.porta, c.target)
		} else {
			c.pitchSlide(-amount)
			c.porta = max(c.porta, c.target)
		}
	}
	c.volume = max(0, min(64, c.volume))
	c.channelVolume = max(0, min(64, c.channelVolume))
	c.pan = max(0, min(256, c.pan))
	c.voice.volume, c.voice.channelVolume, c.voice.pan, c.voice.surround = c.volume, c.channelVolume, c.pan, c.surround
	freq := c.porta
	if fx == effect('H') || fx == effect('U') || fx == effect('K') || (n.fields&hasVolume != 0 && n.volume >= 203 && n.volume <= 212) {
		old := p.module.flags&16 != 0
		if !old || !first {
			c.vibratoPhase = (c.vibratoPhase + c.vibratoRate*4) & 255
		}
		wave := math.Round(math.Sin(float64(c.vibratoPhase)*math.Pi/128) * 64)
		depth := float64(c.vibratoDepth * 4)
		divisor := 64.0
		if old {
			divisor = 32
			wave = -wave
		}
		if fx == effect('U') {
			depth = float64(c.vibratoDepth)
		}
		amount := math.Trunc(wave * depth / divisor)
		if p.module.flags&8 != 0 {
			freq = linearSlide(freq, int(amount))
		} else if freq > 0 {
			freq = 14317456 / max(1, 14317456/freq-amount)
		}
	}
	if fx == effect('J') {
		v := c.memory[fx]
		if tick%3 == 1 {
			freq *= math.Exp2(float64(v>>4) / 12)
		} else if tick%3 == 2 {
			freq *= math.Exp2(float64(v&15) / 12)
		}
	}
	c.voice.frequency = freq
}

func (c *channel) prepareVoices() {
	p := c.player
	c.voice.setup(p)
	tails := c.tails[:0]
	for i := range c.tails {
		v := &c.tails[i]
		v.setup(p)
		if v.sample != nil {
			tails = append(tails, *v)
		}
	}
	c.tails = tails
}

func retriggerVolume(vol, code int) int {
	switch code {
	case 1, 2, 3, 4, 5:
		return vol - (1 << (code - 1))
	case 6:
		return vol * 2 / 3
	case 7:
		return vol / 2
	case 9, 10, 11, 12, 13:
		return vol + (1 << (code - 9))
	case 14:
		return vol * 3 / 2
	case 15:
		return vol * 2
	}
	return vol
}

func (c *channel) special(x int) {
	p := c.player
	v := x & 15
	switch x >> 4 {
	case 6:
		p.extraTicks += v
	case 7:
		switch {
		case v <= 2:
			for i := range c.tails {
				c.tails[i].action([...]int{0, 2, 3}[v])
			}
		case v <= 6:
			c.nnaOverride = v - 2
		case v <= 12:
			mask := byte(1 << ((v - 7) / 2))
			if v%2 == 0 {
				c.voice.envEnabled |= mask
			} else {
				c.voice.envEnabled &^= mask
			}
		}
	case 8:
		c.pan = (v*256 + 8) / 15
		c.surround = false
	case 9:
		if v <= 1 {
			c.surround = v == 1
		}
	case 10:
		c.highOffset = v
	case 11:
		if v == 0 {
			c.loopRow = p.row
		} else {
			if c.loopCount == 0 {
				c.loopCount = v
			} else {
				c.loopCount--
				if c.loopCount == 0 {
					c.loopRow = p.row + 1
					return
				}
			}
			p.nextOrder, p.nextRow = p.order, c.loopRow
		}
	case 14:
		if p.delay == 0 {
			p.delay = v
		}
	}
}
