
chip esp32
hardware {
    bus sensors: I2C(sda: 21, scl: 22)
    device bme: BME280 on sensors at 0x76
}
task monitor {
    every 10s {
        var temp: float = bme.temperature() or skip
    }
}
