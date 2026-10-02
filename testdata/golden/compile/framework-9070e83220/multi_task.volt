
chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
}
task fast {
    every 100ms {
        led1.toggle()
    }
}
task slow {
    every 1s {
        led2.toggle()
    }
}
