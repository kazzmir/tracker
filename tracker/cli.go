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

    for _, channel := range player.GetChannelReaders() {
        playChannel := context.NewPlayer(channel)
        otoPlayers = append(otoPlayers, playChannel)
        playChannel.SetBufferSize(sampleRate * 2 * 4 / 20)
        playChannel.SetVolume(0.8)
        // engine.Players = append(engine.Players, playChannel)
        playChannel.Play()

        /*
        runtime.AddCleanup(playChannel, func(i int) {
            log.Printf("Cleaning up player %v", i)
        }, i)
        */

        if playChannel.Err() != nil {
            return playChannel.Err()
        }
        // playChannel.Play()
    }

    var rate float32 = 1.0 / 100
    sleepTime := time.Duration(float64(time.Second) * float64(rate))

    last := time.Now()
    var counter time.Duration

    for {
        current := time.Now()

        diff := current.Sub(last)

        counter += diff
        last = current
        // log.Printf("Update diff %v: %v vs %v. counter %v sleep time %v", diff, rate, rate * float32(diff / time.Millisecond), counter, sleepTime)

        for counter > sleepTime {
            // player.Update(rate)
            counter -= sleepTime
        }

        time.Sleep(5 * time.Millisecond)

        // KeepAlive() must be inside the loop because if it is outside (under the loop) then
        // the optimizer will remove the call to KeepAlive(), thus leaving otoPlayers available
        // to be GC'd.
        runtime.KeepAlive(otoPlayers)
    }

    return nil
}
