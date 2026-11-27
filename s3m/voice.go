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
	fm                                                   *oplVoice
	fmTrigger                                            bool
	position                                             float64
	period, target, toneSpeed                            int
	memory, parameter, offset, previousOffset, retrigger int
	effectMemory                                         [27]int
	vibratoPhase, vibratoRate, vibratoDepth, vibratoWave int
	tremoloPhase, tremoloRate, tremoloDepth, tremoloWave int
	loopRow, loopCount, tremorCount                      int
	surround                                             bool
	step                                                 float64
	left, right, targetLeft, targetRight                 float32
	ramp                                                 int
}

const pitchClock = 8363 * 1712

func notePeriod(note int, instrument *Instrument, useHz bool) int {
	if note < 0 || note >= 160 || note&15 >= 12 {
		return 0
	}
	periods := [...]int{1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907}
	c4 := 8363
	if instrument != nil && instrument.MiddleC != 0 {
		c4 = int(instrument.MiddleC)
	}
	if useHz {
		ratio := math.Round(65536 * math.Exp2(float64(note&15)/12))
		return int(math.Ldexp(float64(c4)*ratio/65536, note/16-4))
	}
	return int((int64(periods[note&15]) * 8363 * 16) / (int64(c4) << uint(note>>4)))
}

func (track *channel) trigger(portamento bool) {
	note := track.note
	if note.ChangeSample {
		track.selected = note.SampleNumber - 1
		if instrument := track.player.GetInstrument(track.selected); instrument.playable() {
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
		period := notePeriod(note.Note, instrument, track.player.S3M.impulseTracker)
		if period > 0 {
			if portamento && track.period > 0 && track.voice != nil {
				track.target = period
				if track.player.S3M.impulseTracker && instrument != track.voice && instrument.playable() {
					track.position = 0
				}
				if instrument.playable() {
					track.voice = instrument
				}
			} else {
				track.period = period
				track.voice = instrument
				track.position = 0
				track.fmTrigger = instrument != nil && instrument.OPL != nil
				if track.vibratoWave < 4 {
					track.vibratoPhase = 0
				}
				if track.tremoloWave < 4 {
					track.tremoloPhase = 0
				}
				track.left, track.right = 0, 0
				track.retrigger = 0
				if track.player.S3M.separateEffectMemory {
					if note.EffectNumber == EffectSampleOffset {
						track.position = float64(track.offset * 256)
					}
				} else {
					if note.ChangeSample {
						track.previousOffset = 0
					}
					if note.EffectNumber == EffectSampleOffset {
						track.previousOffset += track.offset * 256
					}
					track.position = float64(track.previousOffset)
				}
			}
		}
	} else if note.ChangeSample {
		if instrument := track.player.GetInstrument(track.selected); instrument.playable() {
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
	case EffectSetTempo:
		if param != 0 {
			track.effectMemory[effect] = param
		}
		track.parameter = track.effectMemory[effect]
	case EffectVolumeSlide, EffectPortamentoDown, EffectPortamentoUp, EffectTremor, EffectArpeggio, EffectVibratoAndVolumeSlide, EffectPortamentoAndVolumeSlide, EffectRetriggerAndVolumeSlide, EffectTremolo, EffectSetExtra:
		if track.player.S3M.separateEffectMemory {
			key := effect
			switch effect {
			case EffectPortamentoUp:
				key = EffectPortamentoDown
			case EffectVibratoAndVolumeSlide, EffectPortamentoAndVolumeSlide:
				key = EffectVolumeSlide
			}
			if param != 0 {
				track.effectMemory[key] = param
			}
			track.parameter = track.effectMemory[key]
		} else if param == 0 {
			track.parameter = track.memory
		}
	}
	if !track.player.S3M.separateEffectMemory && effect != 0 && param != 0 {
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
	case EffectSetTempo:
		if !first && param > 0 && param < 32 {
			old := player.BPM
			if param < 16 {
				player.BPM = max(32, old-param)
			} else {
				player.BPM = min(255, old+(param&15))
			}
			if player.BPM != old {
				player.speedChanged()
			}
		}
	case EffectGlobalVolumeSlide:
		player.slideGlobalVolume(param, first)
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
			if player.S3M.impulseTracker && int64(pitchClock)+int64(track.period)*int64(amount) <= 0 {
				track.period = 0
			} else {
				track.period = slidePitch(track.period, amount, player.S3M.impulseTracker)
			}
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
		if player.S3M.impulseTracker {
			if track.period < track.target {
				track.period = min(track.target, slidePitch(track.period, -step, true))
			} else if track.period > track.target {
				track.period = max(track.target, slidePitch(track.period, step, true))
			}
		} else {
			track.period += max(-step, min(step, track.target-track.period))
		}
	}
	track.volume = max(0, min(track.volume, 64))
}

func (player *Player) slideGlobalVolume(param int, first bool) {
	if param != 0 {
		player.globalSlide = param
	}
	up, down := player.globalSlide>>4, player.globalSlide&15
	delta := 0
	switch {
	case down == 15 && up != 0:
		if first {
			delta = up
		}
	case up == 15 && down != 0:
		if first {
			delta = -down
		}
	case !first && up != 0:
		delta = up
	case !first:
		delta = -down
	}
	player.GlobalVolume = uint8(max(0, min(64, int(player.GlobalVolume)+delta)))
}

// Impulse Tracker exports use integer Hz; the other S3Ms use Amiga periods.
func slidePitch(pitch, periodDelta int, useHz bool) int {
	if !useHz {
		return pitch + periodDelta
	}
	denominator := int64(pitchClock) + int64(pitch)*int64(periodDelta)
	if denominator <= 0 {
		return math.MaxInt32
	}
	return int(min(math.MaxInt32, int64(pitchClock)*int64(pitch)/denominator))
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
		if player.S3M.impulseTracker {
			period = max(pitchClock/3424, min(pitchClock/452, period))
		} else {
			period = max(452, min(3424, period))
		}
	}
	if effect == EffectVibrato || effect == EffectVibratoAndVolumeSlide || effect == EffectFineVibrato {
		divisor := 64
		if player.S3M.OldVibrato {
			divisor = 32
		}
		if effect == EffectFineVibrato {
			divisor *= 4
		}
		period = slidePitch(period, waveValue(track.vibratoPhase, track.vibratoWave)*track.vibratoDepth*4/divisor, player.S3M.impulseTracker)
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
		if player.S3M.impulseTracker {
			offset = -offset
		}
		period = int(float64(period) * math.Exp2(-float64(offset)/12))
	}
	track.step = 0
	frequency := float64(period)
	if !player.S3M.impulseTracker {
		frequency = pitchClock / float64(max(1, period))
	}
	if track.period > 0 {
		track.step = frequency / float64(player.SampleRate)
	}
	volume := track.volume
	if effect == EffectTremolo {
		depth, rate := track.tremoloDepth, track.tremoloRate
		if player.S3M.separateEffectMemory {
			depth, rate = track.parameter&15, track.parameter>>4
		}
		volume += waveValue(track.tremoloPhase, track.tremoloWave) * depth / 32
		if player.tick != 0 {
			track.tremoloPhase = (track.tremoloPhase + rate) & 63
		}
	}
	if effect == EffectTremor {
		on, off := (track.parameter>>4)+1, (track.parameter&15)+1
		if (track.tremorCount-1)%(on+off) >= on {
			volume = 0
		}
	}
	if track.voice != nil && track.voice.OPL != nil && track.period > 0 {
		track.syncOPL(frequency, max(0, min(64, volume)))
	} else if track.fm != nil {
		track.fm.noteOff()
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
	var fmSamples []int16
	if track.fm != nil {
		fmSamples = track.fm.chip.GenerateStereoS16(frames)
	}
	for frame := range frames {
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
		if len(fmSamples) > 0 {
			// OPL output has its own mixer level, independent of PCM master volume.
			const gain = 0.816 / 32768
			mono := float32(fmSamples[frame*2]) * gain * float32(track.player.GlobalVolume) / 64
			if track.pan <= 170 {
				left += mono
			}
			if track.pan >= 85 {
				right += mono
			}
		}
		track.buffer.UnsafeWrite(left)
		track.buffer.UnsafeWrite(right)
		track.scope.UnsafeWrite(left)
		track.scope.UnsafeWrite(right)
	}
	track.position = position
}
