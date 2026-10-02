
hardware {
    bus bus: I2C(sda: 21, scl: 22)
    device a: BME280 on bus at 0x76
    device b: BME280 on bus at 0x76
}
