
chip esp32
hardware { pin led: Output = 2 }
task multi_yield {
    every 500ms {
        led.on()
    }
    sleep(100ms)
}
