
chip esp32
hardware { pin led: Output = 2 }
setup {
    wifi.connect("ssid", "pass") or retry(5)
    mqtt.connect("mqtt://broker") or retry(3)
}
