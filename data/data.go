package data

import (
    "embed"
    "strings"
    "io/fs"
    // "log"
)

//go:embed data/*
var Data embed.FS

func FindMod() (fs.File, string, error) {
    entries, err := fs.ReadDir(Data, "data")
    if err != nil {
        return nil, "", err
    }
    for _, entry := range entries {
        if strings.HasSuffix(strings.ToLower(entry.Name()), ".mod") {
            file, err := Data.Open("data/" + entry.Name())
            if err != nil {
                return nil, "", err
            }
            return file, entry.Name(), nil
        }
    }

    return nil, "", fs.ErrNotExist
}
