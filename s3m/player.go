package s3m

type Channel struct {
}

func (channel *Channel) Read(data []byte) (int, error) {
    for i := range data {
        data[i] = 0
    }

    return len(data), nil
}

type Player struct {
    Channels []*Channel
    S3M *S3MFile
}

func MakePlayer(file *S3MFile, sampleRate int) *Player {
    return &Player{
    }
}
