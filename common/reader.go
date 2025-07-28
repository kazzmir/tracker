package common

import (
    "io"
    "math"
)

type ReaderFunc struct {
    Func func(data []byte) (int, error)
}

func (reader *ReaderFunc) Read(data []byte) (int, error) {
    if reader.Func == nil {
        return 0, io.EOF
    }
    return reader.Func(data)
}

// returns the number of floats copied
func CopyFloat32(dst []byte, src []float32) int {
    maxBytes := min(len(dst), len(src) * 4)

    for i := range src {
        if i * 4 >= maxBytes {
            return i
        }

        bits := math.Float32bits(src[i])
        dst[i*4+0] = byte(bits)
        dst[i*4+1] = byte(bits >> 8)
        dst[i*4+2] = byte(bits >> 16)
        dst[i*4+3] = byte(bits >> 24)
    }

    return len(src)
}
