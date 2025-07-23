package xm

import (
    "io"
    "bufio"
    "fmt"
    "log"
    "bytes"
    "encoding/binary"
    "errors"
)

type XMFile struct {
    Instruments []*Instrument
    Patterns []Pattern
}

type Pattern struct {
    Rows uint16
    PatternData []byte // packed data
}

type Instrument struct {
}

type Sample struct {
    Name string
    Length uint32
    LoopStart uint32
    LoopLength uint32
    Volume uint8
    FineTune int8
    Type uint8
    Panning uint8
    RelativeNoteNumber int8
    CompressionType uint8

    Data []float32
}

func Load(reader_ io.ReadSeeker) (*XMFile, error) {
    reader := bufio.NewReader(reader_)

    maximumFileSize, err := reader_.Seek(0, io.SeekEnd)
    if err != nil {
        return nil, fmt.Errorf("Error seeking to end of file: %v", err)
    }

    _, err = reader_.Seek(0, io.SeekStart)
    if err != nil {
        return nil, fmt.Errorf("Error seeking to start of file: %v", err)
    }

    idText := make([]byte, 17)
    _, err = io.ReadFull(reader, idText)
    if err != nil {
        return nil, err
    }

    id := string(idText)
    log.Printf("XM ID: '%s'", id)

    moduleName := make([]byte, 20)
    _, err = io.ReadFull(reader, moduleName)
    if err != nil {
        return nil, err
    }

    moduleName = bytes.TrimRight(moduleName, "\x00")
    log.Printf("Module Name: '%s'", moduleName)

    check, err := reader.ReadByte()
    if err != nil {
        return nil, err
    }

    if check != 0x1a {
        return nil, fmt.Errorf("Expected 0x1a but got 0x%x", check)
    }

    trackerName := make([]byte, 20)
    _, err = io.ReadFull(reader, trackerName)
    if err != nil {
        return nil, err
    }
    trackerName = bytes.TrimRight(trackerName, "\x00")
    log.Printf("Tracker Name: '%s'", trackerName)

    var versionNumber uint16
    err = binary.Read(reader, binary.LittleEndian, &versionNumber)

    if err != nil {
        return nil, fmt.Errorf("Error reading version number: %v", err)
    }

    log.Printf("Version Number: %d", versionNumber)

    var headerSize uint32
    err = binary.Read(reader, binary.LittleEndian, &headerSize)
    if err != nil {
        return nil, fmt.Errorf("Error reading header size: %v", err)
    }

    log.Printf("Header Size: %d", headerSize)

    if headerSize < 20 + 1 {
        return nil, fmt.Errorf("header size was invalid, must be at least 21 but was %d", headerSize)
    }

    var songLength uint16
    err = binary.Read(reader, binary.LittleEndian, &songLength)
    if err != nil {
        return nil, fmt.Errorf("Error reading song length: %v", err)
    }
    log.Printf("Song Length: %d", songLength)

    if songLength < 1 || songLength > 256 {
        return nil, fmt.Errorf("Song length is invalid, must be between 1 and 256, got %d", songLength)
    }

    var restartPosition uint16
    err = binary.Read(reader, binary.LittleEndian, &restartPosition)
    if err != nil {
        return nil, fmt.Errorf("Error reading restart position: %v", err)
    }
    log.Printf("Restart Position: %d", restartPosition)

    var channelCount uint16
    err = binary.Read(reader, binary.LittleEndian, &channelCount)
    if err != nil {
        return nil, fmt.Errorf("Error reading channel count: %v", err)
    }
    log.Printf("Channel Count: %d", channelCount)

    var patternCount uint16
    err = binary.Read(reader, binary.LittleEndian, &patternCount)
    if err != nil {
        return nil, fmt.Errorf("Error reading pattern count: %v", err)
    }
    log.Printf("Pattern Count: %d", patternCount)

    var instrumentCount uint16
    err = binary.Read(reader, binary.LittleEndian, &instrumentCount)
    if err != nil {
        return nil, fmt.Errorf("Error reading instrument count: %v", err)
    }
    log.Printf("Instrument Count: %d", instrumentCount)

    var flags uint16
    err = binary.Read(reader, binary.LittleEndian, &flags)
    if err != nil {
        return nil, fmt.Errorf("Error reading flags: %v", err)
    }
    log.Printf("Flags: %d", flags)

    var tempo uint16
    err = binary.Read(reader, binary.LittleEndian, &tempo)
    if err != nil {
        return nil, fmt.Errorf("Error reading tempo: %v", err)
    }
    log.Printf("Tempo: %d", tempo)

    var bpm uint16
    err = binary.Read(reader, binary.LittleEndian, &bpm)
    if err != nil {
        return nil, fmt.Errorf("Error reading BPM: %v", err)
    }

    log.Printf("BPM: %d", bpm)

    // number of bytes in the pattern order data
    orderLength := headerSize - 20

    limitReader := io.LimitReader(reader, int64(orderLength))

    var orderData []byte

    reader = bufio.NewReader(limitReader)
    for {
        pattern, err := reader.ReadByte()
        if err != nil {
            if err == io.EOF {
                break
            }
            return nil, fmt.Errorf("Error reading pattern order: %v", err)
        }
        orderData = append(orderData, pattern)
    }

    if uint16(len(orderData)) > songLength {
        orderData = orderData[:songLength]
    }

    log.Printf("Pattern Order Data: %v", orderData)

    // pattern data
    reader_.Seek(int64(headerSize + 60), io.SeekStart)

    reader = bufio.NewReader(reader_)

    var patterns []Pattern

    for i := range patternCount {
        pattern, err := readPattern(reader, maximumFileSize, int(i))
        if err != nil {
            return nil, fmt.Errorf("Error reading pattern %d: %v", i, err)
        }
        patterns = append(patterns, pattern)
    }

    var instruments []*Instrument

    for i := range instrumentCount {
        log.Printf("Reading instrument %d", i)
        var size uint32
        err = binary.Read(reader, binary.LittleEndian, &size)
        if err != nil {
            return nil, fmt.Errorf("Error reading instrument size: %v", err)
        }
        log.Printf("Instrument Size: %d", size)

        instrument, err := readInstrument(reader, size)
        if err != nil {
            return nil, fmt.Errorf("Error reading instrument %d: %v", i, err)
        }

        instruments = append(instruments, instrument)
    }

    return &XMFile{
        Instruments: instruments,
        Patterns: patterns,
    }, nil
}

func readPattern(reader *bufio.Reader, maximumFileSize int64, patternIndex int) (Pattern, error) {
    log.Printf("Reading pattern %d", patternIndex)

    var patternHeaderSize uint32
    err := binary.Read(reader, binary.LittleEndian, &patternHeaderSize)
    if err != nil {
        return Pattern{}, fmt.Errorf("Error reading pattern header size: %v", err)
    }
    // log.Printf("Pattern Header Size: %d", patternHeaderSize)

    if int64(patternHeaderSize) > maximumFileSize {
        return Pattern{}, fmt.Errorf("Pattern header size exceeds maximum file size: %d > %d", patternHeaderSize, maximumFileSize)
    }

    _, err = reader.Discard(1)
    if err != nil {
        return Pattern{}, fmt.Errorf("Error discarding byte: %v", err)
    }

    var rows uint16
    err = binary.Read(reader, binary.LittleEndian, &rows)
    if err != nil {
        return Pattern{}, fmt.Errorf("Error reading rows: %v", err)
    }

    log.Printf("Rows: %d", rows)

    if rows < 1 || rows > 256 {
        return Pattern{}, fmt.Errorf("Rows must be between 1 and 256, got %d", rows)
    }

    var packedSize uint16
    err = binary.Read(reader, binary.LittleEndian, &packedSize)
    if err != nil {
        return Pattern{}, fmt.Errorf("Error reading packed size: %v", err)
    }

    // log.Printf("Packed Size: %d", packedSize)
    if packedSize > 0 {
        var buffer bytes.Buffer
        // patternData := make([]byte, packedSize)
        // _, err = io.ReadFull(reader, patternData)
        _, err := io.CopyN(&buffer, reader, int64(packedSize))
        if err != nil {
            return Pattern{}, fmt.Errorf("Error reading pattern data: %v", err)
        }

        return Pattern{
            Rows: rows,
            PatternData: buffer.Bytes(),
        }, nil
    } else {
        log.Printf("Empty pattern..")
        return Pattern{}, nil
    }
}

func readInstrument(reader_ io.Reader, instrumentHeaderSize uint32) (*Instrument, error) {
    instrumentReader := bufio.NewReader(io.LimitReader(reader_, int64(instrumentHeaderSize - 4)))

    name := make([]byte, 22)
    _, err := io.ReadFull(instrumentReader, name)
    if err != nil {
        return nil, fmt.Errorf("Error reading instrument name: %v", err)
    }
    name = bytes.TrimRight(name, "\x00")
    log.Printf("Instrument Name: '%s'", name)

    _, err = instrumentReader.Discard(1)
    if err != nil {
        return nil, fmt.Errorf("Error reading instrument type: %v", err)
    }

    var samples uint16
    err = binary.Read(instrumentReader, binary.LittleEndian, &samples)
    if err != nil {
        return nil, fmt.Errorf("Error reading sample count: %v", err)
    }

    var sampleHeaderSizes []uint32

    log.Printf("Sample Count: %d", samples)
    for range samples {
        var sampleHeaderSize uint32
        err = binary.Read(instrumentReader, binary.LittleEndian, &sampleHeaderSize)
        if err != nil {
            return nil, fmt.Errorf("Error reading sample header size: %v", err)
        }
        log.Printf("Sample Header Size: %d", sampleHeaderSize)

        sampleHeaderSizes = append(sampleHeaderSizes, sampleHeaderSize)

        keymapAssignments := make([]byte, 96)
        _, err = io.ReadFull(instrumentReader, keymapAssignments)
        if err != nil {
            return nil, fmt.Errorf("Error reading keymap assignments: %v", err)
        }

        // log.Printf("Keymap Assignments: %d", keymapAssignments)

        volumeEnvelopePoints := make([]uint16, 24)
        for i := range volumeEnvelopePoints {
            err = binary.Read(instrumentReader, binary.LittleEndian, &volumeEnvelopePoints[i])
            if err != nil {
                return nil, fmt.Errorf("Error reading volume envelope points: %v", err)
            }
        }

        panningEnvelopePoints := make([]uint16, 24)
        for i := range panningEnvelopePoints {
            err = binary.Read(instrumentReader, binary.LittleEndian, &panningEnvelopePoints[i])
            if err != nil {
                return nil, fmt.Errorf("Error reading panning envelope points: %v", err)
            }
        }

        volumePoints, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading volume points: %v", err)
        }
        log.Printf("Volume Points: %d", volumePoints)

        panningPoints, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading panning points: %v", err)
        }
        log.Printf("Panning Points: %d", panningPoints)

        volumeSustainPoint, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading volume sustain point: %v", err)
        }
        log.Printf("Volume Sustain Point: %d", volumeSustainPoint)

        volumeLoopStart, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading volume loop start: %v", err)
        }

        log.Printf("Volume Loop Start: %d", volumeLoopStart)

        volumeLoopEnd, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading volume loop end: %v", err)
        }

        log.Printf("Volume Loop End: %d", volumeLoopEnd)

        panningSustainPoint, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading panning sustain point: %v", err)
        }

        log.Printf("Panning Sustain Point: %d", panningSustainPoint)

        panningLoopStart, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading panning loop start: %v", err)
        }

        log.Printf("Panning Loop Start: %d", panningLoopStart)

        panningLoopEnd, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading panning loop end: %v", err)
        }

        log.Printf("Panning Loop End: %d", panningLoopEnd)

        volumeType, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading volume type: %v", err)
        }

        log.Printf("Volume Type: %d", volumeType)

        panningType, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading panning type: %v", err)
        }

        log.Printf("Panning Type: %d", panningType)

        vibratoType, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading vibrato type: %v", err)
        }
        log.Printf("Vibrato Type: %d", vibratoType)

        vibratoSweep, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading vibrato sweep: %v", err)
        }
        log.Printf("Vibrato Sweep: %d", vibratoSweep)

        vibratoDepth, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading vibrato depth: %v", err)
        }

        log.Printf("Vibrato Depth: %d", vibratoDepth)

        vibratoRate, err := instrumentReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading vibrato rate: %v", err)
        }

        log.Printf("Vibrato Rate: %d", vibratoRate)

        var volumeFadeOut uint16
        err = binary.Read(instrumentReader, binary.LittleEndian, &volumeFadeOut)
        if err != nil {
            return nil, fmt.Errorf("Error reading volume: %v", err)
        }

        log.Printf("Volume: %d", volumeFadeOut)

        instrumentReader.Discard(22) // reserved 22 bytes
    }

    for {
        _, err = instrumentReader.Discard(1)
        if err != nil {
            break
        } else {
            log.Printf("Extra byte in instrument header")
        }
    }

    var sampleData []Sample

    for i := range samples {

        sampleReader := bufio.NewReader(io.LimitReader(reader_, int64(sampleHeaderSizes[i])))

        var sampleLength uint32
        err = binary.Read(sampleReader, binary.LittleEndian, &sampleLength)
        if err != nil {
            return nil, fmt.Errorf("Error reading sample length: %v", err)
        }

        log.Printf("Sample Length: %d", sampleLength)

        var loopStart uint32
        err = binary.Read(sampleReader, binary.LittleEndian, &loopStart)
        if err != nil {
            return nil, fmt.Errorf("Error reading loop start: %v", err)
        }

        log.Printf("Loop Start: %d", loopStart)

        var loopLength uint32
        err = binary.Read(sampleReader, binary.LittleEndian, &loopLength)
        if err != nil {
            return nil, fmt.Errorf("Error reading loop length: %v", err)
        }

        log.Printf("Loop Length: %d", loopLength)

        volume, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading sample volume: %v", err)
        }

        log.Printf("Sample Volume: %d", volume)

        fineTune, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading sample fine tune: %v", err)
        }

        log.Printf("Sample Fine Tune: %d", fineTune)

        sampleType, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading sample type: %v", err)
        }

        log.Printf("Sample Type: %d", sampleType)

        panning, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading sample panning: %v", err)
        }

        log.Printf("Sample Panning: %d", panning)

        relativeNoteNumber, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading relative note number: %v", err)
        }

        log.Printf("Relative Note Number: %d", relativeNoteNumber)

        compressionType, err := sampleReader.ReadByte()
        if err != nil {
            return nil, fmt.Errorf("Error reading compression type: %v", err)
        }

        log.Printf("Compression Type: %d", compressionType)

        sampleName := make([]byte, 22)
        _, err = io.ReadFull(sampleReader, sampleName)
        if err != nil {
            return nil, fmt.Errorf("Error reading sample name: %v", err)
        }

        sampleName = bytes.TrimRight(sampleName, "\x00")
        log.Printf("Sample Name: '%s'", sampleName)

        sampleData = append(sampleData, Sample{
            Name: string(sampleName),
            Length: sampleLength,
            LoopStart: loopStart,
            LoopLength: loopLength,
            Volume: volume,
            FineTune: int8(fineTune),
            Type: sampleType,
            Panning: panning,
            RelativeNoteNumber: int8(relativeNoteNumber),
            CompressionType: compressionType,
        })
    }

    _, err = instrumentReader.Discard(1)
    if !errors.Is(err, io.EOF) {
        return nil, fmt.Errorf("Error discarding byte after sample data: %v", err)
    }

    for i := range sampleData {
        sampleReader := bufio.NewReader(io.LimitReader(reader_, int64(sampleData[i].Length)))

        is8Bit := sampleData[i].Type & 0b1000 == 0
        if is8Bit {
            numSamples := sampleData[i].Length
            log.Printf("Reading 8-bit sample data for sample %d, samples %d", i, numSamples)

            var last int8 = 0
            for range numSamples {
                v, err := sampleReader.ReadByte()
                if err != nil {
                    return nil, fmt.Errorf("Error reading 8-bit sample data: %v", err)
                }

                last += int8(v)
                sampleData[i].Data = append(sampleData[i].Data, float32(last)/128.0)
            }

        } else {
            // 16-bit
            numSamples := sampleData[i].Length / 2
            log.Printf("Reading 16-bit sample data for sample %d samples %d", i, numSamples)

            var last int16 = 0
            for range numSamples {
                var v int16
                err = binary.Read(sampleReader, binary.LittleEndian, &v)
                if err != nil {
                    return nil, fmt.Errorf("Error reading 16-bit sample data: %v", err)
                }

                last += int16(v)
                sampleData[i].Data = append(sampleData[i].Data, float32(last)/32768.0)
            }

        }

    }

    return &Instrument{}, nil
}
