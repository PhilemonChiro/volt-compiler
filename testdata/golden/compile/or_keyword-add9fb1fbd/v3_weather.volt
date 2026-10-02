
chip esp32
hardware {
    pin led: Output = 2
    bus sensors: I2C(sda: 21, scl: 22)
    device bme: BME280 on sensors at 0x76
}
storage boot_count: int = 0
setup {
    boot_count += 1
    wifi.connect("WeatherNet", "pass") or retry(10)
    mqtt.connect("mqtt://192.168.1.100") or retry(5)
}
task monitor {
    every 10s {
        var temp: float = bme.temperature() or skip
        var hum: float = bme.humidity() or skip
        mqtt.send_float("weather/temp", temp) or retry(3)
        mqtt.send_float("weather/hum", hum) or retry(3)
    }
}
task heartbeat {
    every 1s {
        led.toggle()
    }
}
