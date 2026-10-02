
chip esp32
hardware { pin led: Output = 2 }
task loop_check {
    every 1s {
        while mqtt.is_connected() {
            led.toggle()
            sleep(100ms)
        }
    }
}
