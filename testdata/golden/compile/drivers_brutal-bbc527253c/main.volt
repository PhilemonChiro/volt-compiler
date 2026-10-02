chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device dev: QMC5883L on i2c0 at 0x0D
}
every 1s {
    led.toggle()
    dev.x()
}
