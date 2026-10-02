
chip esp32
setup {
    ble.advertise("Volt Sensor")
    var temp_id: int = ble.expose_int("temperature", 0)
    var hum_id: int = ble.expose_float("humidity", 0.0)
}
task update {
    every 5s {
        ble.update_int(0, 25)
        ble.update_float(1, 65.5)
    }
}
