package xm

import "testing"

func TestAmigaPeriod(t *testing.T) {
	for _, test := range []struct {
		note, tune, want int
	}{
		{48, 0, 1712}, // C-4
		{49, 0, 1616}, // C#4: not the equal-temperament approximation
		{59, 0, 906},  // B-4
		{60, 0, 856},  // C-5
		{48, -8, 1718},
		{48, 8, 1706},
		{48, -128, 1814},
		{48, 120, 1622},
	} {
		if period := amigaPeriod(test.note, test.tune); period != test.want {
			t.Errorf("note %d, finetune %d: got period %d, want %d", test.note, test.tune, period, test.want)
		}
	}
}
