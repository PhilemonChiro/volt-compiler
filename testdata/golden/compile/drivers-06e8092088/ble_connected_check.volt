
chip esp32
hardware {
    pin led: Output = 2
}
setup {
    ble.advertise("Device")
}
task status {
    every 1s {
        if ble.is_connected() {
            led.on()
        } else {
            led.off()
        }
    }
}
