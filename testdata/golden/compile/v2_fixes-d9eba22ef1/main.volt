chip esp32
hardware {
    pin led: Output = 2
}
task fast {
    every 100us strict {
        led.toggle()
    }
}
