
chip esp32
setup {
    wifi.connect("net", "pass") or retry(5)
}
task telemetry {
    every 30s {
        mqtt.send_int("t", 25) or retry(3)
    }
}
