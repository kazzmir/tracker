package lib

import (
    "os"
    "io"
    "encoding/binary"
    "log"
)

func SaveToWav(path string, reader io.Reader, sampleRate int) error {
    outputFile, err := os.Create(path)
    if err != nil {
        return err
    }
    defer outputFile.Close()

    dataLength := int64(0)
    bitsPerSample := 32
    bytePerBloc := 2 * bitsPerSample / 8
    bytePerSec := sampleRate * bytePerBloc // 2 channels, 32 bits per sample

    binary.Write(outputFile, binary.LittleEndian, []byte("RIFF"))
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength + 36))
    binary.Write(outputFile, binary.LittleEndian, []byte("WAVE"))
    binary.Write(outputFile, binary.LittleEndian, []byte("fmt "))
    binary.Write(outputFile, binary.LittleEndian, uint32(16))  // BlocSize
    binary.Write(outputFile, binary.LittleEndian, uint16(3))   // AudioFormat, IEEE float
    binary.Write(outputFile, binary.LittleEndian, uint16(2))
    binary.Write(outputFile, binary.LittleEndian, uint32(sampleRate))
    binary.Write(outputFile, binary.LittleEndian, uint32(bytePerSec))
    binary.Write(outputFile, binary.LittleEndian, uint16(bytePerBloc))
    binary.Write(outputFile, binary.LittleEndian, uint16(bitsPerSample))
    binary.Write(outputFile, binary.LittleEndian, []byte("data"))
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength))
    dataLength, err = io.Copy(outputFile, reader)

    // now that we know the data length, we can go back and write it in the header
    outputFile.Seek(4, io.SeekStart)
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength + 36))
    outputFile.Seek(40, io.SeekStart)
    binary.Write(outputFile, binary.LittleEndian, uint32(dataLength))

    log.Printf("Copied %v bytes to %v", dataLength, path)

    return err
}
