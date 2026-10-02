chip esp32

hardware {
    pin led: Output = 2
}

task a {
    every 1s { led.toggle() }
}

task b {
    every 2s { led.toggle() }
}
