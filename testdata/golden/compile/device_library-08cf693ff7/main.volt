chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device ds3231: DS3231 on i2c0 at 0x68
}
every 1s {
    led.toggle()
}
