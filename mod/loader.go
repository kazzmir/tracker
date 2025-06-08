package mod

import (
    "io"
    "bytes"
    "log"
)

type ModFile struct {
    Channels int
    Name string
}

// little endian 16-bit word
func readUint16(reader io.ReadSeeker) (uint16, error) {
    var buf [2]byte
    _, err := io.ReadFull(reader, buf[:])
    if err != nil {
        return 0, err
    }
    return (uint16(buf[0]) | (uint16(buf[1]) << 8)), nil
}

func readByte(reader io.ReadSeeker) (byte, error) {
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

    return &ModFile{
        Channels: channels,
        Name: string(name),
    }, nil
}
