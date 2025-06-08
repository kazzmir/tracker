package main

import (
    "os"
    "log"

    "github.com/kazzmir/tracker/mod"

    "github.com/hajimehoshi/ebiten/v2"
    "github.com/hajimehoshi/ebiten/v2/inpututil"
)

type Engine struct {
    ModFile *mod.ModFile
}

func MakeEngine(modFile *mod.ModFile) *Engine {
    return &Engine{
        ModFile: modFile,
    }
}

func (engine *Engine) Update() error {

    keys := inpututil.AppendJustPressedKeys(nil)
    for _, key := range keys {
        switch key {
            case ebiten.KeyEscape, ebiten.KeyCapsLock:
                return ebiten.Termination
        }
    }

    return nil
}

func (engine *Engine) Draw(screen *ebiten.Image) {
}

func (engine *Engine) Layout(outsideWidth, outsideHeight int) (int, int) {
    return outsideWidth, outsideHeight
}

func main(){
    log.SetFlags(log.Lshortfile | log.Ldate | log.Lmicroseconds)
    if len(os.Args) < 2 {
        log.Println("Usage: tracker <path to mod file>")
        return
    }
    path := os.Args[1]
    file, err := os.Open(path)
    if err != nil {
        log.Printf("Error opening %v: %v", path, err)
    }

    modFile, err := mod.Load(file)
    if err != nil {
        log.Printf("Error loading %v: %v", path, err)
    } else {
        log.Printf("Successfully loaded %v", path)
        log.Printf("Mod name: '%v'", modFile.Name)
    }

    ebiten.SetWindowSize(640, 480)
    ebiten.SetWindowTitle("Mod Tracker")
    ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)

    engine := MakeEngine(modFile)

    err = ebiten.RunGame(engine)
    if err != nil {
        log.Printf("Error: %v", err)
    }
}
