package xm

import (
    "io"
    "bufio"
    "fmt"
    "log"
    "bytes"
    "encoding/binary"
)

type XMFile struct {
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

    for i := range patternCount {
        log.Printf("Reading pattern %d", i)

        var patternHeaderSize uint32
        err = binary.Read(reader, binary.LittleEndian, &patternHeaderSize)
        if err != nil {
            return nil, fmt.Errorf("Error reading pattern header size: %v", err)
        }
        // log.Printf("Pattern Header Size: %d", patternHeaderSize)

        if int64(patternHeaderSize) > maximumFileSize {
            return nil, fmt.Errorf("Pattern header size exceeds maximum file size: %d > %d", patternHeaderSize, maximumFileSize)
        }

        _, err = reader.Discard(1)
        if err != nil {
            return nil, fmt.Errorf("Error discarding byte: %v", err)
        }

        var rows uint16
        err = binary.Read(reader, binary.LittleEndian, &rows)
        if err != nil {
            return nil, fmt.Errorf("Error reading rows: %v", err)
        }

        log.Printf("Rows: %d", rows)

        if rows < 1 || rows > 256 {
            return nil, fmt.Errorf("Rows must be between 1 and 256, got %d", rows)
        }

        var packedSize uint16
        err = binary.Read(reader, binary.LittleEndian, &packedSize)
        if err != nil {
            return nil, fmt.Errorf("Error reading packed size: %v", err)
        }

        // log.Printf("Packed Size: %d", packedSize)
        if packedSize > 0 {
            patternData := make([]byte, packedSize)
            _, err = io.ReadFull(reader, patternData)
            if err != nil {
                return nil, fmt.Errorf("Error reading pattern data: %v", err)
            }
        } else {
            log.Printf("Empty pattern..")
        }
    }

    for i := range instrumentCount {
        log.Printf("Reading instrument %d", i)
        var size uint32
        err = binary.Read(reader, binary.LittleEndian, &size)
        if err != nil {
            return nil, fmt.Errorf("Error reading instrument size: %v", err)
        }
        log.Printf("Instrument Size: %d", size)

        reader = bufio.NewReader(io.LimitReader(reader, int64(size)))

        name := make([]byte, 22)
        _, err = io.ReadFull(reader, name)
        if err != nil {
            return nil, fmt.Errorf("Error reading instrument name: %v", err)
        }
        name = bytes.TrimRight(name, "\x00")
        log.Printf("Instrument Name: '%s'", name)

        _, err = reader.Discard(1)
        if err != nil {
            return nil, fmt.Errorf("Error reading instrument type: %v", err)
        }

        var samples uint16
        err = binary.Read(reader, binary.LittleEndian, &samples)
        if err != nil {
            return nil, fmt.Errorf("Error reading sample count: %v", err)
        }

        log.Printf("Sample Count: %d", samples)
        if samples > 0 {
            var sampleHeaderSize uint32
            err = binary.Read(reader, binary.LittleEndian, &sampleHeaderSize)
            if err != nil {
                return nil, fmt.Errorf("Error reading sample header size: %v", err)
            }
            log.Printf("Sample Header Size: %d", sampleHeaderSize)

            keymapAssignments, err := reader.ReadByte()
            if err != nil {
                return nil, fmt.Errorf("Error reading keymap assignments: %v", err)
            }

            log.Printf("Keymap Assignments: %d", keymapAssignments)

            volumeEnvelopePoints := make([]uint16, 24)
            for i := range volumeEnvelopePoints {
                err = binary.Read(reader, binary.LittleEndian, &volumeEnvelopePoints[i])
                if err != nil {
                    return nil, fmt.Errorf("Error reading volume envelope points: %v", err)
                }
            }

            panningEnvelopePoints := make([]uint16, 24)
            for i := range panningEnvelopePoints {
                err = binary.Read(reader, binary.LittleEndian, &panningEnvelopePoints[i])
                if err != nil {
                    return nil, fmt.Errorf("Error reading panning envelope points: %v", err)
                }
            }
        }
    }

    return &XMFile{}, nil
}
