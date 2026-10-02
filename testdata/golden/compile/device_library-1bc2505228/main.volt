chip esp32
hardware {
    pin led: Output = 2
    bus i2c0: I2C(sda: 21, scl: 22)
    device mlx90614: MLX90614 on i2c0 at 0x5A
}
every 1s {
    led.toggle()
}
