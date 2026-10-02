/* Safe-boot example — try blocks for boot sequence + telemetry

The setup `try { ... } or retry 5 times else restart` is the idiom for
bring-up: each step (wifi → mqtt → storage) must succeed before the
device runs. If any step keeps failing, reboot.

The telemetry `try { ... } or skip` lets a bad sensor read fall through
without killing the task — next tick gets a fresh attempt. */

chip esp32

hardware {
    pin led: Output = 4
    bus i2c0: I2C(sda: 21, scl: 22)
    device weather: BME280 on i2c0 at 0x76
}

setup {
    // Boot sequence — must come up cleanly. 5 attempts each, reboot on
    // exhaustion. wifi.connect / mqtt.connect / storage.load all idempotent.
    try {
        wifi.connect("home-ssid", "password")
        mqtt.connect("broker.local")
        storage.load("device_id")
    } or retry 5 times else restart
}

every 500ms { led.toggle() }

// Per-tick telemetry pipeline. A flaky sensor or transient MQTT failure
// drops this cycle, but the task keeps running.
every 30s {
    try {
        let t: float = weather.temperature()
        let h: float = weather.humidity()
        mqtt.send_float("env/temp", t)
        mqtt.send_float("env/hum", h)
    } or skip
}
