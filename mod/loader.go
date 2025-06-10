package mod

import (
    "io"
    "bytes"
    "log"
    "fmt"
)

type Effect byte
const (
    EffectNone Effect = 0xff
    EffectArpeggio = 0x0
    EffectPortamentoUp = 0x1
    EffectPortamentoDown = 0x2
    EffectTonePortamento = 0x3
    EffectVibrato = 0x4
    EffectPortamentoAndVolumeSlide = 0x5
    EffectVibratoAndVolumeSlide = 0x6
    EffectTremolo = 0x7
    EffectPan = 0x8
    EffectSampleOffset = 0x9
    EffectVolumeSlide = 0xa
    EffectPositionJump = 0xb
    EffectSetVolume = 0xc
    EffectPatternBreak = 0xd
    EffectExtra = 0xe
    EffectSetSpeed = 0xf
)

type ModFile struct {
    Channels int
    Name string
    Patterns []Pattern
    Orders []byte
    Samples []Sample
    // length in orders
    SongLength int
}

type Note struct {
    SampleNumber byte // 0-31
    PeriodFrequency uint16 // 0-65535
    EffectNumber byte // 0-15
    EffectParameter byte // 0-255
}

type Row struct {
    Notes []Note
}

type Pattern struct {
    // array of channels, each channel has 64 entries
    Rows []Row
}

type Sample struct {
    Name string
    Length uint16
    FineTune byte // -128 to 127
    Volume byte // 0-64
    LoopStart int
    LoopLength int
    // Data []int8 // the raw sample data
    Data []float32
}

// big endian 16-bit word
func readUint16(reader io.Reader) (uint16, error) {
    var buf [2]byte
    _, err := io.ReadFull(reader, buf[:])
    if err != nil {
        return 0, err
    }
    return (uint16(buf[1]) | (uint16(buf[0]) << 8)), nil
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

    var samples []Sample

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

        samples = append(samples, Sample{
            Name: string(sampleName),
            Length: sampleLength,
            FineTune: fineTune,
            Volume: volume,
            LoopStart: int(loopStart),
            LoopLength: int(loopLength),
        })
    }

    orderCount, err := readByte(reader)
    if err != nil {
        return nil, err
    }

    _, err = readByte(reader)
    if err != nil {
        return nil, err
    }

    patternMax := 0
    log.Printf("Reading %v orders", orderCount)
    orders := make([]byte, 128)
    _, err = io.ReadFull(reader, orders)
    if err != nil {
        return nil, fmt.Errorf("Could not read orders: %v", err)
    }

    for _, value := range orders {
        patternMax = max(patternMax, int(value))
    }

    log.Printf("Pattern max: %v", patternMax)

    // read mod kind again
    io.ReadFull(reader, kind)

    position, err := reader.Seek(0, io.SeekCurrent)
    log.Printf("Position before patterns: %v", position)

    // read patterns
    // a pattern consists of 64 rows where each row contains 'channels' number of notes
    // a note is a sample to play, combined with an effect and pitch
    patternBytes := make([]byte, 4)
    var patterns []Pattern
    for i := range patternMax + 1 {
        log.Printf("Reading pattern %d", i)

        var rows []Row
        for range 64 {
            var row Row
            for range channels {
                _, err = io.ReadFull(reader, patternBytes)
                if err != nil {
                    return nil, fmt.Errorf("Could not read pattern data: %v", err)
                }

                sampleNumber := (patternBytes[0] & 0xf0) + (patternBytes[2] >> 4)
                periodFrequency := (uint(patternBytes[0] & 0xf) << 8) + uint(patternBytes[1])
                effectNumber := patternBytes[2] & 0xf
                effectParameter := patternBytes[3]

                // log.Printf("Pattern data: Sample=%d, PeriodFrequency=%d, EffectNumber=%d, EffectParameter=%d", sampleNumber, periodFrequency, effectNumber, effectParameter)

                row.Notes = append(row.Notes, Note{
                    SampleNumber: sampleNumber,
                    PeriodFrequency: uint16(periodFrequency),
                    EffectNumber: effectNumber,
                    EffectParameter: effectParameter,
                })
            }

            rows = append(rows, row)
        }

        patterns = append(patterns, Pattern{
            Rows: rows,
        })
    }

    position, err = reader.Seek(0, io.SeekCurrent)
    log.Printf("Before sample data: %v", position)

    // read sample data
    for i := range 31 {
        if samples[i].Length == 0 {
            continue
        }

        log.Printf("Sample %v length %v", i, samples[i].Length * 2)
        data := make([]float32, 0, samples[i].Length * 2)

        for range samples[i].Length * 2 {
            value, err := readByte(reader)
            if err != nil {
                return nil, err
            }

            // convert to float, -1/+1 range
            floatValue := float32(int8(value)) / 128 * (float32(samples[i].Volume) / 64)

            data = append(data, floatValue)
        }

        samples[i].Data = data
    }

    position, err = reader.Seek(0, io.SeekCurrent)
    log.Printf("Position after sample data is now %v", position)

    end, err := reader.Seek(0, io.SeekEnd)
    log.Printf("End of file at %v", end)

    if position != end {
        log.Printf("  extra bytes!!")
    }

    rest, err := readByte(reader)
    if err == nil {
        return nil, fmt.Errorf("Extra data after samples: %v", rest)
    }

    // log.Printf("%v", samples[12].Data)
    // log.Printf("Data length %v samples %v", len(samples[5].Data), samples[5].Length)

    return &ModFile{
        SongLength: int(orderCount),
        Channels: channels,
        Patterns: patterns,
        Name: string(name),
        Orders: orders,
        Samples: samples,
    }, nil
}
