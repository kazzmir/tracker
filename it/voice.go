package it

import "math"

type voice struct {
	sample                                       *sample
	instrument                                   *instrument
	key, mapped, volume, pan, channelVolume      int
	panOffset                                    int
	surround, released, fading, backward         bool
	position, frequency, step                    float64
	envPos                                       [3]int
	envEnabled                                   byte
	vibratoPhase, vibratoDepth                   int
	vibratoNoise                                 uint32
	fade                                         int
	leftGain, rightGain, leftTarget, rightTarget float32
	ramp                                         int
}

func (env envelope) enabled() bool { return env.flags&1 != 0 && env.count > 0 }
func (env envelope) value(tick int) float64 {
	prev := env.points[0]
	for index := 1; index < env.count; index++ {
		next := env.points[index]
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
func (env envelope) advance(tick int, released bool) int {
	next := tick + 1
	if !released && env.flags&4 != 0 && env.susStart <= env.susEnd && env.susEnd < env.count {
		if next > env.points[env.susEnd].tick {
			return env.points[env.susStart].tick
		}
		return next
	}
	if env.flags&2 != 0 && env.loopStart <= env.loopEnd && env.loopEnd < env.count && next > env.points[env.loopEnd].tick {
		return env.points[env.loopStart].tick
	}
	return min(next, env.points[env.count-1].tick)
}

func (active *voice) resetEnvelopes() {
	active.envPos = [3]int{}
	active.envEnabled = 0
	if inst := active.instrument; inst != nil {
		for index, env := range [...]envelope{inst.volume, inst.panning, inst.pitch} {
			if env.enabled() {
				active.envEnabled |= 1 << index
			}
		}
	}
}

func (active *voice) release() {
	active.released = true
	if active.instrument == nil || active.envEnabled&1 == 0 || active.instrument.volume.flags&2 != 0 {
		active.fading = true
	}
}
func (active *voice) action(action int) {
	switch action {
	case 0:
		active.sample = nil
	case 2:
		active.release()
	case 3:
		active.fading = true
	}
}

func (track *channel) trigger(portamento bool) {
	note := track.note
	if note.fields&hasPitch != 0 && note.pitch >= 120 {
		switch note.pitch {
		case 254:
			track.voice.action(0)
		case 255:
			track.voice.release()
		default:
			track.voice.fading = true
		}
		return
	}
	if note.fields&hasInstrument != 0 && note.instrument != 0 {
		track.instrument = int(note.instrument)
	}
	key := track.voice.key
	if note.fields&hasPitch != 0 {
		key = int(note.pitch)
	}
	if track.instrument == 0 || key < 0 || key >= 120 {
		return
	}
	var inst *instrument
	mapped, index := key, track.instrument
	module := track.player.module
	if module.flags&4 != 0 {
		if track.instrument > len(module.instruments) {
			return
		}
		inst = &module.instruments[track.instrument-1]
		mapped, index = int(inst.notes[key]), int(inst.samples[key])
	}
	if index < 1 || index > len(module.samples) || mapped >= 120 {
		return
	}
	wave := &module.samples[index-1]
	if len(wave.left) == 0 {
		return
	}
	if note.fields&hasInstrument != 0 && note.instrument != 0 {
		track.volume = wave.volume
	}
	if note.fields&hasPitch == 0 {
		return
	}
	// IT's note table stores semitone ratios in 16.16 fixed point.
	ratio := math.Round(65536 * math.Exp2(float64(mapped%12)/12))
	freq := math.Floor(float64(wave.c5) * math.Ldexp(ratio/65536, mapped/12-5))
	if portamento && track.voice.sample != nil {
		track.target = freq
		if note.fields&hasInstrument != 0 && inst != track.voice.instrument {
			track.voice.instrument = inst
			track.voice.resetEnvelopes()
		}
		return
	}
	if inst != nil && inst.dct != 0 {
		for index := range track.tails {
			checkDuplicate(&track.tails[index], inst, wave, mapped)
		}
		checkDuplicate(&track.voice, inst, wave, mapped)
	}
	if track.voice.sample != nil && track.voice.instrument != nil {
		action := track.voice.instrument.nna
		if track.nnaOverride != 0 {
			action = track.nnaOverride - 1
		}
		if action != 0 {
			tail := track.voice
			tail.frequency = track.porta
			tail.action(action)
			if len(track.tails) >= 32 {
				copy(track.tails, track.tails[1:])
				track.tails = track.tails[:31]
			}
			track.tails = append(track.tails, tail)
		}
	}
	if inst != nil && inst.pan&128 == 0 {
		track.pan = min(256, (inst.pan&127)*4)
		track.surround = false
	}
	if wave.pan&128 != 0 {
		track.pan = min(256, (wave.pan&127)*4)
		track.surround = false
	}
	track.voice = voice{sample: wave, instrument: inst, key: key, mapped: mapped, fade: 65536, frequency: freq}
	track.voice.resetEnvelopes()
	track.porta, track.target = freq, 0
	track.vibratoPhase, track.retrigger = 0, 0
	track.nnaOverride = 0
	if note.command == effect('O') {
		offset := track.highOffset*65536 + track.memory[effect('O')]*256
		if offset < len(wave.left) {
			track.voice.position = float64(offset)
		} else if module.flags&16 != 0 {
			track.voice.position = float64(len(wave.left) - 1)
		} else {
			track.voice.sample = nil
		}
	}
}

func checkDuplicate(active *voice, inst *instrument, wave *sample, mapped int) {
	if active.sample == nil || active.instrument != inst {
		return
	}
	if (inst.dct == 1 && active.mapped == mapped) || (inst.dct == 2 && active.sample == wave) || inst.dct == 3 {
		active.action([...]int{0, 2, 3}[min(2, inst.dca)])
	}
}

func (active *voice) setup(player *Player) {
	if active.sample == nil {
		return
	}
	vol := float64(active.volume*active.channelVolume*active.sample.globalVolume) / (64 * 64 * 64)
	pan := float64(active.pan)
	frequency := active.frequency
	if inst := active.instrument; inst != nil {
		vol *= float64(inst.globalVolume) / 128
		for index, env := range [...]envelope{inst.volume, inst.panning, inst.pitch} {
			if env.count == 0 || active.envEnabled&(1<<index) == 0 {
				continue
			}
			value := env.value(active.envPos[index])
			next := env.advance(active.envPos[index], active.released)
			switch index {
			case 0:
				vol *= max(0, min(64, value)) / 64
				if active.envPos[index] == env.points[env.count-1].tick && next == active.envPos[index] {
					active.fading = true
					if value == 0 {
						active.sample = nil
						return
					}
				}
			case 1:
				pan += value * (128 - math.Abs(pan-128)) / 32
			case 2:
				if env.flags&128 == 0 {
					frequency *= math.Exp2(value / 24)
				}
			}
			active.envPos[index] = next
		}
		if active.fading {
			active.fade = max(0, active.fade-inst.fadeout*64)
		}
	}
	if active.fade == 0 {
		active.sample = nil
		return
	}
	vol *= float64(active.fade) / 65536 * float64(player.global) / 128 * float64(player.module.mixVolume) / 128
	pan = 128 + (pan-128)*float64(player.module.separation)/128 + float64(active.panOffset)
	pan = max(0, min(256, pan)) / 256
	if player.module.flags&1 == 0 {
		pan = .5
	}
	active.leftTarget, active.rightTarget = float32(vol*(1-pan)), float32(vol*pan)
	if active.surround && player.module.flags&1 != 0 {
		active.leftTarget = float32(vol * .5)
		active.rightTarget = -active.leftTarget
	}
	frequency = active.applySampleVibrato(frequency)
	active.step = min(65536, max(0, frequency/float64(player.rate)))
	if active.leftGain != active.leftTarget || active.rightGain != active.rightTarget {
		active.ramp = max(1, player.rate/1000)
	}
}

// Sample vibrato has independent phase and sweep state for every voice,
// including notes continuing in the background after an NNA.
func (active *voice) applySampleVibrato(frequency float64) float64 {
	wave := active.sample
	if wave.vibratoRate == 0 || wave.vibratoDepth == 0 {
		return frequency
	}
	active.vibratoDepth = min(wave.vibratoDepth*256, active.vibratoDepth+wave.vibratoSweep)
	phase := active.vibratoPhase
	active.vibratoPhase = (phase + wave.vibratoRate) & 255
	var height int
	switch wave.vibratoWave {
	case 1:
		height = 64 - (phase+1)/2
	case 2:
		if phase < 128 {
			height = 64
		}
	case 3:
		active.vibratoNoise = active.vibratoNoise*1664525 + 1013904223
		height = int(active.vibratoNoise>>25) - 64
	case 4:
		height = (phase+1)/2 - 64
	default:
		height = int(math.Round(64 * math.Sin(float64(phase)*math.Pi/128)))
	}
	amount := height * (active.vibratoDepth / 256) / 64
	return frequency * math.Exp2(float64(amount)/768)
}

func (active *voice) frame() (float32, float32) {
	wave := active.sample
	if wave == nil {
		return 0, 0
	}
	loop := wave.loop
	if !active.released && wave.sustain.end > 0 {
		loop = wave.sustain
	}
	end := len(wave.left)
	if loop.end > 0 {
		end = loop.end
	}
	if active.position >= float64(end) || active.position < 0 {
		if loop.end == 0 {
			active.sample = nil
			return 0, 0
		}
		active.position = float64(loop.start)
	}
	index := int(active.position)
	next := index + 1
	if next >= end {
		next = index
		if loop.end > 0 && !loop.pingPong {
			next = loop.start
		}
	}
	frac := float32(active.position - float64(index))
	left := wave.left[index] + (wave.left[next]-wave.left[index])*frac
	right := left
	if wave.right != nil {
		right = wave.right[index] + (wave.right[next]-wave.right[index])*frac
	}
	if active.ramp > 0 {
		active.leftGain += (active.leftTarget - active.leftGain) / float32(active.ramp)
		active.rightGain += (active.rightTarget - active.rightGain) / float32(active.ramp)
		active.ramp--
	}
	if loop.end == 0 || !loop.pingPong {
		active.backward = false
		active.position += active.step
		if loop.end > 0 && active.position >= float64(end) {
			active.position = float64(loop.start) + math.Mod(active.position-float64(loop.start), float64(end-loop.start))
		}
	} else {
		low, high := float64(loop.start), float64(end-1)
		if active.backward {
			active.position -= active.step
		} else {
			active.position += active.step
		}
		if active.position > high || active.backward && active.position < low {
			length := high - low
			phase := active.position - low
			if active.backward {
				phase = -phase
			}
			phase = math.Mod(phase, 2*length)
			if phase < 0 {
				phase += 2 * length
			}
			active.backward = phase >= length
			if active.backward {
				phase = 2*length - phase
			}
			active.position = low + phase
		}
	}
	return left * active.leftGain, right * active.rightGain
}

func (track *channel) render(frames int) {
	track.buffer.Lock()
	defer track.buffer.Unlock()
	track.scope.Lock()
	defer track.scope.Unlock()
	for range frames {
		left, right := track.voice.frame()
		for index := range track.tails {
			tailLeft, tailRight := track.tails[index].frame()
			left += tailLeft
			right += tailRight
		}
		track.buffer.UnsafeWrite(left)
		track.buffer.UnsafeWrite(right)
		track.scope.UnsafeWrite(left)
		track.scope.UnsafeWrite(right)
	}
}
