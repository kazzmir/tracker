package mod

import (
    "io"
    "bytes"
    "log"
)

type ModFile struct {
    Channels int
}

func Load(reader io.ReadSeeker) (*ModFile, error) {
    _, err := reader.Seek(0x438, io.SeekStart)
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

    return &ModFile{Channels: channels}, nil
}
