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

    idText := make([]byte, 17)
    _, err := io.ReadFull(reader, idText)
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

    var songLength uint16
    err = binary.Read(reader, binary.LittleEndian, &songLength)
    if err != nil {
        return nil, fmt.Errorf("Error reading song length: %v", err)
    }
    log.Printf("Song Length: %d", songLength)

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

    return &XMFile{}, nil
}
