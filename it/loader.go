// Package it loads and plays Impulse Tracker modules in pure Go.
package it

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
)

var ErrNotIT = errors.New("not an Impulse Tracker module")

const maxChannels = 64

type Module struct {
	name                                              string
	orders                                            []byte
	patterns                                          []pattern
	samples                                           []sample
	instruments                                       []instrument
	channels                                          int
	pan, channelVolume                                [maxChannels]byte
	speed, tempo, globalVolume, mixVolume, separation int
	flags                                             uint16
}

type sample struct {
	left, right                   []float32
	c5, volume, globalVolume, pan int
	loop, sustain                 sampleLoop
}
type sampleLoop struct {
	start, end int
	pingPong   bool
}

type instrument struct {
	notes, samples                            [120]byte
	nna, dct, dca, fadeout, globalVolume, pan int
	volume, panning, pitch                    envelope
}
type envelope struct {
	flags, count, loopStart, loopEnd, susStart, susEnd int
	points                                             [25]point
}
type point struct{ tick, value int }

type pattern struct{ rows [][maxChannels]Note }

type Note struct {
	pitch, instrument, volume, command, param byte
	fields                                    byte
}

const (
	hasPitch = 1 << iota
	hasInstrument
	hasVolume
	hasCommand
)

func (n Note) GetNotePosition() int {
	if n.fields&hasPitch != 0 && n.pitch < 120 {
		return int(n.pitch)
	}
	return -1
}
func (n Note) GetName() string {
	if n.fields&hasPitch == 0 {
		return "..."
	}
	switch n.pitch {
	case 255:
		return "==="
	case 254:
		return "^^^"
	}
	if n.pitch >= 120 {
		return "~~~"
	}
	return fmt.Sprintf("%s%d", [...]string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}[n.pitch%12], n.pitch/12)
}
func (n Note) GetSampleName() string {
	if n.instrument == 0 {
		return ".."
	}
	return fmt.Sprintf("%02d", n.instrument)
}
func (n Note) GetEffectName() string {
	if n.command < 1 || n.command > 26 {
		return "..."
	}
	return fmt.Sprintf("%c%02X", 'A'+n.command-1, n.param)
}

var le = binary.LittleEndian

func Load(r io.Reader) (*Module, error) {
	var magic [4]byte
	if _, err := io.ReadFull(r, magic[:]); err != nil || string(magic[:]) != "IMPM" {
		return nil, ErrNotIT
	}
	const maxFile = 64 << 20
	rest, err := io.ReadAll(io.LimitReader(r, maxFile-3))
	if err != nil {
		return nil, err
	}
	if len(rest) > maxFile-4 {
		return nil, fmt.Errorf("IT module exceeds %d bytes", maxFile)
	}
	data := append(magic[:], rest...)
	if len(data) < 192 {
		return nil, io.ErrUnexpectedEOF
	}
	numOrders, numInst, numSamples, numPatterns := int(le.Uint16(data[32:])), int(le.Uint16(data[34:])), int(le.Uint16(data[36:])), int(le.Uint16(data[38:]))
	if numOrders == 0 || numOrders > 256 || numInst > 256 || numSamples > 256 || numPatterns > 254 {
		return nil, fmt.Errorf("unsupported IT order, instrument, sample or pattern count")
	}
	flags, compatible := le.Uint16(data[44:]), le.Uint16(data[42:])
	if flags&4 != 0 && compatible < 0x200 {
		return nil, fmt.Errorf("old IT instrument format is not supported")
	}
	m := &Module{name: strings.TrimSpace(string(bytes.TrimRight(data[4:30], "\x00"))), flags: flags,
		speed: max(1, int(data[50])), tempo: max(32, int(data[51])), globalVolume: min(128, int(data[48])), mixVolume: min(128, int(data[49]&127)), separation: min(128, int(data[52]))}
	copy(m.pan[:], data[64:128])
	copy(m.channelVolume[:], data[128:192])
	table := 192 + numOrders
	if table+4*(numInst+numSamples+numPatterns) > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	m.orders = data[192:table]
	readOffset := func() uint32 { o := le.Uint32(data[table:]); table += 4; return o }
	for i := range numInst {
		o := readOffset()
		var inst instrument
		if o != 0 {
			h, err := section(data, o, 554)
			if err != nil || string(h[:4]) != "IMPI" {
				return nil, fmt.Errorf("invalid IT instrument %d", i+1)
			}
			inst.nna, inst.dct, inst.dca = int(h[17]), int(h[18]), int(h[19])
			inst.fadeout = int(le.Uint16(h[20:]))
			inst.globalVolume = min(128, int(h[24]))
			inst.pan = int(h[25])
			for n := range 120 {
				inst.notes[n], inst.samples[n] = h[64+n*2], h[65+n*2]
			}
			inst.volume = readEnvelope(h[304:])
			inst.panning = readEnvelope(h[386:])
			inst.pitch = readEnvelope(h[468:])
		}
		m.instruments = append(m.instruments, inst)
	}
	budget := 16 << 20 // decoded float samples, including both stereo planes
	for i := range numSamples {
		o := readOffset()
		var s sample
		if o != 0 {
			h, err := section(data, o, 80)
			if err != nil || string(h[:4]) != "IMPS" {
				return nil, fmt.Errorf("invalid IT sample %d", i+1)
			}
			s, err = readSample(data, h, &budget)
			if err != nil {
				return nil, fmt.Errorf("IT sample %d: %w", i+1, err)
			}
		}
		m.samples = append(m.samples, s)
	}
	for i := range numPatterns {
		pat, channels, err := readPattern(data, readOffset())
		if err != nil {
			return nil, fmt.Errorf("IT pattern %d: %w", i, err)
		}
		m.channels = max(m.channels, channels)
		m.patterns = append(m.patterns, pat)
	}
	m.channels = max(1, m.channels)
	return m, nil
}

func section(data []byte, offset uint32, count int) ([]byte, error) {
	if uint64(offset)+uint64(count) > uint64(len(data)) {
		return nil, io.ErrUnexpectedEOF
	}
	return data[int(offset) : int(offset)+count], nil
}

func readEnvelope(h []byte) envelope {
	e := envelope{flags: int(h[0]), count: min(25, int(h[1])), loopStart: int(h[2]), loopEnd: int(h[3]), susStart: int(h[4]), susEnd: int(h[5])}
	for i := range e.count {
		o := 6 + 3*i
		e.points[i] = point{int(le.Uint16(h[o+1:])), int(int8(h[o]))}
	}
	return e
}

func readSample(data, h []byte, budget *int) (sample, error) {
	flags, convert := h[18], h[46]
	s := sample{c5: max(1, int(le.Uint32(h[60:]))), volume: min(64, int(h[19])), globalVolume: min(64, int(h[17])), pan: int(h[47])}
	length := le.Uint32(h[48:])
	if length == 0 || flags&1 == 0 {
		return s, nil
	}
	planes, bits := 1, 8
	if flags&4 != 0 {
		planes = 2
	}
	if flags&2 != 0 {
		bits = 16
	}
	if uint64(length)*uint64(planes) > uint64(*budget) {
		return s, fmt.Errorf("decoded IT samples exceed size limit")
	}
	*budget -= int(length) * planes
	readLoop := func(start, end uint32, ping bool) sampleLoop {
		end = min(length, end)
		if start >= end || end-start < 2 {
			return sampleLoop{}
		}
		return sampleLoop{int(start), int(end), ping}
	}
	if flags&16 != 0 {
		s.loop = readLoop(le.Uint32(h[52:]), le.Uint32(h[56:]), flags&64 != 0)
	}
	if flags&32 != 0 {
		s.sustain = readLoop(le.Uint32(h[64:]), le.Uint32(h[68:]), flags&128 != 0)
	}
	offset := le.Uint32(h[72:])
	if uint64(offset) > uint64(len(data)) {
		return s, io.ErrUnexpectedEOF
	}
	raw := data[int(offset):]
	var err error
	s.left = make([]float32, int(length))
	if planes == 2 {
		s.right = make([]float32, int(length))
	}
	for _, dst := range [][]float32{s.left, s.right} {
		if len(dst) == 0 {
			continue
		}
		if flags&8 != 0 {
			raw, err = decompress(raw, dst, bits, convert&4 != 0)
			if err != nil {
				return s, err
			}
			continue
		}
		if convert&^7 != 0 {
			return s, fmt.Errorf("unsupported IT sample conversion 0x%x", convert)
		}
		size := len(dst) * (bits / 8)
		if len(raw) < size {
			return s, io.ErrUnexpectedEOF
		}
		var delta uint16
		for i := range dst {
			var v uint16
			if bits == 8 {
				v = uint16(raw[i])
			} else if convert&2 != 0 {
				v = binary.BigEndian.Uint16(raw[i*2:])
			} else {
				v = le.Uint16(raw[i*2:])
			}
			if convert&4 != 0 {
				delta += v
				v = delta
			}
			if bits == 8 {
				if convert&1 == 0 {
					v ^= 128
				}
				dst[i] = float32(int8(v)) / 128
			} else {
				if convert&1 == 0 {
					v ^= 32768
				}
				dst[i] = float32(int16(v)) / 32768
			}
		}
		raw = raw[size:]
	}
	return s, nil
}

func readPattern(data []byte, offset uint32) (pattern, int, error) {
	if offset == 0 {
		return pattern{rows: make([][maxChannels]Note, 64)}, 0, nil
	}
	h, err := section(data, offset, 8)
	if err != nil {
		return pattern{}, 0, err
	}
	rows := int(le.Uint16(h[2:]))
	if rows < 1 || rows > 1024 {
		return pattern{}, 0, fmt.Errorf("invalid row count %d", rows)
	}
	packed, err := section(data, offset+8, int(le.Uint16(h)))
	if err != nil {
		return pattern{}, 0, err
	}
	p := pattern{rows: make([][maxChannels]Note, rows)}
	in := bytes.NewReader(packed)
	var masks [maxChannels]byte
	var last [maxChannels]Note
	channels := 0
	for row := 0; row < rows; {
		tag, err := in.ReadByte()
		if err != nil {
			return pattern{}, 0, err
		}
		if tag == 0 {
			row++
			continue
		}
		ch := int((tag - 1) & 63)
		channels = max(channels, ch+1)
		if tag&128 != 0 {
			masks[ch], err = in.ReadByte()
			if err != nil {
				return pattern{}, 0, err
			}
		}
		mask := masks[ch]
		old := &last[ch]
		n := &p.rows[row][ch]
		for i, pair := range [][2]*byte{{&n.pitch, &old.pitch}, {&n.instrument, &old.instrument}, {&n.volume, &old.volume}, {&n.command, &old.command}} {
			if mask&(1<<i) != 0 {
				*pair[1], err = in.ReadByte()
				if err != nil {
					return pattern{}, 0, err
				}
				if i == 3 {
					old.param, err = in.ReadByte()
					if err != nil {
						return pattern{}, 0, err
					}
				}
			}
			if (mask|mask>>4)&(1<<i) != 0 {
				*pair[0] = *pair[1]
				n.fields |= 1 << i
				if i == 3 {
					n.param = old.param
				}
			}
		}
	}
	return p, channels, nil
}
