
chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device bme: BME280 on i2c0 at 0x76
}

storage readings_count: int = 0

setup {
    wifi.connect("MySSID", "MyPass")
    mqtt.connect("mqtt://broker.local")
}

task telemetry {
    every 30s {
        var temp: float = bme.temperature()
        var hum: float = bme.humidity()
        mqtt.send("home/temp", "temp={temp}")
        mqtt.send("home/hum", "hum={hum}")
        readings_count = readings_count + 1
        storage.save("count", readings_count)
        led.toggle()
    }
}
