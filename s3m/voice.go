package s3m

import (
	"math"
	"sync/atomic"

	"github.com/kazzmir/tracker/common"
)

type channel struct {
	player                                               *Player
	buffer, scope                                        *common.AudioBuffer
	readBuffer                                           []float32
	mute                                                 atomic.Bool
	note                                                 Note
	selected, volume, pan                                int
	voice                                                *Instrument
	position                                             float64
	period, target, toneSpeed                            int
	memory, parameter, offset, previousOffset, retrigger int
	vibratoPhase, vibratoRate, vibratoDepth, vibratoWave int
	tremoloPhase, tremoloRate, tremoloDepth, tremoloWave int
	loopRow, loopCount, tremorCount                      int
	surround                                             bool
	step                                                 float64
	left, right, targetLeft, targetRight                 float32
	ramp                                                 int
}

func notePeriod(note int, instrument *Instrument) int {
	if note < 0 || note >= 160 || note&15 >= 12 {
		return 0
	}
	periods := [...]int{1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907}
	c4 := 8363
	if instrument != nil && instrument.MiddleC != 0 {
		c4 = int(instrument.MiddleC)
	}
	return int((int64(periods[note&15]) * 8363 * 16) / (int64(c4) << uint(note>>4)))
}

func (track *channel) trigger(portamento bool) {
	note := track.note
	if note.ChangeSample {
		track.selected = note.SampleNumber - 1
		if instrument := track.player.GetInstrument(track.selected); instrument != nil && len(instrument.Data) > 0 {
			track.volume = int(instrument.Volume)
		}
	}
	if note.ChangeNote {
		if note.Note == 254 {
			track.voice = nil
			track.period = 0
			return
		}
		instrument := track.player.GetInstrument(track.selected)
		period := notePeriod(note.Note, instrument)
		if period > 0 {
			if portamento && track.period > 0 && track.voice != nil {
				track.target = period
				if instrument != nil && len(instrument.Data) > 0 {
					track.voice = instrument
				}
			} else {
				track.period = period
				track.voice = instrument
				track.position = 0
				if track.vibratoWave < 4 {
					track.vibratoPhase = 0
				}
				if track.tremoloWave < 4 {
					track.tremoloPhase = 0
				}
				track.left, track.right = 0, 0
				track.retrigger = 0
				if note.ChangeSample {
					track.previousOffset = 0
				}
				if note.EffectNumber == EffectSampleOffset {
					track.previousOffset += track.offset * 256
				}
				track.position = float64(track.previousOffset)
			}
		}
	} else if note.ChangeSample {
		if instrument := track.player.GetInstrument(track.selected); instrument != nil && len(instrument.Data) > 0 {
			track.voice = instrument
		}
	}
	if note.ChangeVolume {
		if note.Volume >= 128 && note.Volume <= 192 {
			track.pan = (note.Volume - 128) * 4
			track.surround = false
		} else {
			track.volume = note.Volume
		}
	}
}

func (track *channel) beginRow(note Note) {
	track.note = note
	effect, param := int(note.EffectNumber), int(note.EffectParameter)
	if !note.ChangeEffect {
		track.parameter = 0
		return
	}
	track.parameter = param
	switch effect {
	case EffectPortamentoToNote:
		if param != 0 {
			track.toneSpeed = param
		}
	case EffectVibrato, EffectFineVibrato:
		if param>>4 != 0 {
			track.vibratoRate = param >> 4
		}
		if param&15 != 0 {
			track.vibratoDepth = param & 15
		}
	case EffectSampleOffset:
		if param != 0 {
			track.offset = param
		}
	case EffectVolumeSlide, EffectPortamentoDown, EffectPortamentoUp, EffectTremor, EffectArpeggio, EffectVibratoAndVolumeSlide, EffectPortamentoAndVolumeSlide, EffectRetriggerAndVolumeSlide, EffectTremolo, EffectSetExtra:
		if param == 0 {
			track.parameter = track.memory
		}
	}
	if effect != 0 && param != 0 {
		track.memory = param
		track.tremoloRate = param >> 4
		track.tremoloDepth = param & 15
	}
}

func (track *channel) updateTick(tick int) {
	player := track.player
	effect, param := int(track.note.EffectNumber), track.parameter
	first := tick == 0
	delay := 0
	if effect == EffectSetExtra && param>>4 == 13 {
		delay = param & 15
		if delay == 0 {
			return
		}
	}
	if tick == delay && delay < player.Speed+player.extraTicks {
		track.trigger(effect == EffectPortamentoToNote || effect == EffectPortamentoAndVolumeSlide)
	}
	if first {
		switch effect {
		case EffectSetSpeed:
			if param != 0 {
				player.Speed = param
				player.speedChanged()
			}
		case EffectSetTempo:
			if param >= 32 {
				player.BPM = param
				player.speedChanged()
			}
		case EffectPatternJump:
			player.nextOrder = param
			player.breakRow = 0
		case EffectPatternBreak:
			if player.nextOrder < 0 {
				player.nextOrder = player.CurrentOrder + 1
			}
			player.breakRow = min(63, (param>>4)*10+(param&15))
		case EffectGlobalVolume:
			if param <= 64 {
				player.GlobalVolume = uint8(param)
			}
		case EffectSetExtra:
			track.special(param)
		case 24:
			if param <= 128 {
				track.pan = param * 2
				track.surround = false
			} else if param == 164 {
				track.surround = true
			}
		}
	}
	switch effect {
	case EffectVolumeSlide, EffectVibratoAndVolumeSlide, EffectPortamentoAndVolumeSlide:
		hi, lo := param>>4, param&15
		switch {
		case lo == 15 && hi != 0:
			if first && effect == EffectVolumeSlide {
				track.volume += hi
			}
		case hi == 15 && lo != 0:
			if first && effect == EffectVolumeSlide {
				track.volume -= lo
			}
		case lo == 0:
			if !first || player.S3M.FastVolumeSlides || hi == 15 {
				track.volume += hi
			}
		default:
			if !first || player.S3M.FastVolumeSlides || lo == 15 {
				track.volume -= lo
			}
		}
	case EffectPortamentoDown, EffectPortamentoUp:
		if track.period > 0 {
			amount := 0
			switch {
			case first && param >= 240:
				amount = (param & 15) * 4
			case first && param >= 224:
				amount = param & 15
			case !first && param < 224:
				amount = param * 4
			}
			if effect == EffectPortamentoUp {
				amount = -amount
			}
			track.period += amount
			if track.period < 1 {
				track.voice = nil
				track.period = 0
			}
		}
	case EffectSetExtra:
		if param>>4 == 12 && tick == max(1, param&15) {
			track.volume = 0
		}
	case EffectRetriggerAndVolumeSlide:
		interval := param & 15
		if interval != 0 {
			if track.retrigger >= interval {
				track.position = 0
				track.retrigger = 0
				track.volume = retriggerVolume(track.volume, param>>4)
			}
			track.retrigger++
		}
	case EffectTremor:
		track.tremorCount++
	}
	if !first && (effect == EffectPortamentoToNote || effect == EffectPortamentoAndVolumeSlide) && track.target > 0 && track.period > 0 {
		step := track.toneSpeed * 4
		track.period += max(-step, min(step, track.target-track.period))
	}
	track.volume = max(0, min(track.volume, 64))
}

func (player *Player) speedChanged() {
	if player.OnChangeSpeed != nil {
		player.OnChangeSpeed(player.Speed, player.BPM)
	}
}

func (track *channel) special(param int) {
	player := track.player
	value := param & 15
	switch param >> 4 {
	case 3:
		track.vibratoWave = value
	case 4:
		track.tremoloWave = value
	case 6:
		player.extraTicks += value
	case 8:
		track.pan = (value*256 + 8) / 15
		track.surround = false
	case 10:
		track.pan = ((value + 8) % 16) * 17
		track.surround = false
	case 11:
		if value == 0 {
			track.loopRow = player.CurrentRow
		} else {
			if track.loopCount == 0 {
				track.loopCount = value
			} else {
				track.loopCount--
				if track.loopCount == 0 {
					track.loopRow = player.CurrentRow + 1
					return
				}
			}
			player.nextOrder, player.breakRow = player.CurrentOrder, track.loopRow
		}
	case 14:
		if player.delay == 0 {
			player.delay = value
		}
	}
}

func retriggerVolume(volume, command int) int {
	switch command {
	case 1, 2, 3, 4, 5:
		return volume - (1 << (command - 1))
	case 6:
		return volume * 2 / 3
	case 7:
		return volume / 2
	case 9, 10, 11, 12, 13:
		return volume + (1 << (command - 9))
	case 14:
		return volume * 3 / 2
	case 15:
		return volume * 2
	}
	return volume
}

func waveValue(phase, kind int) int {
	phase &= 63
	switch kind & 3 {
	case 1:
		if phase < 32 {
			return -phase * 4
		}
		return 255 - phase*4
	case 2:
		if phase < 32 {
			return 127
		}
		return -127
	default:
		return int(math.Trunc(127 * math.Sin(float64(phase)*math.Pi/32)))
	}
}

func (track *channel) prepare() {
	player := track.player
	effect := int(track.note.EffectNumber)
	period := track.period
	if player.S3M.AmigaLimits && period > 0 {
		period = max(452, min(3424, period))
	}
	if effect == EffectVibrato || effect == EffectVibratoAndVolumeSlide || effect == EffectFineVibrato {
		divisor := 64
		if player.S3M.OldVibrato {
			divisor = 32
		}
		if effect == EffectFineVibrato {
			divisor *= 4
		}
		period += waveValue(track.vibratoPhase, track.vibratoWave) * track.vibratoDepth * 4 / divisor
		if player.tick != 0 {
			track.vibratoPhase = (track.vibratoPhase + track.vibratoRate) & 63
		}
	}
	if effect == EffectArpeggio && track.parameter != 0 {
		offset := 0
		switch player.tick % 3 {
		case 1:
			offset = track.parameter >> 4
		case 2:
			offset = track.parameter & 15
		}
		period = int(float64(period) * math.Exp2(-float64(offset)/12))
	}
	track.step = 0
	if track.period > 0 {
		track.step = 14317456 / (float64(max(1, period)) * float64(player.SampleRate))
	}
	volume := track.volume
	if effect == EffectTremolo {
		volume += waveValue(track.tremoloPhase, track.tremoloWave) * track.tremoloDepth / 32
		if player.tick != 0 {
			track.tremoloPhase = (track.tremoloPhase + track.tremoloRate) & 63
		}
	}
	if effect == EffectTremor {
		on, off := (track.parameter>>4)+1, (track.parameter&15)+1
		if (track.tremorCount-1)%(on+off) >= on {
			volume = 0
		}
	}
	gain := float32(max(0, min(64, volume))*int(player.GlobalVolume)*int(player.S3M.MixVolume)) / (64 * 64 * 128)
	pan := float32(track.pan) / 256
	track.targetLeft, track.targetRight = gain*(1-pan), gain*pan
	if track.surround {
		track.targetLeft, track.targetRight = gain/2, -gain/2
	}
	if track.targetLeft != track.left || track.targetRight != track.right {
		duration := 363
		if gain < track.left+track.right {
			duration = 952
		}
		track.ramp = max(1, (player.SampleRate*duration+500000)/1000000)
	}
}

func (track *channel) render(frames int) {
	track.buffer.Lock()
	defer track.buffer.Unlock()
	track.scope.Lock()
	defer track.scope.Unlock()
	var data, stereoRight []float32
	start, end := 0, 0
	loop := false
	if instrument := track.voice; instrument != nil {
		data, stereoRight = instrument.Data, instrument.RightData
		end = len(data)
		start = instrument.LoopBegin
		if instrument.Loop && start >= 0 && min(end, instrument.LoopEnd)-start > 1 {
			end = min(end, instrument.LoopEnd)
			loop = true
		}
	}
	position := track.position
	for range frames {
		var leftSample, rightSample float32
		if position < float64(end) && track.step > 0 {
			index := int(position)
			next := min(end-1, index+1)
			if loop && index+1 == end {
				next = start
			}
			fraction := float32(position - float64(index))
			leftSample = data[index] + (data[next]-data[index])*fraction
			rightSample = leftSample
			if len(stereoRight) == len(data) {
				rightSample = stereoRight[index] + (stereoRight[next]-stereoRight[index])*fraction
			}
			position += track.step
			if loop && position >= float64(end) {
				position = float64(start) + math.Mod(position-float64(end), float64(end-start))
			}
		}
		if track.ramp > 0 {
			track.left += (track.targetLeft - track.left) / float32(track.ramp)
			track.right += (track.targetRight - track.right) / float32(track.ramp)
			track.ramp--
		}
		left, right := leftSample*track.left, rightSample*track.right
		track.buffer.UnsafeWrite(left)
		track.buffer.UnsafeWrite(right)
		track.scope.UnsafeWrite(left)
		track.scope.UnsafeWrite(right)
	}
	track.position = position
}
