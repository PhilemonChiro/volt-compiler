
chip esp32
hardware { pin led: Output = 2 }
task worker {
    every 1s {
        led.on() or retry(5)
    }
}
