chip esp32

hardware {
    pin led: Output = 2
}

task blink {
    every 500ms { led.toggle() }
}

task slow {
    every 2s { led.toggle() }
}
