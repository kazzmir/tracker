package xm

// FastTracker's Amiga period table has eight steps per semitone, with
// entries on either side of the octave for finetune interpolation.
var amigaPeriods = [...]int{
	907, 900, 894, 887, 881, 875, 868, 862, 856, 850, 844, 838, 832, 826, 820, 814,
	808, 802, 796, 791, 785, 779, 774, 768, 762, 757, 752, 746, 741, 736, 730, 725,
	720, 715, 709, 704, 699, 694, 689, 684, 678, 675, 670, 665, 660, 655, 651, 646,
	640, 636, 632, 628, 623, 619, 614, 610, 604, 601, 597, 592, 588, 584, 580, 575,
	570, 567, 563, 559, 555, 551, 547, 543, 538, 535, 532, 528, 524, 520, 516, 513,
	508, 505, 502, 498, 494, 491, 487, 484, 480, 477, 474, 470, 467, 463, 460, 457,
	453, 450, 447, 443, 440, 437, 434, 431,
}

func amigaPeriod(note, tune int) int {
	note = max(0, note)
	index := note%12*8 + 8 + tune/16
	next, fraction := index+1, tune%16
	if tune < 0 {
		next, fraction = index-1, -fraction
	}
	first := amigaPeriods[max(0, min(len(amigaPeriods)-1, index))]
	second := amigaPeriods[max(0, min(len(amigaPeriods)-1, next))]
	return max(1, ((first*(16-fraction)+second*fraction)*2)>>(note/12))
}
