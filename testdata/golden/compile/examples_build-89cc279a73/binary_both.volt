
chip esp32
hardware { pin led: Output = 2 }
shared var ready: bool = false
task check {
    every 2s {
        ready = wifi.is_connected() && ble.is_connected()
    }
}
