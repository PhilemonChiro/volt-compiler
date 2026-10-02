
chip esp32
hardware { pin led: Output = 2 }
func blink_once() {
    led.on()
    sleep(100ms)
    led.off()
}
task worker {
    every 1s {
        blink_once()
    }
}
