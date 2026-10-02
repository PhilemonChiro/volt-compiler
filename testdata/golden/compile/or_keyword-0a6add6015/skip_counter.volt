
chip esp32
hardware { pin led: Output = 2 }
task worker {
    every 500ms {
        led.toggle() or skip
    }
}
