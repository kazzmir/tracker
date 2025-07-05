package common

import (
    "sync"
)

type AudioBuffer struct {
    // mono channel buffer of samples
    Buffer []float32
    lock sync.Mutex

    start int
    end int
    count int
}

func (buffer *AudioBuffer) Lock() {
    buffer.lock.Lock()
}

func (buffer *AudioBuffer) Unlock() {
    buffer.lock.Unlock()
}

func (buffer *AudioBuffer) Len() int {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()
    return buffer.count
}

func (buffer *AudioBuffer) Clear() {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    buffer.start = 0
    buffer.end = 0
    buffer.count = 0
}

func (buffer *AudioBuffer) Read(data []float32) int {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    total := 0

    if buffer.count == 0 {
        return total
    }

    // using copy() is much faster than a for loop, so we copy ranges of bytes out of the
    // ring buffer
    index := 0
    for buffer.count > 0 && index < len(data) {
        limit := buffer.count
        if buffer.start + buffer.count > len(buffer.Buffer) {
            limit = len(buffer.Buffer) - buffer.start
        }
        limit = min(limit, len(data[index:]))
        copy(data[index:], buffer.Buffer[buffer.start:buffer.start + limit])
        buffer.start = (buffer.start + limit) % len(buffer.Buffer)
        index += limit
        buffer.count -= limit
        total += limit
    }

    /*
    for i := range len(data) {
        if buffer.count == 0 {
            break
        }
        data[i] = buffer.Buffer[buffer.start]
        buffer.start = (buffer.start + 1) % len(buffer.Buffer)
        buffer.count -= 1
        total += 1
    }
    */

    return total
}

// copy data from the start of the buffer without removing it. returns the number of samples copied
func (buffer *AudioBuffer) Peek(data []float32) int {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    if buffer.count == 0 {
        return 0
    }

    // using copy() is much faster than a for loop, so we copy ranges of bytes out of the
    // ring buffer

    dataIndex := 0
    bufferStart := buffer.start
    bufferEnd := buffer.start + buffer.count

    if bufferEnd > len(buffer.Buffer) {
        bufferEnd = len(buffer.Buffer)
    }

    if bufferEnd - bufferStart > len(data) {
        bufferEnd = bufferStart + len(data)
    }

    copy(data[dataIndex:], buffer.Buffer[bufferStart:bufferEnd])

    copied := bufferEnd - bufferStart

    dataIndex += bufferEnd - bufferStart
    toCopy := buffer.count - (bufferEnd - bufferStart)

    if toCopy > 0 {
        // must wrap around
        bufferStart = 0
        bufferEnd = toCopy

        if bufferEnd > len(data) - dataIndex {
            bufferEnd = len(data) - dataIndex
        }

        copy(data[dataIndex:], buffer.Buffer[bufferStart:bufferStart + bufferEnd])

        copied += bufferEnd
    }

    return copied
}

func (buffer *AudioBuffer) UnsafeWrite(value float32) {
    if buffer.count < len(buffer.Buffer) {
        buffer.count += 1
        buffer.Buffer[buffer.end] = value
        buffer.end += 1
        if buffer.end >= len(buffer.Buffer) {
            buffer.end = 0
        }
        // buffer.end = (buffer.end + 1) % len(buffer.Buffer)
    } else {
        // log.Printf("overflow in audio buffer, dropping sample %v count is %v", value, buffer.count)

        buffer.Buffer[buffer.end] = value
        buffer.end += 1
        if buffer.end >= len(buffer.Buffer) {
            buffer.end = 0
        }
        buffer.start += 1
        if buffer.start >= len(buffer.Buffer) {
            buffer.start = 0
        }
    }
}

func (buffer *AudioBuffer) Write(data []float32, rate float32) {
    buffer.lock.Lock()
    defer buffer.lock.Unlock()

    var index float32
    for int(index) < len(data) {
        value := data[int(index)]
        index += rate
        if buffer.count >= len(buffer.Buffer) {
            break
        }

        buffer.count += 1
        buffer.Buffer[buffer.end] = value
        buffer.end = (buffer.end + 1) % len(buffer.Buffer)
    }
}

func MakeAudioBuffer(bufferSize int) *AudioBuffer {
    // log.Printf("Creating audio buffer with sample rate %d", bufferSize)
    return &AudioBuffer{
        Buffer: make([]float32, bufferSize),
    }
}

