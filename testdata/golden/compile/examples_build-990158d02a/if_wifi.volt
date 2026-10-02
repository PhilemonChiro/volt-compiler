
chip esp32
hardware { pin led: Output = 2 }
task monitor {
    every 5s {
        if wifi.is_connected() {
            led.on()
        }
    }
}
