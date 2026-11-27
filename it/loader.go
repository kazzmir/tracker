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

func (note Note) GetNotePosition() int {
	if note.fields&hasPitch != 0 && note.pitch < 120 {
		return int(note.pitch)
	}
	return -1
}
func (note Note) GetName() string {
	if note.fields&hasPitch == 0 {
		return "..."
	}
	switch note.pitch {
	case 255:
		return "==="
	case 254:
		return "^^^"
	}
	if note.pitch >= 120 {
		return "~~~"
	}
	return fmt.Sprintf("%s%d", [...]string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}[note.pitch%12], note.pitch/12)
}
func (note Note) GetSampleName() string {
	if note.instrument == 0 {
		return ".."
	}
	return fmt.Sprintf("%02d", note.instrument)
}
func (note Note) GetEffectName() string {
	if note.command < 1 || note.command > 26 {
		return "..."
	}
	return fmt.Sprintf("%c%02X", 'A'+note.command-1, note.param)
}

var le = binary.LittleEndian

func Load(reader io.Reader) (*Module, error) {
	var magic [4]byte
	if _, err := io.ReadFull(reader, magic[:]); err != nil || string(magic[:]) != "IMPM" {
		return nil, ErrNotIT
	}
	const maxFile = 64 << 20
	rest, err := io.ReadAll(io.LimitReader(reader, maxFile-3))
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
	module := &Module{name: strings.TrimSpace(string(bytes.TrimRight(data[4:30], "\x00"))), flags: flags,
		speed: max(1, int(data[50])), tempo: max(32, int(data[51])), globalVolume: min(128, int(data[48])), mixVolume: min(128, int(data[49]&127)), separation: min(128, int(data[52]))}
	copy(module.pan[:], data[64:128])
	copy(module.channelVolume[:], data[128:192])
	table := 192 + numOrders
	if table+4*(numInst+numSamples+numPatterns) > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	module.orders = data[192:table]
	readOffset := func() uint32 { offset := le.Uint32(data[table:]); table += 4; return offset }
	for index := range numInst {
		offset := readOffset()
		var inst instrument
		if offset != 0 {
			header, err := section(data, offset, 554)
			if err != nil || string(header[:4]) != "IMPI" {
				return nil, fmt.Errorf("invalid IT instrument %d", index+1)
			}
			inst.nna, inst.dct, inst.dca = int(header[17]), int(header[18]), int(header[19])
			inst.fadeout = int(le.Uint16(header[20:]))
			inst.globalVolume = min(128, int(header[24]))
			inst.pan = int(header[25])
			for key := range 120 {
				inst.notes[key], inst.samples[key] = header[64+key*2], header[65+key*2]
			}
			inst.volume = readEnvelope(header[304:])
			inst.panning = readEnvelope(header[386:])
			inst.pitch = readEnvelope(header[468:])
		}
		module.instruments = append(module.instruments, inst)
	}
	budget := 16 << 20 // decoded float samples, including both stereo planes
	for index := range numSamples {
		offset := readOffset()
		var decoded sample
		if offset != 0 {
			header, err := section(data, offset, 80)
			if err != nil || string(header[:4]) != "IMPS" {
				return nil, fmt.Errorf("invalid IT sample %d", index+1)
			}
			decoded, err = readSample(data, header, &budget)
			if err != nil {
				return nil, fmt.Errorf("IT sample %d: %w", index+1, err)
			}
		}
		module.samples = append(module.samples, decoded)
	}
	for index := range numPatterns {
		pat, channels, err := readPattern(data, readOffset())
		if err != nil {
			return nil, fmt.Errorf("IT pattern %d: %w", index, err)
		}
		module.channels = max(module.channels, channels)
		module.patterns = append(module.patterns, pat)
	}
	module.channels = max(1, module.channels)
	return module, nil
}

func section(data []byte, offset uint32, count int) ([]byte, error) {
	if uint64(offset)+uint64(count) > uint64(len(data)) {
		return nil, io.ErrUnexpectedEOF
	}
	return data[int(offset) : int(offset)+count], nil
}

func readEnvelope(header []byte) envelope {
	env := envelope{flags: int(header[0]), count: min(25, int(header[1])), loopStart: int(header[2]), loopEnd: int(header[3]), susStart: int(header[4]), susEnd: int(header[5])}
	for index := range env.count {
		offset := 6 + 3*index
		env.points[index] = point{int(le.Uint16(header[offset+1:])), int(int8(header[offset]))}
	}
	return env
}

func readSample(data, header []byte, budget *int) (sample, error) {
	flags, convert := header[18], header[46]
	decoded := sample{c5: max(1, int(le.Uint32(header[60:]))), volume: min(64, int(header[19])), globalVolume: min(64, int(header[17])), pan: int(header[47])}
	length := le.Uint32(header[48:])
	if length == 0 || flags&1 == 0 {
		return decoded, nil
	}
	planes, bits := 1, 8
	if flags&4 != 0 {
		planes = 2
	}
	if flags&2 != 0 {
		bits = 16
	}
	if uint64(length)*uint64(planes) > uint64(*budget) {
		return decoded, fmt.Errorf("decoded IT samples exceed size limit")
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
		decoded.loop = readLoop(le.Uint32(header[52:]), le.Uint32(header[56:]), flags&64 != 0)
	}
	if flags&32 != 0 {
		decoded.sustain = readLoop(le.Uint32(header[64:]), le.Uint32(header[68:]), flags&128 != 0)
	}
	offset := le.Uint32(header[72:])
	if uint64(offset) > uint64(len(data)) {
		return decoded, io.ErrUnexpectedEOF
	}
	raw := data[int(offset):]
	var err error
	decoded.left = make([]float32, int(length))
	if planes == 2 {
		decoded.right = make([]float32, int(length))
	}
	for _, dst := range [][]float32{decoded.left, decoded.right} {
		if len(dst) == 0 {
			continue
		}
		if flags&8 != 0 {
			raw, err = decompress(raw, dst, bits, convert&4 != 0)
			if err != nil {
				return decoded, err
			}
			continue
		}
		if convert&^7 != 0 {
			return decoded, fmt.Errorf("unsupported IT sample conversion 0x%x", convert)
		}
		size := len(dst) * (bits / 8)
		if len(raw) < size {
			return decoded, io.ErrUnexpectedEOF
		}
		var delta uint16
		for index := range dst {
			var value uint16
			if bits == 8 {
				value = uint16(raw[index])
			} else if convert&2 != 0 {
				value = binary.BigEndian.Uint16(raw[index*2:])
			} else {
				value = le.Uint16(raw[index*2:])
			}
			if convert&4 != 0 {
				delta += value
				value = delta
			}
			if bits == 8 {
				if convert&1 == 0 {
					value ^= 128
				}
				dst[index] = float32(int8(value)) / 128
			} else {
				if convert&1 == 0 {
					value ^= 32768
				}
				dst[index] = float32(int16(value)) / 32768
			}
		}
		raw = raw[size:]
	}
	return decoded, nil
}

func readPattern(data []byte, offset uint32) (pattern, int, error) {
	if offset == 0 {
		return pattern{rows: make([][maxChannels]Note, 64)}, 0, nil
	}
	header, err := section(data, offset, 8)
	if err != nil {
		return pattern{}, 0, err
	}
	rows := int(le.Uint16(header[2:]))
	if rows < 1 || rows > 1024 {
		return pattern{}, 0, fmt.Errorf("invalid row count %d", rows)
	}
	packed, err := section(data, offset+8, int(le.Uint16(header)))
	if err != nil {
		return pattern{}, 0, err
	}
	pat := pattern{rows: make([][maxChannels]Note, rows)}
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
		note := &pat.rows[row][ch]
		for index, pair := range [][2]*byte{{&note.pitch, &old.pitch}, {&note.instrument, &old.instrument}, {&note.volume, &old.volume}, {&note.command, &old.command}} {
			if mask&(1<<index) != 0 {
				*pair[1], err = in.ReadByte()
				if err != nil {
					return pattern{}, 0, err
				}
				if index == 3 {
					old.param, err = in.ReadByte()
					if err != nil {
						return pattern{}, 0, err
					}
				}
			}
			if (mask|mask>>4)&(1<<index) != 0 {
				*pair[0] = *pair[1]
				note.fields |= 1 << index
				if index == 3 {
					note.param = old.param
				}
			}
		}
	}
	return pat, channels, nil
}
