chip esp32

hardware {
    pin led: Output = 2
    pin btn: PullUpInput = 0
    device bme: BME280 on i2c0 at 0x76
    i2c i2c0(sda: 21, scl: 22)
}
