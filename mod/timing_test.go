package mod

import (
	"io"
	"reflect"
	"testing"
)

func TestTickTimingAndPatternBreak(t *testing.T) {
	file := &ModFile{Channels: 2, SongLength: 2, Orders: []byte{0, 1}, Patterns: []Pattern{
		{Rows: []Row{{Notes: []Note{{EffectNumber: EffectPatternBreak, EffectParameter: 1}, {EffectNumber: EffectSetSpeed, EffectParameter: 127}}}}},
		{Rows: []Row{{}, {Notes: []Note{{EffectNumber: EffectSetSpeed, EffectParameter: 3}}}}},
	}}
	player := MakePlayer(file, 48000)
	var rows [][2]int
	player.SetOnChangeRow(func(row int) { rows = append(rows, [2]int{player.CurrentOrder, row}) })
	pcm := player.RenderToPCM()
	// The reader must also work with unaligned byte requests.
	buf := make([]byte, 137)
	n, err := io.CopyBuffer(io.Discard, struct{ io.Reader }{pcm}, buf)
	if err != nil {
		t.Fatal(err)
	}
	if n != (6+3)*944*8 {
		t.Fatalf("rendered %d bytes", n)
	}
	if want := [][2]int{{0, 0}, {1, 1}}; !reflect.DeepEqual(rows, want) {
		t.Fatalf("rows %v, want %v", rows, want)
	}
}
