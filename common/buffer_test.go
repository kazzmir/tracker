package common

import (
	"slices"
	"testing"
)

func TestPeekLatestAcrossWrap(t *testing.T) {
	buffer := MakeAudioBuffer(5)
	for _, sample := range []float32{1, 2, 3, 4, 5, 6, 7} {
		buffer.UnsafeWrite(sample)
	}
	latest := make([]float32, 3)
	if count := buffer.PeekLatest(latest); count != 3 || !slices.Equal(latest, []float32{5, 6, 7}) {
		t.Fatalf("latest %v (%d samples)", latest, count)
	}
	all := make([]float32, 6)
	if count := buffer.Read(all); count != 5 || !slices.Equal(all[:count], []float32{3, 4, 5, 6, 7}) {
		t.Fatalf("read after peek %v (%d samples)", all, count)
	}
}
