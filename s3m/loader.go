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
)

type Instrument struct {
    Name string
    MiddleC uint16
    SampleFormat uint16
    Flags uint8
    Volume uint8
    Loop bool
    LoopBegin int
    LoopEnd int
    Data []float32
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

func (note *Note) GetName() string {
    return "..."
}

func (note *Note) GetEffectName() string {
    if note.EffectNumber > 0 || note.EffectParameter > 0 {
        return fmt.Sprintf("%X%02X", note.EffectNumber, note.EffectParameter)
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
    GlobalVolume uint8
}

func Load(reader_ io.ReadSeeker) (*S3MFile, error) {
    reader := bufio.NewReader(reader_)

    name := make([]byte, 28)
    _, err := io.ReadFull(reader, name)
    if err != nil {
        return nil, err
    }

    name = bytes.TrimRight(name, "\x00")

    log.Printf("Name: '%s'", string(name))

    // skip 0x1a byte
    _, err = reader.ReadByte()
    if err != nil {
        return nil, err
    }

    filetype, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    log.Printf("Filetype: %v", filetype)
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
    log.Printf("Song length: %v", songLength)


    var numInstruments uint16
    err = binary.Read(reader, binary.LittleEndian, &numInstruments)
    if err != nil {
        return nil, err
    }
    log.Printf("Instruments: %v", numInstruments)

    var patternsIgnore uint16
    err = binary.Read(reader, binary.LittleEndian, &patternsIgnore)
    if err != nil {
        return nil, err
    }
    log.Printf("Patterns: %v", patternsIgnore)

    var flags uint16
    err = binary.Read(reader, binary.LittleEndian, &flags)
    if err != nil {
        return nil, err
    }
    log.Printf("Flags: %v", flags)

    var trackerVersion uint16
    err = binary.Read(reader, binary.LittleEndian, &trackerVersion)
    if err != nil {
        return nil, err
    }
    log.Printf("Tracker version: 0x%x", trackerVersion)

    var sampleFormat uint16
    err = binary.Read(reader, binary.LittleEndian, &sampleFormat)
    if err != nil {
        return nil, err
    }
    log.Printf("Sample format: %v", sampleFormat)

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

    log.Printf("Global volume: %v", globalVolume)

    initialSpeed, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    log.Printf("Initial speed: %v", initialSpeed)

    initialTempo, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    log.Printf("Initial tempo: %v", initialTempo)

    masterVolume, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    log.Printf("Master volume: %v", masterVolume)

    // ultraclick removal, skip
    _, err = reader.ReadByte()
    if err != nil {
        return nil, err
    }

    defaultPanning, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }
    log.Printf("Default panning: %v", defaultPanning)

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
    log.Printf("Special: %v", special)

    var channelSettings [32]byte
    _, err = io.ReadFull(reader, channelSettings[:])
    if err != nil {
        return nil, err
    }

    channelMap := make(map[int]int)

    channelCount := 0
    for i, setting := range channelSettings {
        log.Printf("Channel %v setting: %v", i, setting < 16)
        if setting < 16 {
            channelMap[i] = channelCount

            channelCount += 1

            log.Printf("Channel %v default panning %v", i, setting)

            if setting <= 7 {
                // pan left
            } else {
                // pan right
            }
        }
    }

    log.Printf("Channels in use: %v", channelCount)

    numPatterns := 0
    var orders []byte
    for range songLength {
        order, err := reader.ReadByte()
        if err != nil {
            return nil, err
        }

        // log.Printf("Got pattern %v", order)

        // pattern marker, ignore
        if order == 0xfe {
            continue
        }
        // end of song marker
        if order == 0xff {
            // break
            continue
        }
        orders = append(orders, order)
        numPatterns = max(numPatterns, int(order))
    }

    // log.Printf("Song length before %v after %v", songLength, len(orders))

    songLength = uint16(len(orders))

    var instrumentOffsets []uint16
    for range numInstruments {
        var offset uint16
        err := binary.Read(reader, binary.LittleEndian, &offset)
        if err != nil {
            return nil, err
        }

        instrumentOffsets = append(instrumentOffsets, offset)
        log.Printf("Instrument %v offset 0x%x", len(instrumentOffsets)-1, offset << 4)
    }

    var patternOffsets []uint16
    for range numPatterns+1 {
        var offset uint16
        err := binary.Read(reader, binary.LittleEndian, &offset)
        if err != nil {
            return nil, err
        }

        patternOffsets = append(patternOffsets, offset)
        log.Printf("Pattern %v offset 0x%x", len(patternOffsets)-1, offset << 4)
    }

    if defaultPanning == 0xfc {
        var data [32]byte
        for range channelCount {
            _, err := io.ReadFull(reader, data[:])
            if err != nil {
                return nil, err
            }
        }
        for i := range data {
            // only use lower 4 bits
            data[i] = data[i] & 0xf
            log.Printf("Panning for channel %v: %v", i, data[i])
        }
        // FIXME: do something with panning data
    }

    mono := (masterVolume & 128 == 0) || (masterVolume >> 7 == 0)

    log.Printf("Mono: %v", mono)

    var instruments []Instrument

    for i, offset := range instrumentOffsets {
        _, err := reader_.Seek(int64(offset << 4), io.SeekStart)
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
        if type_ == 1 {
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

            var loopBegin uint16
            err = binary.Read(buffer, binary.LittleEndian, &loopBegin)
            if err != nil {
                return nil, err
            }

            // skip 2 bytes
            _, err = buffer.Discard(2)
            if err != nil {
                return nil, err
            }

            var loopEnd uint16
            err = binary.Read(buffer, binary.LittleEndian, &loopEnd)
            if err != nil {
                return nil, err
            }

            _, err = buffer.Discard(2)
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

            // ignore for now
            _ = packing

            flags, err := buffer.ReadByte()
            if err != nil {
                return nil, err
            }

            var middleC uint16
            err = binary.Read(buffer, binary.LittleEndian, &middleC)
            if err != nil {
                return nil, err
            }

            _, err = buffer.Discard(2)

            if err != nil {
                return nil, err
            }

            _, err = buffer.Discard(12)
            if err != nil {
                return nil, err
            }

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
            data := make([]byte, sampleLength)
            n, err := io.ReadFull(buffer, data)
            if err != nil {
                return nil, fmt.Errorf("Unable to read sample data: %v. Read %v", err, n)
            }

            var floatData []float32
            for _, value := range data {
                floatData = append(floatData, (float32(value) - 128)/128.0)
            }

            instruments = append(instruments, Instrument{
                Name: string(sampleNameTrim),
                MiddleC: middleC,
                Volume: sampleVolume,
                Flags: flags,
                Loop: flags & 1 != 0,
                LoopBegin: int(loopBegin),
                LoopEnd: int(loopEnd),
                Data: floatData,
            })

            log.Printf("Instrument %v loop begin %v end %v", i, loopBegin, loopEnd)
        } else {
            instruments = append(instruments, Instrument{
            })
        }

        // log.Printf("Instrument %v: type %v", i, type_)
    }

    var patterns []Pattern

    for _, offset := range patternOffsets {
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
                // set default volume
                hasVolume = true
                volume = 64
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
                ChangeSample: hasSample,
                Note: int(note),
                ChangeNote: hasNote,
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

    return &S3MFile{
        Name: string(name),
        Instruments: instruments,
        Patterns: patterns,
        Orders: orders,
        ChannelMap: channelMap,
        SongLength: len(orders),
        InitialSpeed: initialSpeed,
        InitialTempo: initialTempo,
        GlobalVolume: globalVolume,
    }, nil
}
