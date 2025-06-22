package main

import (
    "image/color"
    "bytes"
    _ "embed"
    "fmt"

    "github.com/kazzmir/tracker/common"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/text/v2"
    "github.com/hajimehoshi/ebiten/v2/vector"

    "github.com/ebitenui/ebitenui"
    "github.com/ebitenui/ebitenui/widget"
    ui_image "github.com/ebitenui/ebitenui/image"
)

//go:embed futura.ttf
var FuturaTTF []byte

type UIHooks struct {
    UpdateRow func(int)
    UpdateOrder func(int, int)
    UpdateSpeed func(int, int)
}
func loadFont(size float64) (text.Face, error) {
    source, err := text.NewGoTextFaceSource(bytes.NewReader(FuturaTTF))
    if err != nil {
        return nil, err
    }

    return &text.GoTextFace{
        Source: source,
        Size: size,
    }, nil
}

func lighten(col color.Color, amount int) color.Color {
    return col
}

func makeNineImage(img *ebiten.Image, border int) *ui_image.NineSlice {
    width := img.Bounds().Dx()
    return ui_image.NewNineSliceSimple(img, border, width - border * 2)
}

func makeRoundedButtonImage(width int, height int, border int, col color.Color) *ebiten.Image {
    img := ebiten.NewImage(width, height)

    vector.DrawFilledRect(img, float32(border), 0, float32(width - border * 2), float32(height), col, true)
    vector.DrawFilledRect(img, 0, float32(border), float32(width), float32(height - border * 2), col, true)
    vector.DrawFilledCircle(img, float32(border), float32(border), float32(border), col, true)
    vector.DrawFilledCircle(img, float32(width-border), float32(border), float32(border), col, true)
    vector.DrawFilledCircle(img, float32(border), float32(height-border), float32(border), col, true)
    vector.DrawFilledCircle(img, float32(width-border), float32(height-border), float32(border), col, true)

    return img
}

func makeNineRoundedButtonImage(width int, height int, border int, col color.Color) *widget.ButtonImage {
    return &widget.ButtonImage{
        Idle: makeNineImage(makeRoundedButtonImage(width, height, border, col), border),
        Hover: makeNineImage(makeRoundedButtonImage(width, height, border, lighten(col, 50)), border),
        Pressed: makeNineImage(makeRoundedButtonImage(width, height, border, lighten(col, 20)), border),
    }
}

type UIPlayer interface {
    GetName() string
    GetCurrentOrder() int
    GetPattern() int
    GetSongLength() int
    GetSpeed() int
    GetBPM() int
    GetChannelCount() int
    GetRowNoteInfo(channel int, row int) common.NoteInfo
}

func makeUI(player UIPlayer) (*ebitenui.UI, UIHooks) {
    face, _ := loadFont(19)

    rootContainer := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
            // widget.RowLayoutOpts.Padding(widget.Insets{Top: 0, Bottom: 0}),
        )),
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255})),
    )

    // put info stuff here
    infoContainer := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(1),
        )),
        widget.ContainerOpts.WidgetOpts(
            widget.WidgetOpts.LayoutData(widget.RowLayoutData{
                // Stretch: true,
            }),
        ),
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 64, G: 64, B: 64, A: 255})),
        /*
        widget.ContainerOpts.WidgetOpts(
            widget.WidgetOpts.MinSize(800, 100),
        ),
        */
    )

    infoContainer.AddChild(widget.NewText(
        widget.TextOpts.Text(fmt.Sprintf("Mod name: %s", player.GetName()), face, color.White),
    ))

    orderText := widget.NewText(
        widget.TextOpts.Text(fmt.Sprintf("Order: %v/%v", player.GetCurrentOrder(), player.GetSongLength()), face, color.White),
    )

    patternText := widget.NewText(
        widget.TextOpts.Text(fmt.Sprintf("Pattern: %02X", player.GetPattern()), face, color.White),
    )

    speedText := widget.NewText(
        widget.TextOpts.Text(fmt.Sprintf("Speed: %d BPM: %d", player.GetSpeed(), player.GetBPM()), face, color.White),
    )

    infoContainer.AddChild(orderText)
    infoContainer.AddChild(patternText)
    infoContainer.AddChild(speedText)

    rootContainer.AddChild(infoContainer)

    channels := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionHorizontal),
            widget.RowLayoutOpts.Spacing(8),
        )),
    )

    rowNumbers := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
        )),
    )

    var rowContainers [][]*widget.Container

    for i := range 64 {
        textColor := color.RGBA{R: 255, G: 255, B: 255, A: 255}
        if (i + 1) % 4 == 0 {
            textColor = color.RGBA{R: 200, G: 200, B: 0, A: 255}
        }

        container := widget.NewContainer(
            widget.ContainerOpts.Layout(widget.NewRowLayout(
                widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                widget.RowLayoutOpts.Spacing(2),
            )),
        )

        rowContainers = append(rowContainers, []*widget.Container{container})

        container.AddChild(widget.NewText(
            widget.TextOpts.Text(fmt.Sprintf("%02X", i), face, textColor),
        ))

        rowNumbers.AddChild(container)
    }

    for range 32 {
        rowNumbers.AddChild(widget.NewText(
            widget.TextOpts.Text("-", face, color.White),
        ))
    }

    makeRowScroller := func(content *widget.Container) *widget.ScrollContainer {
        return widget.NewScrollContainer(
            widget.ScrollContainerOpts.Content(content),
            widget.ScrollContainerOpts.StretchContentWidth(),
            widget.ScrollContainerOpts.WidgetOpts(
                widget.WidgetOpts.LayoutData(widget.RowLayoutData{
                    // FIXME: use a grid layout to automatically stretch the container
                    // row layout doesn't seem to stretch the container to the viewable area
                    MaxHeight: 650,
                }),
            ),
            widget.ScrollContainerOpts.Image(&widget.ScrollContainerImage{
                Idle: ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255}),
                Mask: ui_image.NewNineSliceColor(color.NRGBA{R: 255, G: 255, B: 255, A: 255}),
            }),
        )
    }

    var scrollers []*widget.ScrollContainer

    rowNumberScroller := makeRowScroller(rowNumbers)
    // rootContainer.AddChild(rowNumberScroller)
    scrollers = append(scrollers, rowNumberScroller)

    extraContainer := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
        )),
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255})),
    )
    extraContainer.AddChild(widget.NewText(
        widget.TextOpts.Text(" ", face, color.White),
    ))
    extraContainer.AddChild(rowNumberScroller)

    channels.AddChild(extraContainer)

    var removeChannels []widget.RemoveChildFunc

    var channelColumn []*widget.Container
    for i := range player.GetChannelCount() {
        column := widget.NewContainer(
            widget.ContainerOpts.Layout(widget.NewRowLayout(
                widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                widget.RowLayoutOpts.Spacing(2),
            )),
            widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255})),
        )

        column.AddChild(widget.NewText(
            widget.TextOpts.Text(fmt.Sprintf("Channel %d", i+1), face, color.White),
        ))

        background := color.NRGBA{R: 64, G: 64, B: 64, A: 255}
        if i % 2 == 0 {
            background = color.NRGBA{R: 96, G: 96, B: 96, A: 255}
        }

        data := widget.NewContainer(
            widget.ContainerOpts.Layout(widget.NewRowLayout(
                widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                widget.RowLayoutOpts.Spacing(2),
            )),
            widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(background)),
        )

        scroller := makeRowScroller(data)
        scrollers = append(scrollers, scroller)

        column.AddChild(scroller)

        channelColumn = append(channelColumn, data)
        channels.AddChild(column)
    }

    setupChannels := func(){
        for _, remove := range removeChannels {
            remove()
        }

        removeChannels = nil

        for row := range 64 {
            rowContainers[row] = rowContainers[row][1:]
        }

        for i := range player.GetChannelCount() {
            container := channelColumn[i]

            for row := range 64 {
                note := player.GetRowNoteInfo(i, row)

                noteName := note.GetName()
                if noteName == "" {
                    noteName = "..."
                }
                /*
                if note.PeriodFrequency > 0 {
                    noteName = fmt.Sprintf("%v", mod.ConvertToNote(note.PeriodFrequency))
                    // noteList.AddEntry(name)
                }
                */

                sampleName := note.GetSampleName()
                /*
                if note.SampleNumber > 0 {
                    sampleName = fmt.Sprintf("%02X", note.SampleNumber)
                }
                */

                // effectName := "..."
                effectName := note.GetEffectName()
                /*
                if note.EffectNumber > 0 || note.EffectParameter > 0 {
                    effectName = fmt.Sprintf("%X%02X", note.EffectNumber, note.EffectParameter)
                }
                */

                textContainer := widget.NewContainer(
                    widget.ContainerOpts.Layout(widget.NewRowLayout(
                        widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                        widget.RowLayoutOpts.Spacing(2),
                    )),
                )

                textContainer.AddChild(widget.NewText(
                    widget.TextOpts.Position(widget.TextPositionCenter, widget.TextPositionCenter),
                    widget.TextOpts.Text(fmt.Sprintf("%v %v %v", noteName, sampleName, effectName), face, color.White),
                ))

                rowContainers[row] = append(rowContainers[row], textContainer)
                removeChannels = append(removeChannels, container.AddChild(textContainer))
            }

            for range 32 {
                textContainer := widget.NewContainer(
                    widget.ContainerOpts.Layout(widget.NewRowLayout(
                        widget.RowLayoutOpts.Direction(widget.DirectionVertical),
                        widget.RowLayoutOpts.Spacing(2),
                    )),
                )

                textContainer.AddChild(widget.NewText(
                    widget.TextOpts.Position(widget.TextPositionCenter, widget.TextPositionCenter),
                    widget.TextOpts.Text("-", face, color.White),
                ))

                removeChannels = append(removeChannels, container.AddChild(textContainer))
            }
        }
    }

    setupChannels()

    rootContainer.AddChild(channels)

    ui := ebitenui.UI{
        Container: rootContainer,
    }

    currentRowHighlight := 0
    uiHooks := UIHooks{
        UpdateRow: func(row int) {
            if row < len(rowContainers) {
                top := row - 10
                if top < 0 {
                    top = 0
                }
                position := float64(top) / (64 + 10)

                for _, scroller := range scrollers {
                    scroller.ScrollTop = position
                }
                // log.Printf("Set scroll top to %v", rowScroll.ScrollTop)

                for _, container := range rowContainers[currentRowHighlight] {
                    container.BackgroundImage = nil
                }
                currentRowHighlight = row
                for _, container := range rowContainers[row] {
                    container.BackgroundImage = ui_image.NewNineSliceColor(color.NRGBA{R: 255, G: 0, B: 0, A: 128})
                }
            }
        },
        UpdateOrder: func(order int, pattern int) {
            setupChannels()

            orderText.Label = fmt.Sprintf("Order: %v/%v", order, player.GetSongLength())
            patternText.Label = fmt.Sprintf("Pattern: %02X", pattern)
        },
        UpdateSpeed: func(speed int, bpm int) {
            speedText.Label = fmt.Sprintf("Speed: %d BPM: %d", speed, bpm)
        },
    }

    uiHooks.UpdateRow(currentRowHighlight)

    return &ui, uiHooks
}

func makeDummyUI() *ebitenui.UI {
    rootContainer := widget.NewContainer(
        widget.ContainerOpts.Layout(widget.NewRowLayout(
            widget.RowLayoutOpts.Direction(widget.DirectionVertical),
            widget.RowLayoutOpts.Spacing(2),
            // widget.RowLayoutOpts.Padding(widget.Insets{Top: 0, Bottom: 0}),
        )),
        widget.ContainerOpts.BackgroundImage(ui_image.NewNineSliceColor(color.NRGBA{R: 32, G: 32, B: 32, A: 255})),
    )

    return &ebitenui.UI{
        Container: rootContainer,
    }
}
