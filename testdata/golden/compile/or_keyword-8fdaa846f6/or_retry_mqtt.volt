
chip esp32
setup {
    wifi.connect("net", "pass") or retry(10)
    mqtt.connect("mqtt://broker") or retry(5)
}
