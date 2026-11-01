hardware {
    pin led = 2
    pin soil: Analog = 32
    pin light: Analog = 33
    pin temperature: Analog = 36
}

setup {
    boot_count += 1
    wifi.connect("WeatherNet", "password123") or retry(10)
    mqtt.connect("mqtt://192.168.1.100") or retry(5)
    log("boot", "Weather Station v3 — boot #{boot_count}")
}

channel readings: int[16]

storage boot_count: int = 0

storage total_readings: int = 0

var soil_pct: int = 0

var light_lux: int = 0

var temp_raw: int = 0

every 10s {
    let s: int = soil.read()
    let l: int = light.read()
    let t: int = temperature.read()
    soil_pct = s * 100 / 4095
    light_lux = l * 1000 / 4095
    temp_raw = t
    total_readings += 1
    readings.send(soil_pct)
    log("sensor", "soil={soil_pct}% light={light_lux}lux")
}

every 30s when wifi.is_connected() {
    mqtt.send_int("weather/soil", soil_pct) or retry(3)
    mqtt.send_int("weather/light", light_lux) or retry(3)
    mqtt.send_int("weather/temp", temp_raw) or retry(3)
    while readings.has_data() {
        let val: int = readings.receive()
        mqtt.send_int("weather/soil_buffered", val) or retry(3)
    }
}

every 60s {
    storage.save("boot_count", boot_count)
    storage.save("total_readings", total_readings)
}

task name {
    deep_sleep(60s)
}

every 30s {
    let heap: int = system.free_heap()
    let up: int = system.uptime()
    log("health", "uptime={up}s heap={heap} readings={total_readings}")
}

every 1s { led.toggle() }
