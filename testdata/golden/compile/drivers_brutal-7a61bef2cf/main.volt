chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device bme1: BME280 on i2c0 at 0x76
    device bme2: BME280 on i2c0 at 0x77
}
every 1s { led.toggle() }
