
chip esp32

task net {
    every 5s {
        if system.free_heap() > 10000 {
            wifi.connect("ssid", "pass")
        }
    }
}
