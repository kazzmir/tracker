package main

import (
    "os"
    "log"

    "github.com/kazzmir/tracker/mod"
)

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
}
