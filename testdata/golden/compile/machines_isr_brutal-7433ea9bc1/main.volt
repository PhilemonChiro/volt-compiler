chip esp32
hardware { pin led: Output = 4 }
func flash() {
    led.on()
    sleep(50ms)
    led.off()
}
task t { every 1s { flash() } }
