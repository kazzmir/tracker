package mod

import (
    "io"
    "bytes"
    "log"
    "fmt"
)

type ModFile struct {
    Channels int
    Name string
    Patterns []Pattern
}

type PatternData struct {
    SampleNumber byte // 0-31
    PeriodFrequency uint16 // 0-65535
    EffectNumber byte // 0-15
    EffectParameter byte // 0-255
}

type Pattern struct {
    // array of channels, each channel has 64 entries
    Data [][]PatternData
}

// little endian 16-bit word
func readUint16(reader io.Reader) (uint16, error) {
    var buf [2]byte
    _, err := io.ReadFull(reader, buf[:])
    if err != nil {
        return 0, err
    }
    return (uint16(buf[0]) | (uint16(buf[1]) << 8)), nil
}

func readByte(reader io.Reader) (byte, error) {
    var buf [1]byte
    _, err := io.ReadFull(reader, buf[:])
    if err != nil {
        return 0, err
    }
    return buf[0], nil
}

func Load(reader io.ReadSeeker) (*ModFile, error) {
    var err error

    _, err = reader.Seek(0x438, io.SeekStart)
    if err != nil {
        return nil, err
    }

    kind := make([]byte, 4)
    _, err = io.ReadFull(reader, kind)
    if err != nil {
        return nil, err
    }

    channels := 4

    if bytes.Equal(kind, []byte{'M', '.', 'K', '.'}) {
        channels = 4
        log.Printf("Detected 4 channel mod")
    } else if bytes.Equal(kind, []byte{'6', 'C', 'H', 'N'}) {
        channels = 6
        log.Printf("Detected 6 channel mod")
    } else if bytes.Equal(kind, []byte{'8', 'C', 'H', 'N'}) {
        channels = 8
        log.Printf("Detected 8 channel mod")
    } else {
        return nil, fmt.Errorf("Not a mod file")
    }

    _, err = reader.Seek(0, io.SeekStart)
    if err != nil {
        return nil, err
    }

    name := make([]byte, 20)
    _, err = io.ReadFull(reader, name)
    if err != nil {
        return nil, err
    }

    name = bytes.TrimRight(name, "\x00")

    // read 31 samples
    for i := range 31 {
        sampleName := make([]byte, 22)
        _, err = io.ReadFull(reader, sampleName)
        if err != nil {
            return nil, err
        }
        sampleName = bytes.TrimRight(sampleName, "\x00")

        sampleLength, err := readUint16(reader)
        if err != nil {
            return nil, err
        }

        fineTune, err := readByte(reader)
        if err != nil {
            return nil, err
        }

        volume, err := readByte(reader)
        if err != nil {
            return nil, err
        }

        loopStart, err := readUint16(reader)
        if err != nil {
            return nil, err
        }

        loopLength, err := readUint16(reader)
        if err != nil {
            return nil, err
        }

        log.Printf("Sample %v: Name='%s', Length=%d, FineTune=%d, Volume=%d, LoopStart=%d, LoopLength=%d", i, string(sampleName), sampleLength, fineTune, volume, loopStart, loopLength)
    }

    orderCount, err := readByte(reader)
    if err != nil {
        return nil, err
    }

    _, err = readByte(reader)
    if err != nil {
        return nil, err
    }

    var orders [][]byte

    patternMax := 0
    log.Printf("Reading %v orders", orders)
    for i := range orderCount {
        orderBytes := make([]byte, 128)
        _, err := io.ReadFull(reader, orderBytes)
        if err != nil {
            return nil, fmt.Errorf("Could not read order %v: %v", i, err)
        }

        for value := range orderBytes {
            patternMax = max(patternMax, int(orderBytes[value]))
        }

        orders = append(orders, orderBytes)
    }

    log.Printf("Pattern max: %v", patternMax)

    // read patterns
    // a pattern consists of channels * 64 entries, where each channel has 64 entries
    // an entry is a sample to play, combined with an effect and pitch
    patternBytes := make([]byte, 4)
    var patterns []Pattern
    for i := range patternMax {
        log.Printf("Reading pattern %d", i)

        var patternData [][]PatternData
        for range channels {

            var data []PatternData
            for range 64 {
                _, err = io.ReadFull(reader, patternBytes)
                if err != nil {
                    return nil, fmt.Errorf("Could not read pattern data: %v", err)
                }

                sampleNumber := (patternBytes[0] & 0xf) + (patternBytes[2] >> 4)
                periodFrequency := (uint(patternBytes[0] & 0xf) << 8) + uint(patternBytes[1])
                effectNumber := patternBytes[2] & 0xf
                effectParameter := patternBytes[3]

                log.Printf("Pattern data: Sample=%d, PeriodFrequency=%d, EffectNumber=%d, EffectParameter=%d", sampleNumber, periodFrequency, effectNumber, effectParameter)

                data = append(data, PatternData{
                    SampleNumber: sampleNumber,
                    PeriodFrequency: uint16(periodFrequency),
                    EffectNumber: effectNumber,
                    EffectParameter: effectParameter,
                })
            }

            patternData = append(patternData, data)
        }

        patterns = append(patterns, Pattern{
            Data: patternData,
        })
    }

    return &ModFile{
        Channels: channels,
        Patterns: patterns,
        Name: string(name),
    }, nil
}
