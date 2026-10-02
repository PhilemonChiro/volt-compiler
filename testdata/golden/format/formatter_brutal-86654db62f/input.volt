chip esp32
hardware {
  pin led: Output = 4
  bus i2c0: I2C(sda: 21, scl: 22)
  device bme: BME280 on i2c0 at 0x76
}
every 1s { led.toggle() }
