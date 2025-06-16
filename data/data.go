package data

import (
    "embed"
    "io/fs"
)

//go:embed data/
var Data embed.FS

func FindMod() (fs.File, error) {
}
