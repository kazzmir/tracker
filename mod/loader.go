package mod

import (
    "io"
    "bytes"
    "log"
    "fmt"
    "bufio"
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
    amigaTempo bool // ProTracker writes CIA tempo on the second tick.
}

type Note struct {
    SampleNumber byte // 0-31
    PeriodFrequency uint16 // 0-65535
    EffectNumber byte // 0-15
    EffectParameter byte // 0-255
}

func (note *Note) GetNotePosition() int {
    return noteValue(note.PeriodFrequency)
}

func (note *Note) GetEffectName() string {
    if note.EffectNumber > 0 || note.EffectParameter > 0 {
        return fmt.Sprintf("%X%02X", note.EffectNumber, note.EffectParameter)
    }

    return "..."
}

func (note *Note) GetName() string {
    if note.PeriodFrequency > 0 {
        return fmt.Sprintf("%v", ConvertToNote(note.PeriodFrequency))
    }
    return "..."
}

func (note *Note) GetSampleName() string {
    if note.SampleNumber > 0 {
        return fmt.Sprintf("%02d", note.SampleNumber)
    }
    return ".."
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
    FineTune byte // signed four-bit value in eighths of a semitone
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

var modNotePeriods = [...]uint16{
    3424, 3232, 3048, 2880, 2712, 2560, 2416, 2280, 2152, 2032, 1920, 1812,
    1712, 1616, 1524, 1440, 1356, 1280, 1208, 1140, 1076, 1016, 960, 907,
    856, 808, 762, 720, 678, 640, 604, 570, 538, 508, 480, 453,
    428, 404, 381, 360, 339, 320, 302, 285, 269, 254, 240, 226,
    214, 202, 190, 180, 170, 160, 151, 143, 135, 127, 120, 113,
    107, 101, 95, 90, 85, 80, 75, 71, 67, 63, 60, 56,
    53, 50, 47, 45, 42, 40, 37, 35, 33, 31, 30, 28,
}

// Finetune is a signed nibble used directly as the ProTracker table index.
var modTunedOctave = [16][12]int{
    {1712,1616,1524,1440,1356,1280,1208,1140,1076,1016,960,907},
    {1700,1604,1514,1430,1348,1274,1202,1134,1070,1010,954,900},
    {1688,1592,1504,1418,1340,1264,1194,1126,1064,1004,948,894},
    {1676,1582,1492,1408,1330,1256,1184,1118,1056,996,940,888},
    {1664,1570,1482,1398,1320,1246,1176,1110,1048,990,934,882},
    {1652,1558,1472,1388,1310,1238,1168,1102,1040,982,926,874},
    {1640,1548,1460,1378,1302,1228,1160,1094,1032,974,920,868},
    {1628,1536,1450,1368,1292,1220,1150,1086,1026,968,914,862},
    {1814,1712,1616,1524,1440,1356,1280,1208,1140,1076,1016,960},
    {1800,1700,1604,1514,1430,1350,1272,1202,1134,1070,1010,954},
    {1788,1688,1592,1504,1418,1340,1264,1194,1126,1064,1004,948},
    {1774,1676,1582,1492,1408,1330,1256,1184,1118,1056,996,940},
    {1762,1664,1570,1482,1398,1320,1246,1176,1110,1048,988,934},
    {1750,1652,1558,1472,1388,1310,1238,1168,1102,1040,982,926},
    {1736,1640,1548,1460,1378,1302,1228,1160,1094,1032,974,920},
    {1724,1628,1536,1450,1368,1292,1220,1150,1086,1026,968,914},
}

// tunedMODPeriod returns a period in quarter-period units.
func tunedMODPeriod(period uint16, fineTune byte) int {
    if fineTune == 0 { return int(period) * 4 }
    return modPeriodForNote(nearestMODNote(period), fineTune)
}

func modPeriodForNote(note int, fineTune byte) int {
    if fineTune == 0 && note < len(modNotePeriods) {
        return int(modNotePeriods[note]) * 4
    }
    return (modTunedOctave[fineTune&15][note%12] * 8) >> (note/12)
}

func nearestMODNote(period uint16) int {
    for index := 1; index < len(modNotePeriods); index++ {
        if int(period)*2 >= int(modNotePeriods[index-1])+int(modNotePeriods[index]) {
            return index-1
        }
    }
    return len(modNotePeriods)-1
}

func Load(reader_ io.Reader) (*ModFile, error) {
    var err error

    reader := bufio.NewReader(reader_)

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
    kind := make([]byte, 4)
    io.ReadFull(reader, kind)

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
    } else if bytes.Equal(kind, []byte{'1', '0', 'C', 'H'}) {
        channels = 10
        log.Printf("Detected 10 channel mod")
    } else {
        return nil, fmt.Errorf("Not a mod file: %v '%v'", kind, string(kind))
    }

    /*
    position, err := reader.Seek(0, io.SeekCurrent)
    log.Printf("Position before patterns: %v", position)
    */

    // read patterns
    // a pattern consists of 64 rows where each row contains 'channels' number of notes
    // a note is a sample to play, combined with an effect and pitch
    patternBytes := make([]byte, 4)
    var patterns []Pattern
    amigaTempo := string(kind) == "M.K."
    for _, sample := range samples {
        if sample.Length != 0 && sample.LoopLength == 0 {
            amigaTempo = false
        }
    }
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
                if periodFrequency != 0 {
                    // Pattern periods identify notes; some writers use slightly different tables.
                    periodFrequency = uint(modNotePeriods[nearestMODNote(uint16(periodFrequency))])
                    if periodFrequency < 113 || periodFrequency > 856 {
                        amigaTempo = false
                    }
                }
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

    /*
    position, err = reader.Seek(0, io.SeekCurrent)
    log.Printf("Before sample data: %v", position)
    */

    // read sample data
    for i := range 31 {
        if samples[i].Length == 0 {
            continue
        }

        log.Printf("Sample %v length %v", i, samples[i].Length * 2)
        data := make([]float32, 0, int(samples[i].Length) * 2)

        for range cap(data) {
            value, err := readByte(reader)
            if err != nil {
                return nil, err
            }

            // convert to float, -1/+1 range
            floatValue := float32(int8(value)) / 128

            data = append(data, floatValue)
        }

        samples[i].Data = data
    }

    // The declared pattern and sample sizes delimit the module. Ignore
    // trailing padding or tracker metadata rather than rejecting the file.
    return &ModFile{
        amigaTempo: amigaTempo,
        SongLength: int(orderCount),
        Channels: channels,
        Patterns: patterns,
        Name: string(name),
        Orders: orders,
        Samples: samples,
    }, nil
}
