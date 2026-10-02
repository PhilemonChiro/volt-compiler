
chip esp32
hardware {
    pin led: Output = 2
    device dht_sensor: DHT11 on gpio4 at 4
}
task read_dht {
    every 2s {
        var t: float = dht_sensor.temperature()
        var h: float = dht_sensor.humidity()
    }
}
