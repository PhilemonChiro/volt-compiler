
chip rp2040
hardware {
    pin led: Output = 25
    bus i2c0: I2C(sda: 4, scl: 5)
    device bme: BME280 on i2c0 at 0x76
}
task reader {
    every 2s {
        var temp: float = bme.temperature()
    }
}
