
chip esp32
hardware {
    pin led: Output = 2
}
shared var counter: int = 0
task inc {
    every 100ms {
        counter += 1
    }
}
task reader {
    every 1s {
        if counter > 10 {
            led.on()
        }
    }
}
