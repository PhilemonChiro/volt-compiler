
chip esp32
hardware { pin led: Output = 2 }
task sender {
    every 10s when wifi.is_connected() {
        mqtt.send("topic", "data") or skip
        led.toggle()
    }
}
