
enum AlertLevel { normal, warning, critical }

hardware {
    pin led = 2
    pin buzzer: PWM = 25
    bus s: I2C(sda: 21, scl: 22)
    device bme: BME280 on s at 0x76
}

storage boot_count: int = 0
channel alerts: int[8]

setup {
    boot_count += 1
    system.watchdog_enable(15000)
    wifi.connect("net", "pass") or retry(10)
    mqtt.connect("mqtt://broker") or retry(5)
    buzzer.beep_startup()
}

every 10s when wifi.is_connected() {
    system.watchdog_feed()
    var temp: float = bme.temperature() or skip
    mqtt.send_float("temp", temp) or retry(3)
    if temp > 35 {
        alerts.send(1)
        buzzer.beep_warning()
    }
}

every 100ms {
    if alerts.has_data() {
        var code: int = alerts.receive()
        led.on()
    }
}

every 1s { led.toggle() }
