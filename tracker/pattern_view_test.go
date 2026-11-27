//go:build guitest

package main

import (
	"image"
	"testing"

	"github.com/ebitenui/ebitenui/widget"
	"github.com/kazzmir/tracker/xm"
)

// Run with a display, e.g. xvfb-run -a go test -tags guitest ./tracker.
func TestPatternViewFollowsXMRowCount(t *testing.T) {
	module := &xm.XMFile{
		Channels: 2,
		Orders:   []byte{0, 1, 0},
		Patterns: []xm.Pattern{{Rows: 128}, {Rows: 32}},
	}
	player := xm.MakePlayer(module, 48000)
	face, err := loadFont(24)
	if err != nil {
		t.Fatal(err)
	}
	hooks, view := makeChannelView(player, &face)
	player.SetOnChangeOrder(hooks.UpdateOrder)

	checkRows := func(count int) int {
		t.Helper()
		var scroll float64
		var contentHeight int
		for index, child := range view.Children() {
			column := child.(*widget.Container)
			scroller := column.Children()[1].(*widget.ScrollContainer)
			scroller.SetLocation(image.Rect(0, 0, 100, 650))
			if scroller.ScrollTop != 0 {
				t.Fatal("scroll position was not reset on order change")
			}
			hooks.UpdateRow(count - 1)
			if scroller.ScrollTop <= 0 || scroller.ScrollTop > 1 {
				t.Fatalf("last row %02X did not scroll into view: %v", count-1, scroller.ScrollTop)
			}
			_, height := scroller.PreferredSize()
			if index == 0 {
				scroll, contentHeight = scroller.ScrollTop, height
			} else if scroller.ScrollTop != scroll || height != contentHeight {
				t.Fatal("row labels and channel columns are out of sync")
			}
			hooks.UpdateRow(0)
		}
		return contentHeight
	}

	long := checkRows(128)
	hooks.UpdateRow(127)
	player.NextOrder()
	if short := checkRows(32); short >= long {
		t.Fatalf("shorter pattern height %d did not shrink from %d", short, long)
	}
	player.NextOrder()
	if height := checkRows(128); height != long {
		t.Fatalf("long pattern height after switching back: got %d, want %d", height, long)
	}
}
