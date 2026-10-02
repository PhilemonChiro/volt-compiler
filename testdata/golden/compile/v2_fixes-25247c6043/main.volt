chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
}
task blinker {
    every 1s {
        led1.toggle()
    }
    every 100ms {
        led2.toggle()
    }
}
