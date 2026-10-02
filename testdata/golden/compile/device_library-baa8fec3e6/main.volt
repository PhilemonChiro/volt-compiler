chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device apds9960: APDS9960 on i2c0 at 0x39
}
every 1s {
    led.toggle()
}
