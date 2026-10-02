
hardware {
    bus s: I2C(sda: 21, scl: 22)
    device bme: BME280 on s at 0x76
}
setup {
    wifi.connect("net", "pass") or retry(10)
}
every 10s when wifi.is_connected() {
    var temp: float = bme.temperature() or skip
    mqtt.send_float("temp", temp) or retry(3)
}
