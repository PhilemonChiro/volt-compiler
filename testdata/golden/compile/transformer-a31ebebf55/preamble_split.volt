
chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
}
task worker {
    led1.on()
    every 500ms {
        led1.toggle()
    }
    every 1s {
        led2.toggle()
    }
}
