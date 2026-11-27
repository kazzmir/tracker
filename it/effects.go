package it

import "math"

func effect(letter byte) byte { return letter - 'A' + 1 }

func (track *channel) beginRow(note Note) {
	track.note = note
	fx := note.command
	if fx < 1 || fx > 26 {
		return
	}
	param := int(note.param)
	switch fx {
	case effect('D'), effect('K'), effect('L'):
		track.remember(effect('D'), param)
	case effect('E'), effect('F'), effect('G'):
		// Compatible Gxx keeps its own memory; otherwise E/F/G share it.
		if fx != effect('G') || track.player.module.flags&32 == 0 {
			fx = effect('E')
		}
		track.remember(fx, param)
	case effect('H'), effect('U'):
		if param>>4 != 0 {
			track.vibratoRate = param >> 4
		}
		if param&15 != 0 {
			track.vibratoDepth = param & 15
		}
	case effect('Y'):
		if param>>4 != 0 {
			track.panbrelloSpeed = param >> 4
		}
		if param&15 != 0 {
			track.panbrelloDepth = param & 15
		}
	case effect('N'), effect('O'), effect('P'), effect('Q'), effect('S'), effect('T'), effect('W'), effect('J'):
		track.remember(fx, param)
	}
}
func (track *channel) remember(fx byte, param int) {
	if param != 0 {
		track.memory[fx] = param
	}
}

func volumeSlide(param int, first bool) int {
	hi, lo := param>>4, param&15
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

func (track *channel) pitchSlide(amount float64) {
	if track.porta <= 0 || amount == 0 {
		return
	}
	if track.player.module.flags&8 != 0 {
		track.porta = linearSlide(track.porta, int(amount))
	} else {
		// IT stores the result of an Amiga slide as whole Hz each tick.
		const clock = 1712 * 8363
		track.porta = math.Floor(clock * track.porta / max(1, clock-track.porta*amount))
	}
	track.porta = max(1, min(1500000, track.porta))
}

func (track *channel) updateTick(tick int) {
	player, note := track.player, track.note
	fx, param := note.command, int(note.param)
	first := tick == 0
	delay := 0
	specialParam := track.memory[effect('S')]
	if fx == effect('S') && specialParam>>4 == 13 {
		delay = max(1, specialParam&15)
	}
	porta := fx == effect('G') || fx == effect('L') || note.fields&hasVolume != 0 && note.volume >= 193 && note.volume <= 202
	if tick == delay && delay < player.speed+player.extraTicks {
		track.trigger(porta)
		if note.fields&hasVolume != 0 {
			switch {
			case note.volume <= 64:
				track.volume = int(note.volume)
			case note.volume >= 128 && note.volume <= 192:
				track.pan = (int(note.volume) - 128) * 4
				track.surround = false
			}
		}
	}
	if first {
		switch fx {
		case effect('A'):
			if param > 0 {
				player.speed = param
				if player.onSpeed != nil {
					player.onSpeed(player.speed, player.tempo)
				}
			}
		case effect('B'):
			player.nextOrder = param
			player.nextRow = 0
		case effect('C'):
			if player.nextOrder < 0 {
				player.nextOrder = player.order + 1
			}
			player.nextRow = param
		case effect('M'):
			if param <= 64 {
				track.channelVolume = param
			}
		case effect('S'):
			track.special(specialParam)
		case effect('V'):
			if param <= 128 {
				player.global = param
			}
		case effect('X'):
			track.pan = param
			track.surround = false
		}
	}
	switch fx {
	case effect('D'), effect('K'), effect('L'):
		track.volume += volumeSlide(track.memory[effect('D')], first)
	case effect('N'):
		track.channelVolume += volumeSlide(track.memory[fx], first)
	case effect('P'):
		track.pan -= 4 * volumeSlide(track.memory[fx], first)
		track.surround = false
	case effect('W'):
		player.global = max(0, min(128, player.global+volumeSlide(track.memory[fx], first)))
	case effect('E'), effect('F'):
		slide := track.memory[effect('E')]
		amount := 0
		switch {
		case slide >= 240 && first:
			amount = (slide & 15) * 4
		case slide >= 224 && slide < 240 && first:
			amount = slide & 15
		case slide < 224 && !first:
			amount = slide * 4
		}
		if fx == effect('E') {
			amount = -amount
		}
		track.pitchSlide(float64(amount))
	case effect('Q'):
		retrig := track.memory[fx]
		if retrig&15 != 0 {
			if track.retrigger >= retrig&15 && track.voice.sample != nil {
				track.voice.position = 0
				track.voice.backward = false
				track.retrigger = 0
				track.volume = retriggerVolume(track.volume, retrig>>4)
			}
			track.retrigger++
		}
	case effect('S'):
		if specialParam>>4 == 12 && tick == max(1, specialParam&15) {
			track.voice.action(0)
		}
	case effect('T'):
		param = track.memory[fx]
		old := player.tempo
		if param >= 32 && first {
			player.tempo = param
		} else if param < 32 && !first {
			if param >= 16 {
				player.tempo = min(255, player.tempo+(param&15))
			} else {
				player.tempo = max(32, player.tempo-param)
			}
		}
		if old != player.tempo && player.onSpeed != nil {
			player.onSpeed(player.speed, player.tempo)
		}
	}
	if note.fields&hasVolume != 0 {
		column := int(note.volume)
		switch {
		case column >= 65 && column <= 74:
			if first {
				track.remember(0, column-65)
				track.volume += track.memory[0]
			}
		case column >= 75 && column <= 84:
			if first {
				track.remember(0, column-75)
				track.volume -= track.memory[0]
			}
		case column >= 85 && column <= 94:
			if first {
				track.remember(0, column-85)
			} else {
				track.volume += track.memory[0]
			}
		case column >= 95 && column <= 104:
			if first {
				track.remember(0, column-95)
			} else {
				track.volume -= track.memory[0]
			}
		case column >= 105 && column <= 114:
			if first {
				track.remember(effect('E'), column-105)
			} else {
				track.pitchSlide(-float64(track.memory[effect('E')] * 16))
			}
		case column >= 115 && column <= 124:
			if first {
				track.remember(effect('E'), column-115)
			} else {
				track.pitchSlide(float64(track.memory[effect('E')] * 16))
			}
		case column >= 193 && column <= 202:
			if first && column > 193 {
				key := effect('E')
				if player.module.flags&32 != 0 {
					key = effect('G')
				}
				track.memory[key] = [...]int{0, 1, 4, 8, 16, 32, 64, 96, 128, 255}[column-193]
			}
		case column > 203 && column <= 212:
			if first {
				track.vibratoDepth = column - 203
			}
		}
	}
	if porta && !first && track.target > 0 && track.porta > 0 {
		key := effect('E')
		if player.module.flags&32 != 0 {
			key = effect('G')
		}
		amount := float64(track.memory[key] * 4)
		if track.porta < track.target {
			track.pitchSlide(amount)
			track.porta = min(track.porta, track.target)
		} else {
			track.pitchSlide(-amount)
			track.porta = max(track.porta, track.target)
		}
	}
	track.volume = max(0, min(64, track.volume))
	track.channelVolume = max(0, min(64, track.channelVolume))
	track.pan = max(0, min(256, track.pan))
	track.voice.volume, track.voice.channelVolume, track.voice.pan, track.voice.surround = track.volume, track.channelVolume, track.pan, track.surround
	freq := track.porta
	if fx == effect('H') || fx == effect('U') || fx == effect('K') || (note.fields&hasVolume != 0 && note.volume >= 203 && note.volume <= 212) {
		old := player.module.flags&16 != 0
		if !old || !first {
			track.vibratoPhase = (track.vibratoPhase + track.vibratoRate*4) & 255
		}
		wave := math.Round(math.Sin(float64(track.vibratoPhase)*math.Pi/128) * 64)
		depth := float64(track.vibratoDepth * 4)
		divisor := 64.0
		if old {
			divisor = 32
			wave = -wave
		}
		if fx == effect('U') {
			depth = float64(track.vibratoDepth)
		}
		amount := math.Trunc(wave * depth / divisor)
		if player.module.flags&8 != 0 {
			freq = linearSlide(freq, int(amount))
		} else if freq > 0 {
			freq = 14317456 / max(1, 14317456/freq-amount)
		}
	}
	if fx == effect('J') {
		arp := track.memory[fx]
		if tick%3 == 1 {
			freq *= math.Exp2(float64(arp>>4) / 12)
		} else if tick%3 == 2 {
			freq *= math.Exp2(float64(arp&15) / 12)
		}
	}
	track.voice.frequency = freq
	track.voice.panOffset = 0
	if fx == effect('Y') {
		phase := track.panbrelloPhase
		var wave int
		switch track.panbrelloWave {
		case 1:
			wave = 64 - (phase+1)/2
		case 2:
			if phase < 128 {
				wave = 64
			}
		case 3:
			track.panbrelloNoise = track.panbrelloNoise*1664525 + 1013904223
			wave = int(track.panbrelloNoise>>25) - 64
		default:
			wave = int(math.Round(64 * math.Sin(float64(phase)*math.Pi/128)))
		}
		track.voice.panOffset = (wave*track.panbrelloDepth + 2) / 8
		track.panbrelloPhase = (phase + track.panbrelloSpeed) & 255
	}
}

func (track *channel) prepareVoices() {
	player := track.player
	track.voice.setup(player)
	tails := track.tails[:0]
	for index := range track.tails {
		tail := &track.tails[index]
		tail.setup(player)
		if tail.sample != nil {
			tails = append(tails, *tail)
		}
	}
	track.tails = tails
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

func (track *channel) special(param int) {
	player := track.player
	subcode := param & 15
	switch param >> 4 {
	case 5:
		track.panbrelloWave = subcode & 3
		track.panbrelloPhase = 0
	case 6:
		player.extraTicks += subcode
	case 7:
		switch {
		case subcode <= 2:
			for index := range track.tails {
				track.tails[index].action([...]int{0, 2, 3}[subcode])
			}
		case subcode <= 6:
			track.nnaOverride = subcode - 2
		case subcode <= 12:
			mask := byte(1 << ((subcode - 7) / 2))
			if subcode%2 == 0 {
				track.voice.envEnabled |= mask
			} else {
				track.voice.envEnabled &^= mask
			}
		}
	case 8:
		track.pan = (subcode*256 + 8) / 15
		track.surround = false
	case 9:
		if subcode <= 1 {
			track.surround = subcode == 1
		}
	case 10:
		track.highOffset = subcode
	case 11:
		if subcode == 0 {
			track.loopRow = player.row
		} else {
			if track.loopCount == 0 {
				track.loopCount = subcode
			} else {
				track.loopCount--
				if track.loopCount == 0 {
					track.loopRow = player.row + 1
					return
				}
			}
			player.nextOrder, player.nextRow = player.order, track.loopRow
		}
	case 14:
		if player.delay == 0 {
			player.delay = subcode
		}
	}
}
