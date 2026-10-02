chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device dev: ADS1115 on i2c0 at 0x48
}
every 1s {
    led.toggle()
    dev.read_raw(0)
}
