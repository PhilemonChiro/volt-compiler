
hardware {
    bus bus: I2C(sda: 21, scl: 22)
    device bme: BME280 on bus at 0x76
}
setup {
    log("temp: {bme.temperature()}")
}
