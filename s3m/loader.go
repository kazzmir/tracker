package s3m

import (
    "bufio"
    "bytes"
    "fmt"
    "encoding/binary"
    "log"
    "io"
)

const (
    EffectNone int = 0
    EffectSetSpeed = 1
    EffectPatternJump = 2
    EffectPatternBreak = 3
    EffectVolumeSlide = 4
    EffectPortamentoDown = 5
    EffectPortamentoUp = 6
    EffectPortamentoToNote = 7
    EffectVibrato = 8
    EffectTremor = 9
    EffectArpeggio = 10
    EffectVibratoAndVolumeSlide = 11
    EffectPortamentoAndVolumeSlide = 12
    EffectSampleOffset = 15
    EffectRetriggerAndVolumeSlide = 17
    EffectTremolo = 18
    EffectSetExtra = 19
    EffectSetTempo = 20
    EffectFineVibrato = 21
    EffectGlobalVolume = 22
    EffectGlobalVolumeSlide = 23

    // not a defined effect in the s3m standard, but we use it for the extra delayed note effect
    EffectNoteDelay = 5000
)

type Instrument struct {
    Name string
    MiddleC uint32
    SampleFormat uint16
    Flags uint8
    Volume uint8
    Loop bool
    LoopBegin int
    LoopEnd int
    Data []float32
    RightData []float32 // nil for mono samples
    OPL *OPLPatch // non-nil for a melodic AdLib instrument
}

func (instrument *Instrument) playable() bool {
    return instrument != nil && (instrument.OPL != nil || len(instrument.Data) > 0)
}

type Note struct {
    SampleNumber int
    ChangeSample bool

    ChangeNote bool
    Note int // 0-120, 0 is C-1

    ChangeVolume bool
    Volume int // 0-64

    ChangeEffect bool
    EffectNumber uint8 // 0-15
    EffectParameter uint8 // 0-255

    Channel int // 0-31
}

func (note *Note) GetNotePosition() int {
    if note.ChangeNote && note.Note < 160 && note.Note & 15 < 12 {
        return note.Note / 16 * 12 + note.Note % 16
    }
    return -1
}

func (note *Note) GetName() string {
    if note.ChangeNote {
        if note.Note == 254 {
            return "^^^"
        }
        octave := note.Note / 16
        noteNumber := note.Note % 16

        names := []string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-", "", "", "", ""}

        return names[noteNumber] + fmt.Sprintf("%d", octave + 1)
    }

    return "..."
}

func (note *Note) GetEffectName() string {
    if note.EffectNumber > 0 || note.EffectParameter > 0 {
        if note.EffectNumber == EffectSetExtra {
            return fmt.Sprintf("S%X%X", note.EffectParameter >> 4, note.EffectParameter & 0xf)
        }

        return fmt.Sprintf("%c%02X", 'A' + note.EffectNumber - 1, note.EffectParameter)
    }

    return "..."
}

func (note *Note) GetSampleName() string {
    if note.SampleNumber > 0 {
        return fmt.Sprintf("%02d", note.SampleNumber)
    }
    return ".."
}

type Pattern struct {
    Rows [][]Note
}

type S3MFile struct {
    Name string
    Instruments []Instrument
    Patterns []Pattern
    Orders []byte
    SongLength int
    InitialSpeed uint8
    InitialTempo uint8
    ChannelMap map[int]int // maps channel number to channel index
    ChannelPanning map[int]int // indexed by playback channel, range 0–256
    GlobalVolume uint8
    MixVolume uint8
    FastVolumeSlides bool
    OldVibrato bool
    AmigaLimits bool
    impulseTracker bool
    separateEffectMemory bool
    screamTrackerGUS bool
}

func Load(reader_ io.ReadSeeker, logger *log.Logger) (*S3MFile, error) {
    reader := bufio.NewReader(reader_)

    name := make([]byte, 28)
    _, err := io.ReadFull(reader, name)
    if err != nil {
        return nil, err
    }

    name = bytes.TrimRight(name, "\x00")

    logger.Printf("Name: '%s'", string(name))

    // skip 0x1a byte
    _, err = reader.ReadByte()
    if err != nil {
        return nil, err
    }

    filetype, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    logger.Printf("Filetype: %v", filetype)
    // filetype should be 16, but not sure we really care

    // expansion bytes, unused
    _, err = reader.Discard(2)
    if err != nil {
        return nil, err
    }

    var songLength uint16
    err = binary.Read(reader, binary.LittleEndian, &songLength)
    if err != nil {
        return nil, err
    }
    logger.Printf("Song length: %v", songLength)


    var numInstruments uint16
    err = binary.Read(reader, binary.LittleEndian, &numInstruments)
    if err != nil {
        return nil, err
    }
    logger.Printf("Instruments: %v", numInstruments)

    var patternCount uint16
    err = binary.Read(reader, binary.LittleEndian, &patternCount)
    if err != nil {
        return nil, err
    }
    logger.Printf("Patterns: %v", patternCount)

    var flags uint16
    err = binary.Read(reader, binary.LittleEndian, &flags)
    if err != nil {
        return nil, err
    }
    logger.Printf("Flags: %v", flags)

    var trackerVersion uint16
    err = binary.Read(reader, binary.LittleEndian, &trackerVersion)
    if err != nil {
        return nil, err
    }
    logger.Printf("Tracker version: 0x%x", trackerVersion)

    var sampleFormat uint16
    err = binary.Read(reader, binary.LittleEndian, &sampleFormat)
    if err != nil {
        return nil, err
    }
    logger.Printf("Sample format: %v", sampleFormat)

    var signature [4]byte
    _, err = io.ReadFull(reader, signature[:])
    if err != nil {
        return nil, err
    }
    if !bytes.Equal(signature[:], []byte("SCRM")) {
        return nil, io.ErrUnexpectedEOF
    }

    globalVolume, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }

    logger.Printf("Global volume: %v", globalVolume)

    initialSpeed, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    logger.Printf("Initial speed: %v", initialSpeed)

    initialTempo, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    logger.Printf("Initial tempo: %v", initialTempo)

    masterVolume, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    logger.Printf("Master volume: %v", masterVolume)

    // ultraclick removal, skip
    _, err = reader.ReadByte()
    if err != nil {
        return nil, err
    }

    defaultPanning, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    logger.Printf("Default panning: %v", defaultPanning)

    // 8 more expansion bytes, skip
    _, err = reader.Discard(8)
    if err != nil {
        return nil, err
    }

    var special uint16
    err = binary.Read(reader, binary.LittleEndian, &special)
    if err != nil {
        return nil, err
    }
    logger.Printf("Special: %v", special)

    var channelSettings [32]byte
    _, err = io.ReadFull(reader, channelSettings[:])
    if err != nil {
        return nil, err
    }

    channelMap := make(map[int]int)
    channelPanning := make(map[int]int)

    channelCount := 0
    for i, setting := range channelSettings {
        logger.Printf("Channel %v setting: %v", i, setting)
        if setting < 25 {
            channelMap[i] = channelCount

            channelCount += 1

            logger.Printf("Channel %v default panning %v", i, setting)

            pan := 128
            if setting < 16 && masterVolume & 128 != 0 {
                pan = 51
                if setting & 8 != 0 {
                    pan = 204
                }
            }
            channelPanning[channelMap[i]] = pan
        }
    }

    logger.Printf("Channels in use: %v", channelCount)

    var orders []byte
    endOfSong := false
    for range songLength {
        order, err := reader.ReadByte()
        if err != nil {
            return nil, err
        }

        // log.Printf("Got pattern %v", order)

        if order == 0xff {
            endOfSong = true
        }
        if !endOfSong {
            orders = append(orders, order)
        }
    }

    var instrumentOffsets []uint16
    for range numInstruments {
        var offset uint16
        err := binary.Read(reader, binary.LittleEndian, &offset)
        if err != nil {
            return nil, err
        }

        instrumentOffsets = append(instrumentOffsets, offset)
        // log.Printf("Instrument %v offset 0x%x", len(instrumentOffsets)-1, offset << 4)
    }

    var patternOffsets []uint16
    for range patternCount {
        var offset uint16
        err := binary.Read(reader, binary.LittleEndian, &offset)
        if err != nil {
            return nil, err
        }

        patternOffsets = append(patternOffsets, offset)
        // log.Printf("Pattern %v offset 0x%x", len(patternOffsets)-1, offset << 4)
    }

    if defaultPanning == 0xfc {
        var data [32]byte
        if _, err := io.ReadFull(reader, data[:]); err != nil {
            return nil, err
        }
        for number, index := range channelMap {
            if pan := data[number]; pan & 0x20 != 0 {
                channelPanning[index] = (int(pan & 15) * 256 + 8)/15
            }
        }
    }

    mono := masterVolume & 128 == 0

    logger.Printf("Mono: %v", mono)

    var instruments []Instrument
    usesGUS := false

    for _, offset := range instrumentOffsets {
        _, err := reader_.Seek(int64(offset) << 4, io.SeekStart)
        if err != nil {
            return nil, err
        }

        buffer := bufio.NewReader(reader_)

        type_, err := buffer.ReadByte()
        if err != nil {
            return nil, err
        }

        // ignore name
        var name [12]byte
        _, err = io.ReadFull(buffer, name[:])
        if err != nil {
            return nil, err
        }

        // 1 is digital sample
        if type_ == 2 {
            // SCRI stores the OPL operator registers at offsets 16–27.
            var header [67]byte
            if _, err := io.ReadFull(buffer, header[:]); err != nil {
                return nil, err
            }
            if string(header[63:]) != "SCRI" {
                return nil, fmt.Errorf("invalid S3M AdLib instrument signature")
            }
            patch := new(OPLPatch)
            copy(patch[:], header[3:15])
            instruments = append(instruments, Instrument{
                Name: string(bytes.TrimRight(header[35:63], "\x00")),
                MiddleC: binary.LittleEndian.Uint32(header[19:23]),
                Volume: min(64, header[15]),
                OPL: patch,
            })
        } else if type_ == 1 {
            // read a 3 byte unsigned value
            var high uint8
            var low uint16
            high, err = buffer.ReadByte()
            if err != nil {
                return nil, err
            }
            err = binary.Read(buffer, binary.LittleEndian, &low)
            if err != nil {
                return nil, err
            }

            var total uint32 = (uint32(high) << 16) | uint32(low)

            var sampleLength uint32
            err = binary.Read(buffer, binary.LittleEndian, &sampleLength)
            if err != nil {
                return nil, err
            }

            var loopBegin, loopEnd uint32
            err = binary.Read(buffer, binary.LittleEndian, &loopBegin)
            if err != nil {
                return nil, err
            }
            err = binary.Read(buffer, binary.LittleEndian, &loopEnd)
            if err != nil {
                return nil, err
            }

            sampleVolume, err := buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            _, err = buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            packing, err := buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            if packing != 0 {
                return nil, fmt.Errorf("unsupported S3M PCM packing %d", packing)
            }

            flags, err := buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            var middleC uint32
            err = binary.Read(buffer, binary.LittleEndian, &middleC)
            if err != nil {
                return nil, err
            }

            var driverData [12]byte
            if _, err := io.ReadFull(buffer, driverData[:]); err != nil {
                return nil, err
            }
            // ST3's Sound Blaster driver writes only 0 or 1 here; the GUS
            // driver stores the sample's address in its own memory.
            usesGUS = usesGUS || binary.LittleEndian.Uint16(driverData[4:]) > 1

            var sampleName [28]byte
            _, err = io.ReadFull(buffer, sampleName[:])
            if err != nil {
                return nil, err
            }
            sampleNameTrim := bytes.TrimRight(sampleName[:], "\x00")

            var scrsSignature [4]byte
            _, err = io.ReadFull(buffer, scrsSignature[:])
            if err != nil {
                return nil, err
            }

            if !bytes.Equal(scrsSignature[:], []byte("SCRS")) {
                return nil, fmt.Errorf("Unexpected sample signature: %v", scrsSignature)
            }

            _, err = reader_.Seek(int64(total) * 16, io.SeekStart)
            if err != nil {
                return nil, err
            }

            buffer = bufio.NewReader(reader_)
            if sampleLength > 16<<20 {
                return nil, fmt.Errorf("S3M sample has too many frames: %d", sampleLength)
            }
            readPlane := func() ([]float32, error) {
                width := 1
                if flags & 4 != 0 { width = 2 }
                data := make([]byte, int(sampleLength)*width)
                if _, err := io.ReadFull(buffer, data); err != nil { return nil, err }
                pcm := make([]float32, int(sampleLength))
                for index := range pcm {
                    if width == 1 {
                        value := data[index]
                        if sampleFormat != 1 { value ^= 128 }
                        pcm[index] = float32(int8(value))/128
                    } else {
                        value := binary.LittleEndian.Uint16(data[index*2:])
                        if sampleFormat != 1 { value ^= 32768 }
                        pcm[index] = float32(int16(value))/32768
                    }
                }
                return pcm, nil
            }
            left, err := readPlane()
            if err != nil { return nil, fmt.Errorf("read S3M PCM: %w", err) }
            var right []float32
            if flags & 2 != 0 {
                right, err = readPlane()
                if err != nil { return nil, fmt.Errorf("read S3M right PCM: %w", err) }
            }

            instruments = append(instruments, Instrument{
                Name: string(sampleNameTrim),
                MiddleC: middleC,
                Volume: sampleVolume,
                Flags: flags,
                Loop: flags & 1 != 0,
                LoopBegin: int(loopBegin),
                LoopEnd: int(loopEnd),
                Data: left,
                RightData: right,
            })

            // log.Printf("Instrument %v loop begin %v end %v", i, loopBegin, loopEnd)
        } else {
            instruments = append(instruments, Instrument{
            })
        }

        // log.Printf("Instrument %v: type %v", i, type_)
    }

    var patterns []Pattern

    for _, offset := range patternOffsets {
        if offset == 0 {
            patterns = append(patterns, Pattern{Rows: make([][]Note, 64)})
            continue
        }
        var pattern Pattern
        _, err := reader_.Seek(int64(offset) << 4, io.SeekStart)
        if err != nil {
            return nil, fmt.Errorf("Could not seek to 0x%x for pattern %v: %v", offset << 4, len(patterns), err)
        }

        var patternLength uint16
        err = binary.Read(reader_, binary.LittleEndian, &patternLength)
        if err != nil {
            return nil, err
        }

        // log.Printf("Pattern %v length 0x%x", len(patterns), patternLength)

        // hack: some s3m files assume the pattern length includes the length bytes and some do not
        // for now just add 2 extra bytes and hope we read the rows properly
        limit := io.LimitReader(reader_, int64(patternLength) + 2)
        buffer := bufio.NewReader(limit)

        rows := make([]Note, channelCount)

        row := 0

        for row < 64 {
            marker, err := buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            // if len(patterns) == 54 {
            //    log.Printf("row %v marker %v", row, marker)
            // }

            /*
            if marker == 255 {
                continue
            }
            */

            if marker == 0 /* || marker == 255 */ {
                pattern.Rows = append(pattern.Rows, rows)
                rows = make([]Note, channelCount)
                row += 1
                continue
            }

            var note uint8
            hasNote := false
            hasSample := false
            hasVolume := false
            hasEffect := false
            var instrument uint8
            var volume uint8
            var effect uint8
            var effectParameter uint8

            channel := marker & 31
            if marker & 32 != 0 {
                hasNote = true
                hasSample = true
                note, err = buffer.ReadByte()
                if err != nil {
                    return nil, err
                }
                instrument, err = buffer.ReadByte()
                if err != nil {
                    return nil, err
                }
            }

            if marker & 64 != 0 {
                hasVolume = true
                volume, err = buffer.ReadByte()
                if err != nil {
                    return nil, err
                }
            }

            if marker & 128 != 0 {
                hasEffect = true
                effect, err = buffer.ReadByte()
                if err != nil {
                    return nil, err
                }
                effectParameter, err = buffer.ReadByte()
                if err != nil {
                    return nil, err
                }
            }

            noteObject := Note{
                SampleNumber: int(instrument),
                ChangeSample: hasSample && instrument != 0,
                Note: int(note),
                ChangeNote: hasNote && note != 255,
                Volume: int(volume),
                ChangeVolume: hasVolume,
                ChangeEffect: hasEffect,
                EffectNumber: effect,
                EffectParameter: effectParameter,
                Channel: int(channel),
            }

            // log.Printf("Read note %+v", noteObject)

            _, ok := channelMap[int(channel)]
            /*
            if !ok {
                return nil, fmt.Errorf("Invalid channel number %v in pattern %v row %v", channel, len(patterns), row)
            }
            */
            if ok {
                rows[channelMap[int(channel)]] = noteObject
            }
        }

        patterns = append(patterns, pattern)
    }

    mixVolume := int(masterVolume & 127)
    switch {
    case sampleFormat == 1 && masterVolume < 8:
        mixVolume = min(127, (int(masterVolume)+1)*16)
    case masterVolume == 2 || masterVolume == 18:
        mixVolume = 32
    case mixVolume == 0:
        mixVolume = 48
    default:
        mixVolume = max(16, mixVolume)
    }
    if mono {
        mixVolume = (mixVolume*8 + 5)/11
    }
    screamTrackerGUS := trackerVersion & 0xf000 == 0x1000 && usesGUS
    if screamTrackerGUS {
        mixVolume = 48 // ST3's GUS driver ignores the header's master volume.
    }
    if globalVolume == 0 && trackerVersion < 0x1320 {
        globalVolume = 64
    }
    return &S3MFile{
        Name: string(name),
        Instruments: instruments,
        Patterns: patterns,
        Orders: orders,
        ChannelMap: channelMap,
        ChannelPanning: channelPanning,
        SongLength: len(orders),
        InitialSpeed: initialSpeed,
        InitialTempo: initialTempo,
        GlobalVolume: min(64, globalVolume),
        MixVolume: uint8(mixVolume),
        FastVolumeSlides: trackerVersion == 0x1300 || flags & 0x40 != 0,
        OldVibrato: flags & 1 != 0,
        AmigaLimits: flags & 0x10 != 0,
        impulseTracker: trackerVersion & 0xf000 == 0x3000,
        separateEffectMemory: trackerVersion & 0xf000 == 0x3000 || trackerVersion & 0xf000 == 0x4000,
        screamTrackerGUS: screamTrackerGUS,
    }, nil
}
