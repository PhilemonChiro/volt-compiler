chip esp32
hardware {
    pin led: Output = 2
}
task blink {
    every 5min {
        led.toggle()
    }
}
