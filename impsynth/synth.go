// ImpSynth
// Copyright (C) 2026 Carl Frank Otto III
//
// This library is based in part on the Nuked-OPL3 OPL3 emulator by Nuke.YKT.
// It is free software: you can redistribute it and/or modify it under the
// terms of the GNU Lesser General Public License as published by the Free
// Software Foundation, version 2.1 of the License.
//
// This library is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS
// FOR A PARTICULAR PURPOSE. See the GNU Lesser General Public License for more
// details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with this library; if not, see <https://www.gnu.org/licenses/>.

package impsynth

import (
	"math"
	"math/bits"
)

const (
	opl3DefaultSampleRate = 49716
	opl3ChannelCount      = 18
	opl3OperatorCount     = 2

	oplWaveTableSize  = 1024
	oplWaveTableMask  = oplWaveTableSize - 1
	oplPhaseFracBits  = 9
	oplEnvelopeSilent = 0x1ff
	oplEnvelopeMax    = 0x3ff
	oplAttenTableSize = 2048
	oplPanGainShift   = 15
	oplPanGainUnit    = 1 << oplPanGainShift

	oplChannelMixGain                      = 0.125
	oplOperatorOutputScale                 = 4096.0
	oplPhaseModScale                       = 2032.0
	oplFeedbackPhaseScaleRatio             = 4.0
	oplKeyMaskNormal                       = 1 << 0
	oplKeyMaskDrum                         = 1 << 1
	oplEnvOff                  oplEnvStage = iota
	oplEnvAttack
	oplEnvDecay
	oplEnvSustain
	oplEnvRelease
)

var (
	oplSlotToChannel = [22]int{
		0, 1, 2, 0, 1, 2, -1, -1,
		3, 4, 5, 3, 4, 5, -1, -1,
		6, 7, 8, 6, 7, 8,
	}
	oplSlotToOperator = [22]int{
		0, 0, 0, 1, 1, 1, -1, -1,
		0, 0, 0, 1, 1, 1, -1, -1,
		0, 0, 0, 1, 1, 1,
	}
	// The OPL frequency multiplier table is stored doubled.
	oplFrequencyMultiples = [16]uint32{
		1, 2, 4, 6, 8, 10, 12, 14,
		16, 18, 20, 20, 24, 24, 30, 30,
	}
	oplKSLROM = [16]uint8{
		0, 32, 40, 45, 48, 51, 53, 55,
		56, 58, 59, 60, 61, 62, 63, 64,
	}
	oplKSLShift  = [4]uint8{8, 1, 2, 0}
	oplEGIncStep = [4][4]uint8{
		{0, 0, 0, 0},
		{1, 0, 0, 0},
		{1, 0, 1, 0},
		{1, 1, 1, 0},
	}
	oplFeedbackPhaseScale = [8]float64{
		0,
		1 * oplFeedbackPhaseScaleRatio,
		2 * oplFeedbackPhaseScaleRatio,
		4 * oplFeedbackPhaseScaleRatio,
		8 * oplFeedbackPhaseScaleRatio,
		16 * oplFeedbackPhaseScaleRatio,
		32 * oplFeedbackPhaseScaleRatio,
		64 * oplFeedbackPhaseScaleRatio,
	}
	oplWavePhaseModScale = [8]float64{
		1.18, // sine needs a little more phase authority to keep low-note body
		1.08,
		0.76, // rectified sine runs too bright without an extra trim
		0.92,
		0.92,
		0.88,
		0.72,
		0.80,
	}
	oplWaveTable  [8][oplWaveTableSize]float64
	oplAttenTable [oplAttenTableSize]float64
	oplEnvShift   [64][14][4][2]uint8
	oplWave0LUT   [oplWaveTableSize][oplEnvelopeMax + 1]int16
)

type oplEnvStage uint8
type oplRenderMode uint8

const (
	oplRenderModeGeneric oplRenderMode = iota
	oplRenderModeWave0FMStereo
	oplRenderModeWave0FMLeft
	oplRenderModeWave0FMRight
	oplRenderModeWave0FMFeedbackStereo
	oplRenderModeWave0FMFeedbackLeft
	oplRenderModeWave0FMFeedbackRight
	oplRenderModeWave0AdditiveStereo
	oplRenderModeWave0AdditiveLeft
	oplRenderModeWave0AdditiveRight
	oplRenderModeWave0FMStaticStereo
	oplRenderModeWave0FMStaticLeft
	oplRenderModeWave0FMStaticRight
	oplRenderModeWave0FMFeedbackStaticStereo
	oplRenderModeWave0FMFeedbackStaticLeft
	oplRenderModeWave0FMFeedbackStaticRight
	oplRenderModeWave0AdditiveStaticStereo
	oplRenderModeWave0AdditiveStaticLeft
	oplRenderModeWave0AdditiveStaticRight
)

type impSynthOperatorState struct {
	pgPhase    uint32
	phaseReset bool
	keyMask    uint8
	egRout     uint16
	egOut      uint16
	egBase     uint16
	stage      oplEnvStage
	regVib     bool
	regTrem    bool
	regType    bool
	regKSR     bool
	regMult    uint8
	regKSL     uint8
	regTL      uint8
	regAR      uint8
	regDR      uint8
	regSL      uint8
	regRR      uint8
	regWave    uint8
	egKSL      uint8
	out        float64
}

type impSynthChannelState struct {
	keyOn    bool
	fnum     uint16
	block    uint8
	ksv      uint8
	render   oplRenderMode
	additive bool
	panL     int32
	panR     int32
	feedback uint8
	fbPrev   [2]int16
	ops      [opl3OperatorCount]impSynthOperatorState
}

// A Go-native OPL3-inspired synth for the subset of the chip this
// codebase drives: 2-op voices, operator envelopes, feedback, waveforms, pan,
// and DMX-style register writes.
type Synth struct {
	sampleRate       int
	opl2Mode         bool
	resampleStep     uint64
	resamplePhase    uint64
	resamplePrimed   bool
	resamplePrevL    int32
	resamplePrevR    int32
	resampleNextL    int32
	resampleNextR    int32
	regs             [0x200]uint8
	ch               [opl3ChannelCount]impSynthChannelState
	waveformSelectOn bool
	stereoExt        bool
	rhythmMode       bool
	rhythmBits       uint8
	noteSelect       uint8
	tremoloShift     uint8
	vibShift         uint8
	tremoloPos       uint8
	tremolo          uint8
	vibPos           uint8
	timer            uint64
	egTimer          uint64
	egState          uint8
	egAdd            uint8
	egTimerLo        uint8
	noise            uint32
	rmHHBit2         uint8
	rmHHBit3         uint8
	rmHHBit7         uint8
	rmHHBit8         uint8
	rmTCBit3         uint8
	rmTCBit5         uint8
	activeMask       uint32
	stereoBuf        []int16
	monoBuf          []byte
}

func init() {
	buildOPLWaveTables()
	buildOPLAttenuationTable()
	buildOPLEnvelopeShiftTable()
	buildOPLWave0LUT()
}

// New creates a synth at the provided sample rate.
func New(sampleRate int) *Synth {
	if sampleRate <= 0 {
		sampleRate = opl3DefaultSampleRate
	}
	o := &Synth{
		sampleRate:   sampleRate,
		resampleStep: (uint64(opl3DefaultSampleRate) << 32) / uint64(sampleRate),
	}
	o.Reset()
	return o
}

// NewOPL2 creates a synth constrained to AdLib/OPL2-compatible behavior.
func NewOPL2(sampleRate int) *Synth {
	o := New(sampleRate)
	o.opl2Mode = true
	o.Reset()
	return o
}

// Reset clears all registers and runtime state.
func (o *Synth) Reset() {
	if o == nil {
		return
	}
	o.regs = [0x200]uint8{}
	o.ch = [opl3ChannelCount]impSynthChannelState{}
	o.waveformSelectOn = false
	o.stereoExt = false
	o.rhythmMode = false
	o.rhythmBits = 0
	o.noteSelect = 0
	o.tremoloShift = 4
	o.vibShift = 1
	o.tremoloPos = 0
	o.tremolo = 0
	o.vibPos = 0
	o.timer = 0
	o.egTimer = 0
	o.egState = 0
	o.egAdd = 0
	o.egTimerLo = 0
	o.noise = 1
	o.rmHHBit2 = 0
	o.rmHHBit3 = 0
	o.rmHHBit7 = 0
	o.rmHHBit8 = 0
	o.rmTCBit3 = 0
	o.rmTCBit5 = 0
	o.activeMask = 0
	o.resamplePhase = 0
	o.resamplePrimed = false
	o.resamplePrevL = 0
	o.resamplePrevR = 0
	o.resampleNextL = 0
	o.resampleNextR = 0
	for i := range o.ch {
		o.ch[i].panL = oplPanGainUnit
		o.ch[i].panR = oplPanGainUnit
		for op := range o.ch[i].ops {
			o.ch[i].ops[op] = impSynthOperatorState{
				egRout: oplEnvelopeSilent,
				egOut:  oplEnvelopeSilent,
				stage:  oplEnvRelease,
			}
		}
	}
}

// WriteReg applies a subset of OPL3 register writes.
func (o *Synth) WriteReg(addr uint16, value uint8) {
	if o == nil {
		return
	}
	if o.opl2Mode {
		if addr >= 0x100 {
			return
		}
		if addr >= 0xD0 && addr <= 0xD8 {
			return
		}
	}
	a := int(addr & 0x1FF)
	o.regs[a] = value
	switch a {
	case 0x01:
		o.waveformSelectOn = (value & 0x20) != 0
		for ch := range o.ch {
			for op := 0; op < opl3OperatorCount; op++ {
				o.refreshOperator(ch, op)
			}
		}
		return
	case 0x105:
		o.stereoExt = (value & 0x02) != 0
		for ch := range o.ch {
			o.refreshChannelControl(ch)
		}
		return
	case 0x08:
		o.noteSelect = (value >> 6) & 0x01
		for ch := range o.ch {
			o.refreshChannelFreq(ch)
		}
		return
	case 0xBD:
		o.tremoloShift = (((value >> 7) ^ 1) << 1) + 2
		o.vibShift = ((value >> 6) & 0x01) ^ 1
		o.updateRhythm(value)
		return
	}

	bank := 0
	off := a
	if a >= 0x100 {
		bank = 1
		off = a - 0x100
	}

	switch {
	case off >= 0x20 && off < 0x20+len(oplSlotToChannel):
		if ch, op, ok := decodeOperatorSlot(bank, off-0x20); ok {
			o.refreshOperator(ch, op)
		}
	case off >= 0x40 && off < 0x40+len(oplSlotToChannel):
		if ch, op, ok := decodeOperatorSlot(bank, off-0x40); ok {
			o.refreshOperator(ch, op)
		}
	case off >= 0x60 && off < 0x60+len(oplSlotToChannel):
		if ch, op, ok := decodeOperatorSlot(bank, off-0x60); ok {
			o.refreshOperator(ch, op)
		}
	case off >= 0x80 && off < 0x80+len(oplSlotToChannel):
		if ch, op, ok := decodeOperatorSlot(bank, off-0x80); ok {
			o.refreshOperator(ch, op)
		}
	case off >= 0xE0 && off < 0xE0+len(oplSlotToChannel):
		if ch, op, ok := decodeOperatorSlot(bank, off-0xE0); ok {
			o.refreshOperator(ch, op)
		}
	case off >= 0xA0 && off <= 0xA8:
		o.refreshChannelFreq(bank*9 + off - 0xA0)
	case off >= 0xB0 && off <= 0xB8:
		ch := bank*9 + off - 0xB0
		o.refreshChannelFreq(ch)
		keyOn := (value & 0x20) != 0
		if keyOn != o.ch[ch].keyOn {
			o.ch[ch].keyOn = keyOn
			if keyOn {
				o.keyOnChannel(ch)
			} else {
				o.keyOffChannel(ch)
			}
		}
	case off >= 0xC0 && off <= 0xC8:
		o.refreshChannelControl(bank*9 + off - 0xC0)
	case off >= 0xD0 && off <= 0xD8:
		if o.stereoExt {
			o.refreshChannelStereoPan(bank*9 + off - 0xD0)
		}
	}
}

// GenerateStereoS16 produces interleaved stereo signed-16 PCM.
// The returned slice is backed by an internal reusable buffer and is only
// valid until the next GenerateStereoS16/GenerateMonoU8 call on this instance.
func (o *Synth) GenerateStereoS16(frames int) []int16 {
	if o == nil || frames <= 0 || o.sampleRate <= 0 {
		return nil
	}
	need := frames * 2
	if cap(o.stereoBuf) < need {
		o.stereoBuf = make([]int16, need)
	} else {
		o.stereoBuf = o.stereoBuf[:need]
	}
	out := o.stereoBuf
	for i := 0; i < frames; i++ {
		l, r := o.nextStereoSample()
		out[i*2] = clampPCM16(l)
		out[i*2+1] = clampPCM16(r)
	}
	return out
}

func (o *Synth) nextStereoSample() (int32, int32) {
	if o == nil {
		return 0, 0
	}
	if o.sampleRate == opl3DefaultSampleRate {
		return o.renderChipSample()
	}
	o.primeResampler()
	l := lerpPCM16(o.resamplePrevL, o.resampleNextL, o.resamplePhase)
	r := lerpPCM16(o.resamplePrevR, o.resampleNextR, o.resamplePhase)

	o.resamplePhase += o.resampleStep
	for o.resamplePhase >= (uint64(1) << 32) {
		o.resamplePhase -= uint64(1) << 32
		o.resamplePrevL = o.resampleNextL
		o.resamplePrevR = o.resampleNextR
		o.resampleNextL, o.resampleNextR = o.renderChipSample()
	}
	return l, r
}

func (o *Synth) primeResampler() {
	if o == nil || o.resamplePrimed {
		return
	}
	o.resamplePrevL, o.resamplePrevR = o.renderChipSample()
	o.resampleNextL, o.resampleNextR = o.renderChipSample()
	o.resamplePrimed = true
}

func (o *Synth) renderChipSample() (int32, int32) {
	var l, r int32
	for active := o.activeMask; active != 0; active &= active - 1 {
		ch := bits.TrailingZeros32(active)
		if o.rhythmMode && ch >= 6 && ch <= 8 {
			continue
		}
		sl, sr := o.renderChannel(ch)
		l += sl
		r += sr
	}
	if o.rhythmMode {
		sl, sr := o.renderRhythmChannels()
		l += sl
		r += sr
	}
	o.advanceChipState()
	return l, r
}

// GenerateMonoU8 produces unsigned 8-bit mono PCM from the mixed stereo output.
// The returned slice is backed by an internal reusable buffer and is only
// valid until the next GenerateStereoS16/GenerateMonoU8 call on this instance.
func (o *Synth) GenerateMonoU8(frames int) []byte {
	st := o.GenerateStereoS16(frames)
	if len(st) == 0 {
		return nil
	}
	if cap(o.monoBuf) < frames {
		o.monoBuf = make([]byte, frames)
	} else {
		o.monoBuf = o.monoBuf[:frames]
	}
	out := o.monoBuf
	for i := 0; i < frames; i++ {
		l := int(st[i*2])
		r := int(st[i*2+1])
		m := (l + r) / 2
		u := (m >> 8) + 128
		if u < 0 {
			u = 0
		} else if u > 255 {
			u = 255
		}
		out[i] = byte(u)
	}
	return out
}

func (o *Synth) renderChannel(ch int) (int32, int32) {
	if ch < 0 || ch >= len(o.ch) {
		return 0, 0
	}
	c := &o.ch[ch]
	if !channelHasActiveOperators(c) {
		o.syncChannelActive(ch)
		return 0, 0
	}
	switch c.render {
	case oplRenderModeWave0FMStaticStereo:
		return o.renderChannelWave0FMStaticStereo(c)
	case oplRenderModeWave0FMStaticLeft:
		return o.renderChannelWave0FMStaticLeft(c)
	case oplRenderModeWave0FMStaticRight:
		return o.renderChannelWave0FMStaticRight(c)
	case oplRenderModeWave0FMFeedbackStaticStereo:
		return o.renderChannelWave0FeedbackStaticStereo(c)
	case oplRenderModeWave0FMFeedbackStaticLeft:
		return o.renderChannelWave0FeedbackStaticLeft(c)
	case oplRenderModeWave0FMFeedbackStaticRight:
		return o.renderChannelWave0FeedbackStaticRight(c)
	case oplRenderModeWave0AdditiveStaticStereo:
		return o.renderChannelWave0AdditiveStaticStereo(c)
	case oplRenderModeWave0AdditiveStaticLeft:
		return o.renderChannelWave0AdditiveStaticLeft(c)
	case oplRenderModeWave0AdditiveStaticRight:
		return o.renderChannelWave0AdditiveStaticRight(c)
	case oplRenderModeWave0FMStereo:
		return o.renderChannelWave0FMStereo(c)
	case oplRenderModeWave0FMLeft:
		return o.renderChannelWave0FMLeft(c)
	case oplRenderModeWave0FMRight:
		return o.renderChannelWave0FMRight(c)
	case oplRenderModeWave0FMFeedbackStereo:
		return o.renderChannelWave0FeedbackStereo(c)
	case oplRenderModeWave0FMFeedbackLeft:
		return o.renderChannelWave0FeedbackLeft(c)
	case oplRenderModeWave0FMFeedbackRight:
		return o.renderChannelWave0FeedbackRight(c)
	case oplRenderModeWave0AdditiveStereo:
		return o.renderChannelWave0AdditiveStereo(c)
	case oplRenderModeWave0AdditiveLeft:
		return o.renderChannelWave0AdditiveLeft(c)
	case oplRenderModeWave0AdditiveRight:
		return o.renderChannelWave0AdditiveRight(c)
	}

	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelope(c, mod)
	modPhase := o.advanceOperatorPhase(c, mod)
	modFB := 0
	if c.feedback != 0 {
		modFB = oplFeedbackPhaseOffset(c.fbPrev[0], c.fbPrev[1], c.feedback)
	}
	modRaw := o.sampleOperator(mod, modPhase, modFB)
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelope(c, car)
	carPhase := o.advanceOperatorPhase(c, car)
	carMod := 0
	if !c.additive {
		carMod = modOut
	}
	carRaw := o.sampleOperator(car, carPhase, carMod)

	out := carRaw
	if c.additive {
		out += modOut
	}
	return applyPanGain(out, c.panL), applyPanGain(out, c.panR)
}

func (o *Synth) renderRhythmChannels() (int32, int32) {
	var l, r int32
	for ch := 6; ch <= 8; ch++ {
		if (o.activeMask & (1 << uint(ch))) == 0 {
			continue
		}
		if !channelHasActiveOperators(&o.ch[ch]) {
			o.syncChannelActive(ch)
			continue
		}
	}

	l6, r6 := o.renderRhythmBassDrum()
	l += l6
	r += r6
	l7, r7 := o.renderRhythmHighHatSnare()
	l += l7
	r += r7
	l8, r8 := o.renderRhythmTomCymbal()
	l += l8
	r += r8
	return l, r
}

func (o *Synth) renderRhythmBassDrum() (int32, int32) {
	c := &o.ch[6]
	mod := &c.ops[0]
	car := &c.ops[1]
	if !operatorHasSound(mod) && !operatorHasSound(car) {
		o.syncChannelActive(6)
		return 0, 0
	}

	o.advanceEnvelope(c, mod)
	modPhase := o.advanceOperatorPhase(c, mod)
	modFB := 0
	if c.feedback != 0 {
		modFB = oplFeedbackPhaseOffset(c.fbPrev[0], c.fbPrev[1], c.feedback)
	}
	modRaw := 0
	if operatorHasSound(mod) {
		modRaw = o.sampleOperator(mod, modPhase, modFB)
	}
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelope(c, car)
	carPhase := o.advanceOperatorPhase(c, car)
	carMod := 0
	if !c.additive {
		carMod = modOut
	}
	carRaw := 0
	if operatorHasSound(car) {
		carRaw = o.sampleOperator(car, carPhase, carMod)
	}

	return applyPanGain(carRaw, c.panL), applyPanGain(carRaw, c.panR)
}

func (o *Synth) renderRhythmHighHatSnare() (int32, int32) {
	c := &o.ch[7]
	hh := &c.ops[0]
	sd := &c.ops[1]
	if !operatorHasSound(hh) && !operatorHasSound(sd) {
		o.syncChannelActive(7)
		return 0, 0
	}

	o.advanceEnvelope(c, hh)
	hhPhase := o.advanceOperatorPhase(c, hh)
	o.rmHHBit2 = uint8((hhPhase >> 2) & 1)
	o.rmHHBit3 = uint8((hhPhase >> 3) & 1)
	o.rmHHBit7 = uint8((hhPhase >> 7) & 1)
	o.rmHHBit8 = uint8((hhPhase >> 8) & 1)
	hhNoise := oplAdvanceNoiseN(o.noise, 13)
	hhRaw := 0
	if operatorHasSound(hh) {
		hhRaw = o.sampleOperator(hh, oplRhythmHighHatPhase(o.rmXor(), hhNoise), 0)
	}

	o.advanceEnvelope(c, sd)
	_ = o.advanceOperatorPhase(c, sd)
	sdNoise := oplAdvanceNoiseN(o.noise, 16)
	sdRaw := 0
	if operatorHasSound(sd) {
		sdRaw = o.sampleOperator(sd, oplRhythmSnarePhase(o.rmHHBit8, sdNoise), 0)
	}

	out := hhRaw + sdRaw
	return applyPanGain(out, c.panL), applyPanGain(out, c.panR)
}

func (o *Synth) renderRhythmTomCymbal() (int32, int32) {
	c := &o.ch[8]
	tom := &c.ops[0]
	tc := &c.ops[1]
	if !operatorHasSound(tom) && !operatorHasSound(tc) {
		o.syncChannelActive(8)
		return 0, 0
	}

	o.advanceEnvelope(c, tom)
	tomPhase := o.advanceOperatorPhase(c, tom)
	tomRaw := 0
	if operatorHasSound(tom) {
		tomRaw = o.sampleOperator(tom, tomPhase, 0)
	}

	o.advanceEnvelope(c, tc)
	tcPhase := o.advanceOperatorPhase(c, tc)
	o.rmTCBit3 = uint8((tcPhase >> 3) & 1)
	o.rmTCBit5 = uint8((tcPhase >> 5) & 1)
	tcRaw := 0
	if operatorHasSound(tc) {
		tcRaw = o.sampleOperator(tc, oplRhythmTopCymbalPhase(o.rmXor()), 0)
	}

	out := tomRaw + tcRaw
	return applyPanGain(out, c.panL), applyPanGain(out, c.panR)
}

func (o *Synth) renderChannelCommonWave0Raw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelope(c, mod)
	modPhase := o.advanceOperatorPhase(c, mod)
	modRaw := sampleOperatorWave0(mod, modPhase, 0)
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelope(c, car)
	carPhase := o.advanceOperatorPhase(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, modOut)

	return int32(carRaw)
}

func (o *Synth) renderChannelWave0FMStaticRaw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelopeNoTrem(c, mod)
	modPhase := advanceOperatorPhaseNoVib(c, mod)
	modRaw := sampleOperatorWave0(mod, modPhase, 0)
	modOut := int(feedbackSample(modRaw))

	o.advanceEnvelopeNoTrem(c, car)
	carPhase := advanceOperatorPhaseNoVib(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, modOut)

	return int32(carRaw)
}

func (o *Synth) renderChannelWave0FeedbackRaw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelope(c, mod)
	modPhase := o.advanceOperatorPhase(c, mod)
	modFB := oplFeedbackPhaseOffset(c.fbPrev[0], c.fbPrev[1], c.feedback)
	modRaw := sampleOperatorWave0(mod, modPhase, modFB)
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelope(c, car)
	carPhase := o.advanceOperatorPhase(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, modOut)

	return int32(carRaw)
}

func (o *Synth) renderChannelWave0FeedbackStaticRaw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelopeNoTrem(c, mod)
	modPhase := advanceOperatorPhaseNoVib(c, mod)
	modFB := oplFeedbackPhaseOffset(c.fbPrev[0], c.fbPrev[1], c.feedback)
	modRaw := sampleOperatorWave0(mod, modPhase, modFB)
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelopeNoTrem(c, car)
	carPhase := advanceOperatorPhaseNoVib(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, modOut)

	return int32(carRaw)
}

func (o *Synth) renderChannelWave0AdditiveRaw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelope(c, mod)
	modPhase := o.advanceOperatorPhase(c, mod)
	modRaw := sampleOperatorWave0(mod, modPhase, 0)
	modOut := int(feedbackSample(modRaw))
	c.fbPrev[1] = c.fbPrev[0]
	c.fbPrev[0] = int16(modOut)

	o.advanceEnvelope(c, car)
	carPhase := o.advanceOperatorPhase(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, 0)

	return int32(carRaw + modOut)
}

func (o *Synth) renderChannelWave0AdditiveStaticRaw(c *impSynthChannelState) int32 {
	mod := &c.ops[0]
	car := &c.ops[1]

	o.advanceEnvelopeNoTrem(c, mod)
	modPhase := advanceOperatorPhaseNoVib(c, mod)
	modRaw := sampleOperatorWave0(mod, modPhase, 0)
	modOut := int(feedbackSample(modRaw))

	o.advanceEnvelopeNoTrem(c, car)
	carPhase := advanceOperatorPhaseNoVib(c, car)
	carRaw := sampleOperatorWave0(car, carPhase, 0)

	return int32(carRaw + modOut)
}

func stereoSample(v int32) (int32, int32) { return v, v }
func leftSample(v int32) (int32, int32)   { return v, 0 }
func rightSample(v int32) (int32, int32)  { return 0, v }

func (o *Synth) renderChannelWave0FMStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelCommonWave0Raw(c))
}

func (o *Synth) renderChannelWave0FMLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelCommonWave0Raw(c))
}

func (o *Synth) renderChannelWave0FMRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelCommonWave0Raw(c))
}

func (o *Synth) renderChannelWave0FeedbackStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelWave0FeedbackRaw(c))
}

func (o *Synth) renderChannelWave0FeedbackLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelWave0FeedbackRaw(c))
}

func (o *Synth) renderChannelWave0FeedbackRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelWave0FeedbackRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelWave0AdditiveRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelWave0AdditiveRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelWave0AdditiveRaw(c))
}

func (o *Synth) renderChannelWave0FMStaticStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelWave0FMStaticRaw(c))
}

func (o *Synth) renderChannelWave0FMStaticLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelWave0FMStaticRaw(c))
}

func (o *Synth) renderChannelWave0FMStaticRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelWave0FMStaticRaw(c))
}

func (o *Synth) renderChannelWave0FeedbackStaticStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelWave0FeedbackStaticRaw(c))
}

func (o *Synth) renderChannelWave0FeedbackStaticLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelWave0FeedbackStaticRaw(c))
}

func (o *Synth) renderChannelWave0FeedbackStaticRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelWave0FeedbackStaticRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveStaticStereo(c *impSynthChannelState) (int32, int32) {
	return stereoSample(o.renderChannelWave0AdditiveStaticRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveStaticLeft(c *impSynthChannelState) (int32, int32) {
	return leftSample(o.renderChannelWave0AdditiveStaticRaw(c))
}

func (o *Synth) renderChannelWave0AdditiveStaticRight(c *impSynthChannelState) (int32, int32) {
	return rightSample(o.renderChannelWave0AdditiveStaticRaw(c))
}

func (o *Synth) sampleOperator(op *impSynthOperatorState, phase int, phaseMod int) int {
	if op == nil {
		return 0
	}
	return oplWaveOutput(op.regWave&0x07, uint16(phase+phaseMod), op.egOut)
}

func sampleOperatorWave0(op *impSynthOperatorState, phase int, phaseMod int) int {
	return int(oplWave0LUT[uint16(phase+phaseMod)&oplWaveTableMask][op.egOut])
}

func (o *Synth) advanceEnvelope(c *impSynthChannelState, op *impSynthOperatorState) {
	if c == nil || op == nil {
		return
	}
	trem := 0
	if op.regTrem {
		trem = int(o.tremolo)
	}
	baseAtten := int(op.egRout) + int(op.regTL<<2) + int(op.egKSL>>oplKSLShift[op.regKSL]) + trem
	op.egOut = uint16(clampAtten(baseAtten))

	keyOn := operatorKeyOn(op)
	reset := keyOn && op.stage == oplEnvRelease
	regRate := uint8(0)
	if reset {
		regRate = op.regAR
	} else {
		switch op.stage {
		case oplEnvAttack:
			regRate = op.regAR
		case oplEnvDecay:
			regRate = op.regDR
		case oplEnvSustain:
			if !op.regType {
				regRate = op.regRR
			}
		case oplEnvRelease:
			regRate = op.regRR
		}
	}
	op.phaseReset = reset

	ks := int(c.ksv)
	if !op.regKSR {
		ks >>= 2
	}
	nonZero := regRate != 0
	rate := ks + int(regRate<<2)
	if rate > 0x3f {
		rate = 0x3f
	}
	rateHi := rate >> 2
	shift := 0
	if nonZero {
		shift = int(oplEnvShift[rate][o.egAdd][o.egTimerLo][o.egState])
	}
	egRout := int(op.egRout)
	if reset && rateHi == 0x0f {
		egRout = 0
	}
	egOff := (op.egRout & 0x1f8) == 0x1f8
	if op.stage != oplEnvAttack && !reset && egOff {
		egRout = oplEnvelopeSilent
	}

	egInc := 0
	switch op.stage {
	case oplEnvAttack:
		if op.egRout == 0 {
			op.stage = oplEnvDecay
		} else if keyOn && shift > 0 && rateHi != 0x0f {
			// Match the chip's 9-bit attack wraparound instead of masking the
			// complement first. Masking first leaves a fully silent operator
			// stuck at 0x1ff for medium attack rates.
			egInc = int(^op.egRout) >> uint(4-shift)
		}
	case oplEnvDecay:
		if int(op.egRout>>4) == int(op.regSL) {
			op.stage = oplEnvSustain
		} else if !egOff && !reset && shift > 0 {
			egInc = 1 << (shift - 1)
		}
	case oplEnvSustain, oplEnvRelease:
		if !egOff && !reset && shift > 0 {
			egInc = 1 << (shift - 1)
		}
	}

	op.egRout = uint16((egRout + egInc) & oplEnvelopeSilent)
	if reset {
		op.stage = oplEnvAttack
	}
	if !keyOn {
		op.stage = oplEnvRelease
	}
}

func (o *Synth) advanceEnvelopeNoTrem(c *impSynthChannelState, op *impSynthOperatorState) {
	baseAtten := int(op.egRout) + int(op.egBase)
	op.egOut = uint16(clampAtten(baseAtten))

	keyOn := operatorKeyOn(op)
	reset := keyOn && op.stage == oplEnvRelease
	regRate := uint8(0)
	if reset {
		regRate = op.regAR
	} else {
		switch op.stage {
		case oplEnvAttack:
			regRate = op.regAR
		case oplEnvDecay:
			regRate = op.regDR
		case oplEnvSustain:
			if !op.regType {
				regRate = op.regRR
			}
		case oplEnvRelease:
			regRate = op.regRR
		}
	}
	op.phaseReset = reset

	ks := int(c.ksv)
	if !op.regKSR {
		ks >>= 2
	}
	nonZero := regRate != 0
	rate := ks + int(regRate<<2)
	if rate > 0x3f {
		rate = 0x3f
	}
	rateHi := rate >> 2
	shift := 0
	if nonZero {
		shift = int(oplEnvShift[rate][o.egAdd][o.egTimerLo][o.egState])
	}
	egRout := int(op.egRout)
	if reset && rateHi == 0x0f {
		egRout = 0
	}
	egOff := (op.egRout & 0x1f8) == 0x1f8
	if op.stage != oplEnvAttack && !reset && egOff {
		egRout = oplEnvelopeSilent
	}

	egInc := 0
	switch op.stage {
	case oplEnvAttack:
		if op.egRout == 0 {
			op.stage = oplEnvDecay
		} else if keyOn && shift > 0 && rateHi != 0x0f {
			egInc = int(^op.egRout) >> uint(4-shift)
		}
	case oplEnvDecay:
		if int(op.egRout>>4) == int(op.regSL) {
			op.stage = oplEnvSustain
		} else if !egOff && !reset && shift > 0 {
			egInc = 1 << (shift - 1)
		}
	case oplEnvSustain, oplEnvRelease:
		if !egOff && !reset && shift > 0 {
			egInc = 1 << (shift - 1)
		}
	}

	op.egRout = uint16((egRout + egInc) & oplEnvelopeSilent)
	if reset {
		op.stage = oplEnvAttack
	}
	if !keyOn {
		op.stage = oplEnvRelease
	}
}

func (o *Synth) advanceOperatorPhase(c *impSynthChannelState, op *impSynthOperatorState) int {
	if c == nil || op == nil {
		return 0
	}
	phase := int(uint16(op.pgPhase >> oplPhaseFracBits))
	if op.phaseReset {
		op.pgPhase = 0
		op.phaseReset = false
	}

	fnum := int(c.fnum)
	if op.regVib {
		rangeVal := (fnum >> 7) & 0x07
		vibPos := int(o.vibPos)
		if (vibPos & 0x03) == 0 {
			rangeVal = 0
		} else if (vibPos & 0x01) != 0 {
			rangeVal >>= 1
		}
		rangeVal >>= o.vibShift
		if (vibPos & 0x04) != 0 {
			rangeVal = -rangeVal
		}
		fnum += rangeVal
	}
	baseFreq := (fnum << c.block) >> 1
	op.pgPhase += uint32((baseFreq * int(oplFrequencyMultiples[op.regMult])) >> 1)
	return phase & oplWaveTableMask
}

func advanceOperatorPhaseNoVib(c *impSynthChannelState, op *impSynthOperatorState) int {
	phase := int(uint16(op.pgPhase >> oplPhaseFracBits))
	if op.phaseReset {
		op.pgPhase = 0
		op.phaseReset = false
	}
	baseFreq := (int(c.fnum) << c.block) >> 1
	op.pgPhase += uint32((baseFreq * int(oplFrequencyMultiples[op.regMult])) >> 1)
	return phase & oplWaveTableMask
}

func (o *Synth) keyOnChannel(ch int) {
	if ch < 0 || ch >= len(o.ch) {
		return
	}
	o.ch[ch].fbPrev = [2]int16{}
	for op := range o.ch[ch].ops {
		o.ch[ch].ops[op].keyMask |= oplKeyMaskNormal
	}
	o.syncChannelActive(ch)
}

func (o *Synth) keyOffChannel(ch int) {
	if ch < 0 || ch >= len(o.ch) {
		return
	}
	o.ch[ch].fbPrev = [2]int16{}
	for op := range o.ch[ch].ops {
		o.ch[ch].ops[op].keyMask &^= oplKeyMaskNormal
		if o.ch[ch].ops[op].egRout >= oplEnvelopeSilent {
			o.ch[ch].ops[op].stage = oplEnvRelease
		}
	}
	o.syncChannelActive(ch)
}

func (o *Synth) updateRhythm(value uint8) {
	prevMode := o.rhythmMode
	prevBits := o.rhythmBits
	o.rhythmMode = (value & 0x20) != 0
	o.rhythmBits = value & 0x1F

	if !o.rhythmMode {
		for _, drum := range []struct{ ch, op int }{
			{6, 0}, {6, 1},
			{7, 0}, {7, 1},
			{8, 0}, {8, 1},
		} {
			o.setDrumKey(drum.ch, drum.op, false)
		}
		for ch := 6; ch <= 8; ch++ {
			o.syncChannelActive(ch)
		}
		return
	}
	if !prevMode {
		for ch := 6; ch <= 8; ch++ {
			o.ch[ch].fbPrev = [2]int16{}
		}
	}
	o.setDrumKey(7, 0, (o.rhythmBits&0x01) != 0)
	o.setDrumKey(8, 1, (o.rhythmBits&0x02) != 0)
	o.setDrumKey(8, 0, (o.rhythmBits&0x04) != 0)
	o.setDrumKey(7, 1, (o.rhythmBits&0x08) != 0)
	o.setDrumKey(6, 0, (o.rhythmBits&0x10) != 0)
	o.setDrumKey(6, 1, (o.rhythmBits&0x10) != 0)

	if prevMode && prevBits == o.rhythmBits {
		return
	}
	for ch := 6; ch <= 8; ch++ {
		o.syncChannelActive(ch)
	}
}

func (o *Synth) setDrumKey(ch int, op int, on bool) {
	if ch < 0 || ch >= len(o.ch) || op < 0 || op >= len(o.ch[ch].ops) {
		return
	}
	if on {
		o.ch[ch].ops[op].keyMask |= oplKeyMaskDrum
	} else {
		o.ch[ch].ops[op].keyMask &^= oplKeyMaskDrum
		if o.ch[ch].ops[op].egRout >= oplEnvelopeSilent {
			o.ch[ch].ops[op].stage = oplEnvRelease
		}
	}
}

func (o *Synth) refreshChannelFreq(ch int) {
	base, ci := oplBaseAndChannel(ch)
	if ci < 0 {
		return
	}
	a := o.regs[base+0xA0+ci]
	b := o.regs[base+0xB0+ci]
	o.ch[ch].fnum = uint16(a) | (uint16(b&0x03) << 8)
	o.ch[ch].block = (b >> 2) & 0x07
	o.ch[ch].ksv = (o.ch[ch].block << 1) | uint8((o.ch[ch].fnum>>(0x09-o.noteSelect))&0x01)
	for op := 0; op < opl3OperatorCount; op++ {
		o.updateOperatorKSL(ch, op)
	}
}

func (o *Synth) refreshChannelControl(ch int) {
	base, ci := oplBaseAndChannel(ch)
	if ci < 0 {
		return
	}
	c0 := o.regs[base+0xC0+ci]
	o.ch[ch].additive = (c0 & 0x01) != 0
	o.ch[ch].feedback = (c0 >> 1) & 0x07
	if o.opl2Mode {
		o.ch[ch].panL, o.ch[ch].panR = oplPanGainUnit, oplPanGainUnit
		o.updateRenderMode(ch)
		return
	}
	left := (c0 & 0x10) != 0
	right := (c0 & 0x20) != 0
	if o.stereoExt {
		o.updateRenderMode(ch)
		return
	}
	switch {
	case left && right:
		o.ch[ch].panL, o.ch[ch].panR = oplPanGainUnit, oplPanGainUnit
	case left:
		o.ch[ch].panL, o.ch[ch].panR = 0, oplPanGainUnit
	case right:
		o.ch[ch].panL, o.ch[ch].panR = oplPanGainUnit, 0
	default:
		o.ch[ch].panL, o.ch[ch].panR = oplPanGainUnit, oplPanGainUnit
	}
	o.updateRenderMode(ch)
}

func (o *Synth) refreshChannelStereoPan(ch int) {
	base, ci := oplBaseAndChannel(ch)
	if ci < 0 {
		return
	}
	pan := o.regs[base+0xD0+ci]
	o.ch[ch].panL, o.ch[ch].panR = oplStereoPanGainsFixed(pan)
	o.updateRenderMode(ch)
}

func (o *Synth) refreshOperator(ch int, op int) {
	base, ci := oplBaseAndChannel(ch)
	if ci < 0 || op < 0 || op >= opl3OperatorCount {
		return
	}
	slot := oplSlotForChannelOp(ci, op)
	if slot < 0 {
		return
	}
	s := &o.ch[ch].ops[op]
	reg20 := o.regs[base+0x20+slot]
	reg40 := o.regs[base+0x40+slot]
	reg60 := o.regs[base+0x60+slot]
	reg80 := o.regs[base+0x80+slot]
	regE0 := o.regs[base+0xE0+slot]

	s.regTrem = (reg20 & 0x80) != 0
	s.regVib = (reg20 & 0x40) != 0
	s.regType = (reg20 & 0x20) != 0
	s.regKSR = (reg20 & 0x10) != 0
	s.regMult = reg20 & 0x0F
	s.regKSL = (reg40 >> 6) & 0x03
	s.regTL = reg40 & 0x3F
	s.regAR = (reg60 >> 4) & 0x0F
	s.regDR = reg60 & 0x0F
	s.regSL = (reg80 >> 4) & 0x0F
	if s.regSL == 0x0F {
		s.regSL = 0x1F
	}
	s.regRR = reg80 & 0x0F
	s.regWave = regE0 & 0x07
	if o.opl2Mode {
		if !o.waveformSelectOn {
			s.regWave = 0
		} else {
			s.regWave &= 0x03
		}
	} else if !o.waveformSelectOn {
		s.regWave &= 0x03
	}
	o.updateOperatorKSL(ch, op)
	o.updateRenderMode(ch)
}

func (o *Synth) updateRenderMode(ch int) {
	if ch < 0 || ch >= len(o.ch) {
		return
	}
	c := &o.ch[ch]
	c.render = oplRenderModeGeneric
	if o.opl2Mode && c.feedback != 0 {
		return
	}
	if o.stereoExt || c.ops[0].regWave != 0 || c.ops[1].regWave != 0 {
		return
	}
	panStereo, panLeft, panRight := classifyRenderPan(c.panL, c.panR)
	if !panStereo && !panLeft && !panRight {
		return
	}
	static := !c.ops[0].regVib && !c.ops[1].regVib && !c.ops[0].regTrem && !c.ops[1].regTrem
	if c.additive {
		if c.feedback == 0 {
			if static {
				c.render = selectRenderModePan(panStereo, panLeft,
					oplRenderModeWave0AdditiveStaticStereo,
					oplRenderModeWave0AdditiveStaticLeft,
					oplRenderModeWave0AdditiveStaticRight)
			} else {
				c.render = selectRenderModePan(panStereo, panLeft,
					oplRenderModeWave0AdditiveStereo,
					oplRenderModeWave0AdditiveLeft,
					oplRenderModeWave0AdditiveRight)
			}
		}
		return
	}
	if c.feedback == 0 {
		if static {
			c.render = selectRenderModePan(panStereo, panLeft,
				oplRenderModeWave0FMStaticStereo,
				oplRenderModeWave0FMStaticLeft,
				oplRenderModeWave0FMStaticRight)
		} else {
			c.render = selectRenderModePan(panStereo, panLeft,
				oplRenderModeWave0FMStereo,
				oplRenderModeWave0FMLeft,
				oplRenderModeWave0FMRight)
		}
		return
	}
	if static {
		c.render = selectRenderModePan(panStereo, panLeft,
			oplRenderModeWave0FMFeedbackStaticStereo,
			oplRenderModeWave0FMFeedbackStaticLeft,
			oplRenderModeWave0FMFeedbackStaticRight)
	} else {
		c.render = selectRenderModePan(panStereo, panLeft,
			oplRenderModeWave0FMFeedbackStereo,
			oplRenderModeWave0FMFeedbackLeft,
			oplRenderModeWave0FMFeedbackRight)
	}
}

func classifyRenderPan(panL int32, panR int32) (stereo bool, left bool, right bool) {
	switch {
	case panL == oplPanGainUnit && panR == oplPanGainUnit:
		return true, false, false
	case panL == oplPanGainUnit && panR == 0:
		return false, true, false
	case panL == 0 && panR == oplPanGainUnit:
		return false, false, true
	default:
		return false, false, false
	}
}

func selectRenderModePan(stereo bool, left bool, stereoMode oplRenderMode, leftMode oplRenderMode, rightMode oplRenderMode) oplRenderMode {
	if stereo {
		return stereoMode
	}
	if left {
		return leftMode
	}
	return rightMode
}

func (o *Synth) updateOperatorKSL(ch int, op int) {
	if ch < 0 || ch >= len(o.ch) || op < 0 || op >= opl3OperatorCount {
		return
	}
	fnumIndex := int(o.ch[ch].fnum >> 6)
	if fnumIndex < 0 {
		fnumIndex = 0
	} else if fnumIndex >= len(oplKSLROM) {
		fnumIndex = len(oplKSLROM) - 1
	}
	ksl := (int(oplKSLROM[fnumIndex]) << 2) - ((0x08 - int(o.ch[ch].block)) << 5)
	if ksl < 0 {
		ksl = 0
	}
	s := &o.ch[ch].ops[op]
	s.egKSL = uint8(ksl)
	s.egBase = uint16(int(s.regTL<<2) + int(s.egKSL>>oplKSLShift[s.regKSL]))
}

func (o *Synth) advanceChipState() {
	if (o.timer & 0x3F) == 0x3F {
		o.tremoloPos = (o.tremoloPos + 1) % 210
	}
	if o.tremoloPos < 105 {
		o.tremolo = o.tremoloPos >> o.tremoloShift
	} else {
		o.tremolo = (210 - o.tremoloPos) >> o.tremoloShift
	}

	if (o.timer & 0x3FF) == 0x3FF {
		o.vibPos = (o.vibPos + 1) & 7
	}
	o.timer++

	if o.egState != 0 {
		shift := uint8(0)
		for shift < 13 && ((o.egTimer>>shift)&0x01) == 0 {
			shift++
		}
		if shift > 12 {
			o.egAdd = 0
		} else {
			o.egAdd = shift + 1
		}
		o.egTimerLo = uint8(o.egTimer & 0x03)
		o.egTimer++
	}
	o.egState ^= 1
	o.noise = oplAdvanceNoiseN(o.noise, 36)
}

func (o *Synth) advanceNoise() {
	noise := o.noise
	if noise == 0 {
		noise = 1
	}
	nBit := ((noise >> 14) ^ noise) & 0x01
	o.noise = (noise >> 1) | (nBit << 22)
}

func oplAdvanceNoiseN(noise uint32, steps int) uint32 {
	if noise == 0 {
		noise = 1
	}
	for i := 0; i < steps; i++ {
		nBit := ((noise >> 14) ^ noise) & 0x01
		noise = (noise >> 1) | (nBit << 22)
	}
	return noise
}

func (o *Synth) syncChannelActive(ch int) {
	if ch < 0 || ch >= len(o.ch) {
		return
	}
	if channelHasActiveOperators(&o.ch[ch]) {
		o.activeMask |= 1 << uint(ch)
		return
	}
	o.activeMask &^= 1 << uint(ch)
}

func decodeOperatorSlot(bank int, slot int) (ch int, op int, ok bool) {
	if slot < 0 || slot >= len(oplSlotToChannel) {
		return 0, 0, false
	}
	localCh := oplSlotToChannel[slot]
	localOp := oplSlotToOperator[slot]
	if localCh < 0 || localOp < 0 {
		return 0, 0, false
	}
	return bank*9 + localCh, localOp, true
}

func operatorKeyOn(op *impSynthOperatorState) bool {
	return op != nil && op.keyMask != 0
}

func operatorHasSound(op *impSynthOperatorState) bool {
	return op != nil && (op.keyMask != 0 || op.egRout < oplEnvelopeSilent)
}

func channelHasActiveOperators(c *impSynthChannelState) bool {
	if c == nil {
		return false
	}
	return operatorHasSound(&c.ops[0]) || operatorHasSound(&c.ops[1])
}

func oplRhythmHighHatPhase(rmXor uint8, noise uint32) int {
	phase := int(rmXor) << 9
	if (rmXor ^ uint8(noise&1)) != 0 {
		phase |= 0xD0
	} else {
		phase |= 0x34
	}
	return phase
}

func oplRhythmSnarePhase(hhBit8 uint8, noise uint32) int {
	out := (int(hhBit8) << 9) | (int(hhBit8^uint8(noise&1)) << 8)
	return out
}

func oplRhythmTopCymbalPhase(rmXor uint8) int {
	return (int(rmXor) << 9) | 0x80
}

func (o *Synth) rmXor() uint8 {
	return (o.rmHHBit2 ^ o.rmHHBit7) |
		(o.rmHHBit3 ^ o.rmTCBit5) |
		(o.rmTCBit3 ^ o.rmTCBit5)
}

func oplBaseAndChannel(ch int) (base int, ci int) {
	if ch < 0 || ch >= opl3ChannelCount {
		return 0, -1
	}
	if ch < 9 {
		return 0x000, ch
	}
	return 0x100, ch - 9
}

func oplSlotForChannelOp(ch int, op int) int {
	modSlots := [9]int{0, 1, 2, 8, 9, 10, 16, 17, 18}
	carSlots := [9]int{3, 4, 5, 11, 12, 13, 19, 20, 21}
	if ch < 0 || ch >= 9 {
		return -1
	}
	if op == 0 {
		return modSlots[ch]
	}
	return carSlots[ch]
}

func buildOPLWaveTables() {
	for wave := 0; wave < len(oplWaveTable); wave++ {
		for i := 0; i < oplWaveTableSize; i++ {
			oplWaveTable[wave][i] = oplWaveSample(wave, i)
		}
	}
}

func oplWaveSample(wave int, idx int) float64 {
	idx &= oplWaveTableMask
	phase := float64(idx) / float64(oplWaveTableSize)
	switch wave & 0x07 {
	case 0:
		return math.Sin(phase * 2 * math.Pi)
	case 1:
		if idx >= 512 {
			return 0
		}
		return math.Sin((float64(idx) / 512.0) * math.Pi)
	case 2:
		return math.Abs(math.Sin(phase * 2 * math.Pi))
	case 3:
		if (idx & 0x100) != 0 {
			return 0
		}
		return math.Sin((float64(idx&0x0FF) / 256.0) * (math.Pi / 2))
	case 4:
		if idx >= 512 {
			return 0
		}
		if idx < 256 {
			return math.Sin((float64(idx) / 256.0) * math.Pi)
		}
		return -math.Sin((float64(idx-256) / 256.0) * math.Pi)
	case 5:
		if idx >= 512 {
			return 0
		}
		if idx < 256 {
			return math.Sin((float64(idx) / 256.0) * math.Pi)
		}
		return math.Sin((float64(idx-256) / 256.0) * math.Pi)
	case 6:
		if idx < 512 {
			return 1
		}
		return -1
	default:
		if idx < 512 {
			return 1 - float64(idx)/256.0
		}
		return float64(idx-512)/256.0 - 1
	}
}

func oplStereoPanGains(pan uint8) (float64, float64) {
	left := math.Sin((float64(255-pan) * math.Pi) / 512.0)
	right := math.Sin((float64(pan) * math.Pi) / 512.0)
	return left, right
}

func oplStereoPanGainsFixed(pan uint8) (int32, int32) {
	left, right := oplStereoPanGains(pan)
	l := int32(math.Round(left * float64(oplPanGainUnit)))
	r := int32(math.Round(right * float64(oplPanGainUnit)))
	if l < 0 {
		l = 0
	} else if l > oplPanGainUnit {
		l = oplPanGainUnit
	}
	if r < 0 {
		r = 0
	} else if r > oplPanGainUnit {
		r = oplPanGainUnit
	}
	return l, r
}

func buildOPLAttenuationTable() {
	for i := 0; i < len(oplAttenTable); i++ {
		oplAttenTable[i] = math.Exp2(-float64(i) / 32.0)
	}
}

func buildOPLEnvelopeShiftTable() {
	for rate := 0; rate < 64; rate++ {
		rateHi := rate >> 2
		rateLo := rate & 0x03
		for egAdd := 0; egAdd < 14; egAdd++ {
			for timerLo := 0; timerLo < 4; timerLo++ {
				for egState := 0; egState < 2; egState++ {
					shift := 0
					if rateHi < 12 {
						if egState != 0 {
							switch rateHi + egAdd {
							case 12:
								shift = 1
							case 13:
								shift = (rateLo >> 1) & 0x01
							case 14:
								shift = rateLo & 0x01
							}
						}
					} else {
						shift = (rateHi & 0x03) + int(oplEGIncStep[rateLo][timerLo])
						if (shift & 0x04) != 0 {
							shift = 0x03
						}
						if shift == 0 {
							shift = egState
						}
					}
					oplEnvShift[rate][egAdd][timerLo][egState] = uint8(shift)
				}
			}
		}
	}
}

func buildOPLWave0LUT() {
	for phase := 0; phase < oplWaveTableSize; phase++ {
		for envelope := 0; envelope <= oplEnvelopeMax; envelope++ {
			oplWave0LUT[phase][envelope] = int16(oplWaveOutput0(uint16(phase), uint16(envelope)))
		}
	}
}

func clampEnvelope(v int) uint16 {
	if v < 0 {
		return 0
	}
	if v > oplEnvelopeSilent {
		return oplEnvelopeSilent
	}
	return uint16(v)
}

func clampAtten(v int) int {
	if v < 0 {
		return 0
	}
	if v > 0x3ff {
		return 0x3ff
	}
	return v
}

func clampSample(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

func clampPCM16(v int32) int16 {
	if v < -32768 {
		return -32768
	}
	if v > 32767 {
		return 32767
	}
	return int16(v)
}

func feedbackSample(v int) int16 {
	return clampPCM16(int32(v))
}

func lerpPCM16(prev int32, next int32, phase uint64) int32 {
	return prev + int32((int64(next-prev)*int64(phase))>>32)
}

func applyPanGain(sample int, gain int32) int32 {
	// Old path scale: sample / 4096 * gain * 0.125 * 32767.
	// Since 4096*8 == 32768, this reduces to a near-unity fixed-point scale.
	scaled := int64(sample) * int64(gain) * 32767
	return int32(scaled >> (15 + 15))
}

func phaseModFromSample(op *impSynthOperatorState, sample float64) int {
	scale := oplPhaseModScale
	if op != nil {
		scale *= oplWavePhaseModScale[op.regWave&0x07]
	}
	return int(math.Round(sample * scale))
}

func oplFeedbackPhaseOffset(prev0, prev1 int16, feedback uint8) int {
	if feedback == 0 {
		return 0
	}
	sum := int32(prev0) + int32(prev1)
	shift := 9 - int(feedback)
	if shift <= 0 {
		return int(sum)
	}
	return int(sum >> shift)
}
