package s3m

import (
    "bufio"
    "bytes"
    "fmt"
    "encoding/binary"
    "log"
    "io"
)

type Instrument struct {
    Name string
    MiddleC uint16
    SampleFormat uint16
    Flags uint8
    Volume uint8
    LoopBegin int
    LoopEnd int
    Data []byte
}

type S3MFile struct {
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

    var patterns uint16
    err = binary.Read(reader, binary.LittleEndian, &patterns)
    if err != nil {
        return nil, err
    }
    log.Printf("Patterns: %v", patterns)

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
    channelCount := 0
    for i, setting := range channelSettings {
        log.Printf("Channel %v setting: %v", i, setting < 16)
        if setting < 16 {
            channelCount += 1

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
        // pattern marker, ignore
        if order == 0xfe {
            continue
        }
        // end of song marker
        if order == 0xff {
            break
        }
        orders = append(orders, order)
        numPatterns = max(numPatterns, int(order))
    }

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
    for range numPatterns {
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

            _, err = reader_.Seek(int64(total), io.SeekCurrent)
            if err != nil {
                return nil, err
            }

            buffer = bufio.NewReader(reader_)
            data := make([]byte, sampleLength)
            _, err = io.ReadFull(buffer, data)
            if err != nil {
                return nil, err
            }

            instruments = append(instruments, Instrument{
                Name: string(sampleNameTrim),
                MiddleC: middleC,
                Volume: sampleVolume,
                Flags: flags,
                LoopBegin: int(loopBegin),
                LoopEnd: int(loopEnd),
                Data: data,
            })
        }

        log.Printf("Instrument %v: type %v", i, type_)
    }

    return nil, nil
}
