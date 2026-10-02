
setup {
    wifi.connect("ssid", "pass")
    mqtt.connect("mqtt://broker")
    mqtt.send("topic", "data")
    mqtt.send_int("topic", 42)
    mqtt.send_float("topic", 3.14)
    let connected = wifi.is_connected()
}
