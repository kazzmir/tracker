package common

type NoteInfo interface {
    GetNotePosition() int
    GetName() string
    GetSampleName() string
    GetEffectName() string
}
