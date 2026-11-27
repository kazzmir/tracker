package s3m

import (
	"math"

	"github.com/Distortions81/impsynth"
)

// OPLPatch is the S3M SCRI register data: two operators followed by
// channel feedback/connection and a reserved byte.
type OPLPatch [12]byte

// Each tracker channel owns one melodic voice. Keeping their output separate
// lets the existing channel mute and oscilloscope paths work for FM and PCM.
type oplVoice struct {
	chip      *impsynth.Synth
	patch     *OPLPatch
	keyOn     bool
	pitchHigh byte
}

func (voice *oplVoice) setPatch(patch *OPLPatch) {
	voice.patch = patch
	for operator, address := range [...]uint16{0, 3} {
		for pair, register := range [...]uint16{0x20, 0x40, 0x60, 0x80, 0xe0} {
			voice.chip.WriteReg(register+address, patch[pair*2+operator])
		}
	}
	voice.chip.WriteReg(0xc0, patch[10]&15|0x30)
}

func oplLevel(level byte, volume int) byte {
	if volume >= 63 {
		return level
	}
	if volume > 0 {
		volume++
	}
	loudness := (63 - int(level&63)) * volume / 64
	return level&0xc0 | byte(63-loudness)
}

func (voice *oplVoice) setVolume(volume int) {
	patch := voice.patch
	if patch[10]&1 != 0 {
		voice.chip.WriteReg(0x40, oplLevel(patch[2], volume))
	}
	voice.chip.WriteReg(0x43, oplLevel(patch[3], volume))
}

const oplClockRate = 49716

func oplPitch(frequency float64) (byte, byte) {
	// F-number times the chip clock, scaled by the octave block.
	block := 0
	number := math.Ldexp(max(0, frequency)/oplClockRate, 20)
	for block < 7 && number > 1023 {
		block++
		number /= 2
	}
	packed := int(min(1023, math.Round(number)))
	return byte(packed), byte(block<<2 | packed>>8)
}

func (voice *oplVoice) noteOff() {
	if voice.keyOn {
		voice.chip.WriteReg(0xb0, voice.pitchHigh)
		voice.keyOn = false
	}
}

func (voice *oplVoice) setPitch(frequency float64) {
	low, high := oplPitch(frequency)
	voice.pitchHigh = high
	voice.chip.WriteReg(0xa0, low)
	voice.chip.WriteReg(0xb0, high|0x20)
	voice.keyOn = true
}

func (track *channel) syncOPL(frequency float64, volume int) {
	patch := track.voice.OPL
	if track.fm == nil {
		track.fm = &oplVoice{chip: impsynth.New(track.player.SampleRate)}
		track.fm.chip.WriteReg(0x105, 1) // OPL3 mode, still using a two-operator patch
		track.fm.chip.WriteReg(0x01, 0x20)
	}
	if track.fmTrigger && track.fm.keyOn {
		track.fm.noteOff()
		// Advance at least one chip tick so ImpSynth observes the key-off
		// before we raise the key again, even at higher output rates.
		frames := (track.player.SampleRate + oplClockRate - 1) / oplClockRate
		track.fm.chip.GenerateStereoS16(frames)
	}
	if track.fm.patch != patch {
		track.fm.setPatch(patch)
	}
	track.fm.setVolume((volume*63 + 32) / 64)
	track.fm.setPitch(frequency * 261.625 / 8363)
	track.fmTrigger = false
}
