package impsynth

import (
	"bufio"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

var benchmarkVoiceChannels = []int{0, 1, 2}
var benchmarkEightVoiceChannels = []int{0, 1, 2, 3, 4, 5, 6, 7}
var benchmarkMaxVoiceChannels = []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17}

var nukedDumpOnce sync.Once
var nukedDumpPath string
var nukedDumpErr error
var nukedSeqDumpOnce sync.Once
var nukedSeqDumpPath string
var nukedSeqDumpErr error

const (
	exampleSongSampleRate = 44100
	exampleSongChunkSize  = 1024
	exampleSongReleaseMS  = 250
	wolfMusicTickRate     = 700
	doomMusicTickRate     = 140
)

type benchmarkNoteEvent struct {
	StartMS    int
	DurationMS int
	MIDINote   int
}

type benchmarkPatchFile struct {
	Name string         `json:"name"`
	Regs map[string]int `json:"regs"`
}

type synthReferenceCorpusCase struct {
	name string
	regs []uint16
}

var synthReferenceCorpusCases = []synthReferenceCorpusCase{
	{
		name: "melodic_fm",
		regs: []uint16{0x20, 0x21, 0x23, 0x01, 0x40, 0x08, 0x43, 0x00, 0x60, 0xF2, 0x63, 0xF2, 0x80, 0x24, 0x83, 0x24, 0xC0, 0x30, 0xA0, 0x98, 0xB0, 0x31},
	},
	{
		name: "bright_feedback",
		regs: []uint16{0x20, 0x21, 0x23, 0x21, 0x40, 0x04, 0x43, 0x00, 0x60, 0xF4, 0x63, 0xF4, 0x80, 0x22, 0x83, 0x22, 0xC0, 0x3C, 0xA0, 0xC0, 0xB0, 0x35},
	},
	{
		name: "trem_vib",
		regs: []uint16{0xBD, 0xC0, 0x20, 0xC1, 0x23, 0xC1, 0x40, 0x18, 0x43, 0x00, 0x60, 0xF3, 0x63, 0xF3, 0x80, 0x34, 0x83, 0x34, 0xC0, 0x30, 0xA0, 0x88, 0xB0, 0x33},
	},
}

func writeBenchmarkVoice(opl *Synth, ch int) {
	base := uint16(0)
	localCh := ch
	if ch >= 9 {
		base = 0x100
		localCh = ch - 9
	}

	modSlots := [9]uint16{0, 1, 2, 8, 9, 10, 16, 17, 18}
	carSlots := [9]uint16{3, 4, 5, 11, 12, 13, 19, 20, 21}
	mod := modSlots[localCh]
	car := carSlots[localCh]

	opl.WriteReg(base+0x20+mod, 0x01)
	opl.WriteReg(base+0x20+car, 0x01)
	opl.WriteReg(base+0x40+mod, 0x18)
	opl.WriteReg(base+0x40+car, 0x00)
	opl.WriteReg(base+0x60+mod, 0xF4)
	opl.WriteReg(base+0x60+car, 0xF6)
	opl.WriteReg(base+0x80+mod, 0x55)
	opl.WriteReg(base+0x80+car, 0x14)
	opl.WriteReg(base+0xC0+uint16(localCh), 0x30)
	opl.WriteReg(base+0xA0+uint16(localCh), 0x98)
	opl.WriteReg(base+0xB0+uint16(localCh), 0x31)
}

func benchmarkSynth(sampleRate int, channels []int) *Synth {
	opl := New(sampleRate)
	opl.WriteReg(0x01, 0x20)
	for _, ch := range channels {
		writeBenchmarkVoice(opl, ch)
	}
	return opl
}

func benchmarkReferenceCorpusSynth(sampleRate int, regs []uint16) *Synth {
	opl := New(sampleRate)
	opl.WriteReg(0x01, 0x20)
	for i := 0; i+1 < len(regs); i += 2 {
		opl.WriteReg(regs[i], uint8(regs[i+1]))
	}
	return opl
}

func mustLoadExampleSongBenchmarkData(b *testing.B) ([]benchmarkNoteEvent, *benchmarkPatchFile) {
	b.Helper()

	eventsData, err := os.ReadFile(filepath.Join("examples", "twinkle.csv"))
	if err != nil {
		b.Fatalf("read example song: %v", err)
	}
	patchData, err := os.ReadFile(filepath.Join("examples", "patches", "xylophone.json"))
	if err != nil {
		b.Fatalf("read example patch: %v", err)
	}

	events, err := parseExampleSongEvents(eventsData)
	if err != nil {
		b.Fatalf("parse example song: %v", err)
	}
	patch, err := parseExampleSongPatch(patchData)
	if err != nil {
		b.Fatalf("parse example patch: %v", err)
	}
	return events, patch
}

func parseExampleSongEvents(src []byte) ([]benchmarkNoteEvent, error) {
	lines := strings.Split(string(src), "\n")
	filtered := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		filtered = append(filtered, line)
	}

	r := csv.NewReader(strings.NewReader(strings.Join(filtered, "\n")))
	r.FieldsPerRecord = 3
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}

	events := make([]benchmarkNoteEvent, 0, len(records))
	for _, rec := range records {
		startMS, err := strconv.Atoi(strings.TrimSpace(rec[0]))
		if err != nil {
			return nil, err
		}
		durationMS, err := strconv.Atoi(strings.TrimSpace(rec[1]))
		if err != nil {
			return nil, err
		}
		midiNote, err := strconv.Atoi(strings.TrimSpace(rec[2]))
		if err != nil {
			return nil, err
		}
		if startMS < 0 || durationMS <= 0 || midiNote < 0 || midiNote > 127 {
			return nil, strconv.ErrSyntax
		}
		events = append(events, benchmarkNoteEvent{
			StartMS:    startMS,
			DurationMS: durationMS,
			MIDINote:   midiNote,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		if events[i].StartMS == events[j].StartMS {
			return events[i].MIDINote < events[j].MIDINote
		}
		return events[i].StartMS < events[j].StartMS
	})
	return events, nil
}

func parseExampleSongPatch(src []byte) (*benchmarkPatchFile, error) {
	var patch benchmarkPatchFile
	if err := json.Unmarshal(src, &patch); err != nil {
		return nil, err
	}
	if patch.Name == "" {
		patch.Name = "xylophone"
	}
	return &patch, nil
}

func configureExampleSongVoice(opl *Synth, patch *benchmarkPatchFile) {
	opl.Reset()
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0xC0, 0x30)

	regNames := make([]string, 0, len(patch.Regs))
	for reg := range patch.Regs {
		regNames = append(regNames, reg)
	}
	sort.Strings(regNames)
	for _, reg := range regNames {
		addr, err := strconv.ParseUint(strings.TrimPrefix(strings.ToLower(reg), "0x"), 16, 16)
		if err != nil {
			panic(err)
		}
		opl.WriteReg(uint16(addr), uint8(patch.Regs[reg]))
	}
}

func benchmarkRenderExampleSong(opl *Synth, events []benchmarkNoteEvent) {
	cursorMS := 0
	for _, event := range events {
		renderExampleSongSegment(opl, event.StartMS-cursorMS)
		keyOnExampleSong(opl, event.MIDINote)
		renderExampleSongSegment(opl, event.DurationMS)
		keyOffExampleSong(opl, event.MIDINote)
		cursorMS = event.StartMS + event.DurationMS
	}
	renderExampleSongSegment(opl, exampleSongReleaseMS)
}

func benchmarkRenderExampleSongFastSilenceFill(opl *Synth, events []benchmarkNoteEvent) {
	cursorMS := 0
	for _, event := range events {
		renderExampleSongSegment(opl, event.StartMS-cursorMS)
		keyOnExampleSong(opl, event.MIDINote)
		renderExampleSongSegment(opl, event.DurationMS)
		keyOffExampleSong(opl, event.MIDINote)
		cursorMS = event.StartMS + event.DurationMS
	}
	renderExampleSongReleaseTailFastSilence(opl, exampleSongReleaseMS)
}

func renderExampleSongSegment(opl *Synth, durationMS int) {
	framesRemaining := int(math.Round(float64(durationMS) * exampleSongSampleRate / 1000.0))
	for framesRemaining > 0 {
		frames := framesRemaining
		if frames > exampleSongChunkSize {
			frames = exampleSongChunkSize
		}
		_ = opl.GenerateStereoS16(frames)
		framesRemaining -= frames
	}
}

func renderExampleSongReleaseTailFastSilence(opl *Synth, durationMS int) {
	framesRemaining := int(math.Round(float64(durationMS) * exampleSongSampleRate / 1000.0))
	for framesRemaining > 0 {
		frames := framesRemaining
		if frames > exampleSongChunkSize {
			frames = exampleSongChunkSize
		}
		if benchmarkSynthOutputSilent(opl) {
			_ = benchmarkFillStereoSilence(opl, frames)
		} else {
			_ = opl.GenerateStereoS16(frames)
		}
		framesRemaining -= frames
	}
}

func benchmarkSynthOutputSilent(opl *Synth) bool {
	return opl.activeMask == 0 &&
		(!opl.resamplePrimed ||
			(opl.resamplePrevL == 0 &&
				opl.resamplePrevR == 0 &&
				opl.resampleNextL == 0 &&
				opl.resampleNextR == 0))
}

func benchmarkFillStereoSilence(opl *Synth, frames int) []int16 {
	if frames <= 0 {
		return nil
	}
	need := frames * 2
	if cap(opl.stereoBuf) < need {
		opl.stereoBuf = make([]int16, need)
	} else {
		opl.stereoBuf = opl.stereoBuf[:need]
	}
	clear(opl.stereoBuf)
	return opl.stereoBuf
}

func keyOnExampleSong(opl *Synth, midiNote int) {
	a0, b0 := oplNoteRegsForBenchmark(midiNote)
	opl.WriteReg(0xA0, a0)
	opl.WriteReg(0xB0, b0|0x20)
}

func keyOffExampleSong(opl *Synth, midiNote int) {
	a0, b0 := oplNoteRegsForBenchmark(midiNote)
	opl.WriteReg(0xA0, a0)
	opl.WriteReg(0xB0, b0)
}

func oplNoteRegsForBenchmark(midiNote int) (uint8, uint8) {
	block, fnum := midiToOPLForBenchmark(midiNote)
	return uint8(fnum & 0xFF), uint8(block<<2) | uint8((fnum>>8)&0x03)
}

func midiToOPLForBenchmark(midiNote int) (uint8, uint16) {
	freqHz := 440.0 * math.Pow(2, float64(midiNote-69)/12.0)

	bestBlock := uint8(0)
	bestFNum := uint16(0)
	bestErr := math.MaxFloat64
	for block := uint8(0); block <= 7; block++ {
		scale := math.Pow(2, float64(int(block)-1))
		fnum := int(math.Round(freqHz * 524288.0 / (49716.0 * scale)))
		if fnum < 0 || fnum > 1023 {
			continue
		}
		actualHz := float64(fnum) * 49716.0 * scale / 524288.0
		err := math.Abs(actualHz - freqHz)
		if err < bestErr {
			bestErr = err
			bestBlock = block
			bestFNum = uint16(fnum)
		}
	}
	return bestBlock, bestFNum
}

func TestGenerateStereoS16ProducesPCM(t *testing.T) {
	opl := New(49716)
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0x20, 0x01)
	opl.WriteReg(0x23, 0x01)
	opl.WriteReg(0x60, 0xF3)
	opl.WriteReg(0x63, 0xF3)
	opl.WriteReg(0x80, 0x24)
	opl.WriteReg(0x83, 0x24)
	opl.WriteReg(0xA0, 0x98)
	opl.WriteReg(0xB0, 0x31)
	opl.WriteReg(0x43, 0x00)
	opl.WriteReg(0xC0, 0x30)
	pcm := opl.GenerateStereoS16(256)
	if len(pcm) != 512 {
		t.Fatalf("samples=%d want=512", len(pcm))
	}
	nonZero := false
	for _, s := range pcm {
		if s != 0 {
			nonZero = true
			break
		}
	}
	if !nonZero {
		t.Fatal("expected audible PCM")
	}
}

func TestGenerateStereoS16ReusesBuffer(t *testing.T) {
	opl := New(49716)
	opl.WriteReg(0x20, 0x01)
	opl.WriteReg(0x23, 0x01)
	opl.WriteReg(0xA0, 0x98)
	opl.WriteReg(0xB0, 0x31)
	opl.WriteReg(0x43, 0x00)
	_ = opl.GenerateStereoS16(256)
	allocs := testing.AllocsPerRun(100, func() {
		_ = opl.GenerateStereoS16(256)
	})
	if allocs != 0 {
		t.Fatalf("GenerateStereoS16 allocs=%v want 0", allocs)
	}
}

func TestRhythmModeHighHatProducesPCMWithoutChannelKeyOn(t *testing.T) {
	opl := New(49716)
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0x31, 0x01)
	opl.WriteReg(0x51, 0x00)
	opl.WriteReg(0x71, 0xF4)
	opl.WriteReg(0x91, 0x14)
	opl.WriteReg(0xC7, 0x30)
	opl.WriteReg(0xA7, 0x98)
	opl.WriteReg(0xB7, 0x11)
	opl.WriteReg(0xBD, 0x21)

	if opl.ch[7].ops[0].keyMask&oplKeyMaskDrum == 0 {
		t.Fatal("expected high-hat drum key to be active")
	}

	pcm := opl.GenerateStereoS16(256)
	if !pcmHasSignal(pcm) {
		t.Fatal("expected rhythm-mode high-hat output")
	}
}

func TestRhythmModeOffClearsDrumKeys(t *testing.T) {
	opl := New(49716)
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0x30, 0x01)
	opl.WriteReg(0x33, 0x01)
	opl.WriteReg(0x50, 0x00)
	opl.WriteReg(0x53, 0x00)
	opl.WriteReg(0x70, 0xF4)
	opl.WriteReg(0x73, 0xF4)
	opl.WriteReg(0x90, 0x14)
	opl.WriteReg(0x93, 0x14)
	opl.WriteReg(0xC6, 0x30)
	opl.WriteReg(0xA6, 0x98)
	opl.WriteReg(0xB6, 0x11)
	opl.WriteReg(0xBD, 0x30)

	if opl.ch[6].ops[0].keyMask&oplKeyMaskDrum == 0 || opl.ch[6].ops[1].keyMask&oplKeyMaskDrum == 0 {
		t.Fatal("expected bass drum key to be active")
	}

	opl.WriteReg(0xBD, 0x00)
	if opl.ch[6].ops[0].keyMask != 0 || opl.ch[6].ops[1].keyMask != 0 {
		t.Fatal("expected bass drum keys to clear when rhythm mode is disabled")
	}
}

func TestNewOPL2IgnoresBank1Writes(t *testing.T) {
	opl := NewOPL2(49716)
	opl.WriteReg(0x120, 0x7F)
	if opl.regs[0x120] != 0 {
		t.Fatal("expected OPL2 mode to ignore bank 1 writes")
	}
}

func TestNewOPL2ForcesDualMonoOutput(t *testing.T) {
	opl := NewOPL2(49716)
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0x20, 0x01)
	opl.WriteReg(0x23, 0x01)
	opl.WriteReg(0x43, 0x00)
	opl.WriteReg(0x60, 0xF3)
	opl.WriteReg(0x63, 0xF3)
	opl.WriteReg(0x80, 0x24)
	opl.WriteReg(0x83, 0x24)
	opl.WriteReg(0xC0, 0x10)
	opl.WriteReg(0xA0, 0x98)
	opl.WriteReg(0xB0, 0x31)

	pcm := opl.GenerateStereoS16(64)
	for i := 0; i+1 < len(pcm); i += 2 {
		if pcm[i] != pcm[i+1] {
			t.Fatalf("expected dual-mono output in OPL2 mode at frame %d: %d != %d", i/2, pcm[i], pcm[i+1])
		}
	}
}

func TestNewOPL2WaveformSelectOffForcesWave0(t *testing.T) {
	opl := NewOPL2(49716)
	opl.WriteReg(0xE0, 0x03)
	if got := opl.ch[0].ops[0].regWave; got != 0 {
		t.Fatalf("waveform with select off = %d want 0", got)
	}
	opl.WriteReg(0x01, 0x20)
	opl.WriteReg(0xE0, 0x07)
	if got := opl.ch[0].ops[0].regWave; got != 0x03 {
		t.Fatalf("waveform with select on = %d want 3", got)
	}
}

func pcmHasSignal(pcm []int16) bool {
	for _, s := range pcm {
		if s != 0 {
			return true
		}
	}
	return false
}

func buildNukedDumpTool(t *testing.T) string {
	t.Helper()
	nukedDumpOnce.Do(func() {
		out := filepath.Join(os.TempDir(), "nuked-opl3-dump-test")
		cmd := exec.Command(
			"gcc",
			"-O2",
			"-I", "third_party/nuked-opl3",
			"-o", out,
			"bench/nuked_opl3_dump.c",
			"third_party/nuked-opl3/opl3.c",
			"-lm",
		)
		cmd.Dir = "."
		if output, err := cmd.CombinedOutput(); err != nil {
			nukedDumpErr = fmt.Errorf("gcc failed: %w: %s", err, strings.TrimSpace(string(output)))
		} else {
			nukedDumpPath = out
		}
	})
	if nukedDumpErr != nil {
		t.Fatalf("build nuked dump tool: %v", nukedDumpErr)
	}
	return nukedDumpPath
}

func renderNukedPCM(t *testing.T, sampleRate int, frames int, regs []uint16) []int16 {
	t.Helper()
	tool := buildNukedDumpTool(t)
	args := []string{strconv.Itoa(sampleRate), strconv.Itoa(frames)}
	for i := 0; i+1 < len(regs); i += 2 {
		args = append(args, strconv.FormatUint(uint64(regs[i]), 10), strconv.FormatUint(uint64(regs[i+1]), 10))
	}
	cmd := exec.Command(tool, args...)
	cmd.Dir = "."
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run nuked dump tool: %v", err)
	}

	pcm := make([]int16, 0, frames*2)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			t.Fatalf("unexpected nuked output line: %q", scanner.Text())
		}
		left, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("parse left sample: %v", err)
		}
		right, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("parse right sample: %v", err)
		}
		pcm = append(pcm, int16(left), int16(right))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan nuked output: %v", err)
	}
	if len(pcm) != frames*2 {
		t.Fatalf("nuked pcm len=%d want=%d", len(pcm), frames*2)
	}
	return pcm
}

func buildNukedSeqDumpTool(t *testing.T) string {
	t.Helper()
	nukedSeqDumpOnce.Do(func() {
		out := filepath.Join(os.TempDir(), "nuked-opl3-seq-dump-test")
		cmd := exec.Command(
			"gcc",
			"-O2",
			"-I", "third_party/nuked-opl3",
			"-o", out,
			"bench/nuked_opl3_seq_dump.c",
			"third_party/nuked-opl3/opl3.c",
			"-lm",
		)
		cmd.Dir = "."
		if output, err := cmd.CombinedOutput(); err != nil {
			nukedSeqDumpErr = fmt.Errorf("gcc failed: %w: %s", err, strings.TrimSpace(string(output)))
		} else {
			nukedSeqDumpPath = out
		}
	})
	if nukedSeqDumpErr != nil {
		t.Fatalf("build nuked seq dump tool: %v", nukedSeqDumpErr)
	}
	return nukedSeqDumpPath
}

func renderNukedSeqPCMAtTickRate(t *testing.T, sampleRate int, tickRate int, frames int, seqPath string) []int16 {
	t.Helper()
	tool := buildNukedSeqDumpTool(t)
	cmd := exec.Command(tool, strconv.Itoa(sampleRate), strconv.Itoa(tickRate), strconv.Itoa(frames), seqPath)
	cmd.Dir = "."
	output, err := cmd.Output()
	if err != nil {
		t.Fatalf("run nuked seq dump tool: %v", err)
	}

	pcm := make([]int16, 0, frames*2)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			t.Fatalf("unexpected nuked seq output line: %q", scanner.Text())
		}
		left, err := strconv.Atoi(fields[0])
		if err != nil {
			t.Fatalf("parse left seq sample: %v", err)
		}
		right, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("parse right seq sample: %v", err)
		}
		pcm = append(pcm, int16(left), int16(right))
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan nuked seq output: %v", err)
	}
	if len(pcm) != frames*2 {
		t.Fatalf("nuked seq pcm len=%d want=%d", len(pcm), frames*2)
	}
	return pcm
}

func renderNukedSeqPCM(t *testing.T, sampleRate int, frames int, seqPath string) []int16 {
	t.Helper()
	return renderNukedSeqPCMAtTickRate(t, sampleRate, wolfMusicTickRate, frames, seqPath)
}

func renderImpSynthOPL2(sampleRate int, frames int, regs []uint16) []int16 {
	opl := NewOPL2(sampleRate)
	for i := 0; i+1 < len(regs); i += 2 {
		opl.WriteReg(regs[i], uint8(regs[i+1]))
	}
	out := opl.GenerateStereoS16(frames)
	pcm := make([]int16, len(out))
	copy(pcm, out)
	return pcm
}

func renderImpSynthSeqAtTickRate(t *testing.T, sampleRate int, tickRate int, frames int, seqPath string, newSynth func(int) *Synth) []int16 {
	t.Helper()
	data, err := os.ReadFile(seqPath)
	if err != nil {
		t.Fatalf("read seq fixture: %v", err)
	}
	if len(data)%5 != 0 {
		t.Fatalf("seq fixture size=%d want multiple of 5", len(data))
	}
	type seqEvent struct {
		reg   uint16
		value uint8
		delay uint16
	}
	events := make([]seqEvent, 0, len(data)/5)
	for i := 0; i < len(data); i += 5 {
		events = append(events, seqEvent{
			reg:   binary.LittleEndian.Uint16(data[i : i+2]),
			value: data[i+2],
			delay: binary.LittleEndian.Uint16(data[i+3 : i+5]),
		})
	}
	opl := newSynth(sampleRate)
	pcm := make([]int16, 0, frames*2)
	eventIndex := 0
	framesUntilNext := 0
	tickFrames := sampleRate / tickRate
	if tickFrames < 1 {
		tickFrames = 1
	}
	for remaining := frames; remaining > 0; {
		for framesUntilNext <= 0 {
			ev := events[eventIndex]
			eventIndex++
			if eventIndex >= len(events) {
				eventIndex = 0
			}
			opl.WriteReg(ev.reg, ev.value)
			framesUntilNext = int(ev.delay) * tickFrames
			if framesUntilNext > 0 {
				break
			}
		}
		chunk := remaining
		if framesUntilNext > 0 && chunk > framesUntilNext {
			chunk = framesUntilNext
		}
		if chunk <= 0 {
			chunk = 1
		}
		out := opl.GenerateStereoS16(chunk)
		pcm = append(pcm, out...)
		remaining -= chunk
		framesUntilNext -= chunk
	}
	return pcm
}

func renderImpSynthOPL2Seq(t *testing.T, sampleRate int, frames int, seqPath string) []int16 {
	t.Helper()
	return renderImpSynthSeqAtTickRate(t, sampleRate, wolfMusicTickRate, frames, seqPath, NewOPL2)
}

func renderImpSynthSeq(t *testing.T, sampleRate int, tickRate int, frames int, seqPath string) []int16 {
	t.Helper()
	return renderImpSynthSeqAtTickRate(t, sampleRate, tickRate, frames, seqPath, New)
}

func filterSeqForChannel(t *testing.T, src string, ch int) string {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read seq fixture: %v", err)
	}
	slotToChannel := [32]int{
		0, 1, 2, 0, 1, 2, -1, -1,
		3, 4, 5, 3, 4, 5, -1, -1,
		6, 7, 8, 6, 7, 8, -1, -1,
		-1, -1, -1, -1, -1, -1, -1, -1,
	}
	keepReg := func(reg uint16) bool {
		r := int(reg)
		switch {
		case r == 0x01 || r == 0x08 || r == 0xBD:
			return true
		case r >= 0xA0 && r <= 0xA8:
			return r-0xA0 == ch
		case r >= 0xB0 && r <= 0xB8:
			return r-0xB0 == ch
		case r >= 0xC0 && r <= 0xC8:
			return r-0xC0 == ch
		case r >= 0x20 && r <= 0x35:
			slot := r - 0x20
			return slot >= 0 && slot < len(slotToChannel) && slotToChannel[slot] == ch
		case r >= 0x40 && r <= 0x55:
			slot := r - 0x40
			return slot >= 0 && slot < len(slotToChannel) && slotToChannel[slot] == ch
		case r >= 0x60 && r <= 0x75:
			slot := r - 0x60
			return slot >= 0 && slot < len(slotToChannel) && slotToChannel[slot] == ch
		case r >= 0x80 && r <= 0x95:
			slot := r - 0x80
			return slot >= 0 && slot < len(slotToChannel) && slotToChannel[slot] == ch
		case r >= 0xE0 && r <= 0xF5:
			slot := r - 0xE0
			return slot >= 0 && slot < len(slotToChannel) && slotToChannel[slot] == ch
		default:
			return false
		}
	}

	type seqEvent struct {
		reg   uint16
		value uint8
		delay uint16
	}
	events := make([]seqEvent, 0, len(data)/5)
	for i := 0; i+4 < len(data); i += 5 {
		events = append(events, seqEvent{
			reg:   binary.LittleEndian.Uint16(data[i : i+2]),
			value: data[i+2],
			delay: binary.LittleEndian.Uint16(data[i+3 : i+5]),
		})
	}
	filtered := make([]seqEvent, 0, len(events))
	carried := uint32(0)
	for _, ev := range events {
		if keepReg(ev.reg) {
			if len(filtered) > 0 {
				filtered[len(filtered)-1].delay = uint16(carried)
			}
			filtered = append(filtered, ev)
			carried = uint32(ev.delay)
		} else {
			carried += uint32(ev.delay)
		}
	}
	if len(filtered) > 0 {
		filtered[len(filtered)-1].delay = uint16(carried)
	}

	tmp, err := os.CreateTemp("", "impsynth-ch-*.seq")
	if err != nil {
		t.Fatalf("create temp seq: %v", err)
	}
	buf := make([]byte, 0, len(filtered)*5)
	for _, ev := range filtered {
		var rec [5]byte
		binary.LittleEndian.PutUint16(rec[:2], ev.reg)
		rec[2] = ev.value
		binary.LittleEndian.PutUint16(rec[3:], ev.delay)
		buf = append(buf, rec[:]...)
	}
	if _, err := tmp.Write(buf); err != nil {
		_ = tmp.Close()
		t.Fatalf("write temp seq: %v", err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatalf("close temp seq: %v", err)
	}
	return tmp.Name()
}

func sliceStereoFrames(pcm []int16, frameOffset int, frameCount int) []int16 {
	if frameOffset < 0 {
		frameOffset = 0
	}
	if frameCount < 0 {
		frameCount = 0
	}
	start := frameOffset * 2
	if start > len(pcm) {
		start = len(pcm)
	}
	end := start + frameCount*2
	if end > len(pcm) {
		end = len(pcm)
	}
	out := make([]int16, end-start)
	copy(out, pcm[start:end])
	return out
}

func maxPCMDelta(a []int16, b []int16) int {
	if len(a) != len(b) {
		return int(^uint(0) >> 1)
	}
	max := 0
	for i := range a {
		d := int(a[i]) - int(b[i])
		if d < 0 {
			d = -d
		}
		if d > max {
			max = d
		}
	}
	return max
}

func monoAbsEnergy(pcm []int16) int64 {
	var total int64
	for i := 0; i+1 < len(pcm); i += 2 {
		s := int64(pcm[i]+pcm[i+1]) / 2
		if s < 0 {
			s = -s
		}
		total += s
	}
	return total
}

func monoFrames(pcm []int16) []float64 {
	out := make([]float64, 0, len(pcm)/2)
	for i := 0; i+1 < len(pcm); i += 2 {
		out = append(out, float64(int(pcm[i])+int(pcm[i+1]))/2.0)
	}
	return out
}

func normalizedMagnitudeSpectrum(pcm []int16, fftSize int) []float64 {
	frames := monoFrames(pcm)
	if fftSize <= 0 {
		fftSize = 256
	}
	if len(frames) < fftSize {
		fftSize = len(frames)
	}
	if fftSize < 8 {
		return nil
	}

	windowed := make([]float64, fftSize)
	for i := 0; i < fftSize; i++ {
		w := 0.5 - 0.5*math.Cos((2*math.Pi*float64(i))/float64(fftSize-1))
		windowed[i] = frames[i] * w
	}

	bins := fftSize/2 + 1
	spec := make([]float64, bins)
	var sum float64
	for k := 0; k < bins; k++ {
		var re, im float64
		for n := 0; n < fftSize; n++ {
			phase := -2 * math.Pi * float64(k*n) / float64(fftSize)
			re += windowed[n] * math.Cos(phase)
			im += windowed[n] * math.Sin(phase)
		}
		mag := math.Hypot(re, im)
		spec[k] = mag
		sum += mag
	}
	if sum > 0 {
		for i := range spec {
			spec[i] /= sum
		}
	}
	return spec
}

func spectrumCosineSimilarity(a []int16, b []int16, fftSize int) float64 {
	sa := normalizedMagnitudeSpectrum(a, fftSize)
	sb := normalizedMagnitudeSpectrum(b, fftSize)
	if len(sa) == 0 || len(sb) == 0 || len(sa) != len(sb) {
		return 0
	}
	var dot, na, nb float64
	for i := range sa {
		dot += sa[i] * sb[i]
		na += sa[i] * sa[i]
		nb += sb[i] * sb[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / math.Sqrt(na*nb)
}

func TestNewOPL2MelodicOutputTracksNuked(t *testing.T) {
	cases := []struct {
		name     string
		regs     []uint16
		maxDelta int
	}{
		{
			name:     "basic_fm",
			maxDelta: 2048,
			regs: []uint16{
				0x01, 0x20,
				0x20, 0x01, 0x23, 0x01,
				0x40, 0x08, 0x43, 0x00,
				0x60, 0xF2, 0x63, 0xF2,
				0x80, 0x24, 0x83, 0x24,
				0xC0, 0x31,
				0xA0, 0x98, 0xB0, 0x31,
			},
		},
		{
			name:     "additive_waveform",
			maxDelta: 4096,
			regs: []uint16{
				0x01, 0x20,
				0x20, 0x41, 0x23, 0x11,
				0x40, 0x12, 0x43, 0x04,
				0x60, 0xE4, 0x63, 0xF2,
				0x80, 0x34, 0x83, 0x15,
				0xE0, 0x01, 0xE3, 0x03,
				0xC0, 0x01,
				0xA0, 0xB0, 0xB0, 0x32,
			},
		},
		{
			name:     "note_select_feedback",
			maxDelta: 8192,
			regs: []uint16{
				0x08, 0x40,
				0x20, 0x21, 0x23, 0x01,
				0x40, 0x04, 0x43, 0x00,
				0x60, 0xF3, 0x63, 0xF2,
				0x80, 0x28, 0x83, 0x14,
				0xC0, 0x0E,
				0xA0, 0x88, 0xB0, 0x35,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderImpSynthOPL2(49716, 64, tc.regs)
			want := renderNukedPCM(t, 49716, 64, tc.regs)
			if delta := maxPCMDelta(got, want); delta > tc.maxDelta {
				t.Fatalf("OPL2 melodic delta too large: %d", delta)
			}
		})
	}
}

func TestNewOPL2RhythmOutputComparableEnergyToNuked(t *testing.T) {
	regs := []uint16{
		0x01, 0x20,
		0x30, 0x01, 0x33, 0x01,
		0x31, 0x01, 0x34, 0x01,
		0x32, 0x01, 0x35, 0x01,
		0x50, 0x08, 0x53, 0x00,
		0x51, 0x00, 0x54, 0x00,
		0x52, 0x00, 0x55, 0x00,
		0x70, 0xF4, 0x73, 0xF6,
		0x71, 0xF4, 0x74, 0xF6,
		0x72, 0xF4, 0x75, 0xF6,
		0x90, 0x24, 0x93, 0x14,
		0x91, 0x24, 0x94, 0x14,
		0x92, 0x24, 0x95, 0x14,
		0xC6, 0x31, 0xC7, 0x30, 0xC8, 0x30,
		0xA6, 0x98, 0xB6, 0x11,
		0xA7, 0x88, 0xB7, 0x11,
		0xA8, 0x78, 0xB8, 0x11,
		0xBD, 0x3F,
	}

	got := renderImpSynthOPL2(49716, 64, regs)
	want := renderNukedPCM(t, 49716, 64, regs)
	if !pcmHasSignal(got) || !pcmHasSignal(want) {
		t.Fatal("expected both renderers to produce audible rhythm output")
	}
	gotEnergy := monoAbsEnergy(got)
	wantEnergy := monoAbsEnergy(want)
	if gotEnergy*4 < wantEnergy || wantEnergy*4 < gotEnergy {
		t.Fatalf("rhythm energy diverged too far: got=%d want=%d", gotEnergy, wantEnergy)
	}
}

func BenchmarkGenerateStereoS16_2048Frames(b *testing.B) {
	benchmarkGenerateStereoS16(b, 49716, benchmarkVoiceChannels)
}

func BenchmarkGenerateStereoS16_2048Frames_44100Hz(b *testing.B) {
	benchmarkGenerateStereoS16(b, 44100, benchmarkVoiceChannels)
}

func BenchmarkGenerateStereoS16_2048Frames_8Voices(b *testing.B) {
	benchmarkGenerateStereoS16(b, 49716, benchmarkEightVoiceChannels)
}

func BenchmarkGenerateStereoS16_2048Frames_8Voices_44100Hz(b *testing.B) {
	benchmarkGenerateStereoS16(b, 44100, benchmarkEightVoiceChannels)
}

func BenchmarkGenerateStereoS16_2048Frames_MaxVoices(b *testing.B) {
	benchmarkGenerateStereoS16(b, 49716, benchmarkMaxVoiceChannels)
}

func BenchmarkGenerateStereoS16_2048Frames_MaxVoices_44100Hz(b *testing.B) {
	benchmarkGenerateStereoS16(b, 44100, benchmarkMaxVoiceChannels)
}

func benchmarkGenerateStereoS16(b *testing.B, sampleRate int, channels []int) {
	opl := benchmarkSynth(sampleRate, channels)
	b.ReportAllocs()
	b.SetBytes(2048 * 2 * 2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = opl.GenerateStereoS16(2048)
	}
}

func BenchmarkRenderExampleSongTwinkle44100Hz(b *testing.B) {
	events, patch := mustLoadExampleSongBenchmarkData(b)
	totalDurationMS := exampleSongReleaseMS
	if len(events) > 0 {
		last := events[len(events)-1]
		totalDurationMS += last.StartMS + last.DurationMS
	}
	totalFrames := int(math.Round(float64(totalDurationMS) * exampleSongSampleRate / 1000.0))

	b.ReportAllocs()
	b.SetBytes(int64(totalFrames * 2 * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opl := New(exampleSongSampleRate)
		configureExampleSongVoice(opl, patch)
		benchmarkRenderExampleSong(opl, events)
	}
}

func BenchmarkRenderExampleSongTwinkle44100Hz_FastSilenceFill(b *testing.B) {
	events, patch := mustLoadExampleSongBenchmarkData(b)
	totalDurationMS := exampleSongReleaseMS
	if len(events) > 0 {
		last := events[len(events)-1]
		totalDurationMS += last.StartMS + last.DurationMS
	}
	totalFrames := int(math.Round(float64(totalDurationMS) * exampleSongSampleRate / 1000.0))

	b.ReportAllocs()
	b.SetBytes(int64(totalFrames * 2 * 2))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		opl := New(exampleSongSampleRate)
		configureExampleSongVoice(opl, patch)
		benchmarkRenderExampleSongFastSilenceFill(opl, events)
	}
}

func BenchmarkGenerateStereoS16_2048Frames_ReferenceCorpus(b *testing.B) {
	for _, tc := range synthReferenceCorpusCases {
		b.Run(tc.name, func(b *testing.B) {
			opl := benchmarkReferenceCorpusSynth(49716, tc.regs)
			b.ReportAllocs()
			b.SetBytes(2048 * 2 * 2)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = opl.GenerateStereoS16(2048)
			}
		})
	}
}

func BenchmarkGenerateStereoS16_2048Frames_ReferenceCorpus_44100Hz(b *testing.B) {
	for _, tc := range synthReferenceCorpusCases {
		b.Run(tc.name, func(b *testing.B) {
			opl := benchmarkReferenceCorpusSynth(44100, tc.regs)
			b.ReportAllocs()
			b.SetBytes(2048 * 2 * 2)
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = opl.GenerateStereoS16(2048)
			}
		})
	}
}
