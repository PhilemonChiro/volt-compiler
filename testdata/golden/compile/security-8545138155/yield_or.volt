
chip esp32
hardware { pin led = 2 }
task worker {
    every 1s {
        led.toggle()
    }
    sleep(500ms) or skip
}
