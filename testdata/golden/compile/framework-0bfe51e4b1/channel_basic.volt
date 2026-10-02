
chip esp32
hardware {
    pin led: Output = 2
}
channel data: int[8]
task producer {
    every 1s {
        data.send(42)
    }
}
task consumer {
    every 100ms {
        var val: int = data.receive()
        if val > 0 {
            led.on()
        }
    }
}
