chip esp32

hardware {
    pin led: Output = 4
    device bme: BME280 on i2c0 at 0x76
    i2c i2c0(sda: 21, scl: 22)
}

every 1s { led.toggle() }
