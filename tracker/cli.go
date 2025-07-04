package main

import (
    "log"
    "time"
    "runtime"
    "github.com/ebitengine/oto/v3"
)

func runCli(player TrackerPlayer, sampleRate int) error {
    var options oto.NewContextOptions
    options.SampleRate = sampleRate
    options.ChannelCount = 2
    options.Format = oto.FormatFloat32LE

    context, ready, err := oto.NewContext(&options)
    if err != nil {
        return err
    }

    log.Printf("Waiting for audio context to be ready...")
    <-ready

    var otoPlayers []*oto.Player

    for i, channel := range player.GetChannelReaders() {
        playChannel := context.NewPlayer(channel)
        otoPlayers = append(otoPlayers, playChannel)
        playChannel.SetBufferSize(sampleRate * 2 * 4 / 10)
        playChannel.SetVolume(0.8)
        // engine.Players = append(engine.Players, playChannel)
        playChannel.Play()

        runtime.AddCleanup(playChannel, func(i int) {
            log.Printf("Cleaning up player %v", i)
        }, i)

        if playChannel.Err() != nil {
            return playChannel.Err()
        }
        // playChannel.Play()
    }

    var rate float32 = 1.0 / 100
    sleepTime := time.Duration(float64(time.Second) * float64(rate))

    for {
        /*
        for i, player := range otoPlayers {
            if !player.IsPlaying() {
                log.Printf("Player stopped, restarting...")
                player.Play()
            }

            log.Printf("Player %v buffer size %v", i, player.BufferedSize())
        }
        */

        player.Update(rate)
        time.Sleep(sleepTime)
        runtime.KeepAlive(otoPlayers)
    }

    /*
    for {
        time.Sleep(time.Second / 60)
    }
    */

    /*
    runtime.KeepAlive(context)
    runtime.KeepAlive(otoPlayers)
    */

    return nil
}
