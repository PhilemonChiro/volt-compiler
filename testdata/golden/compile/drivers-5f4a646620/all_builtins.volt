
chip esp32
storage count: int = 0
setup {
    count = count + 1
    storage.save("count", count)
    system.watchdog_enable(10000)
    ble.advertise("Volt")
    wifi.connect("ssid", "pass")
}
task main {
    every 5s {
        system.watchdog_feed()
        if ble.is_connected() {
            ble.update_int(0, count)
        }
    }
}
