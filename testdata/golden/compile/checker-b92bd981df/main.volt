
setup {
    wifi.connect("net", "pass")
    mqtt.connect("mqtt://localhost")
    mqtt.send("topic", "data")
    let connected = wifi.is_connected()
}
