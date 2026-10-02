
chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device bme: BME280 on i2c0 at 0x76
}
task read_sensor {
    every 2s {
        var temp: float = bme.temperature()
        var hum: float = bme.humidity()
        if temp > 30.0 {
            led.on()
        }
    }
}
