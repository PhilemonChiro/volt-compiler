
hardware {
    device bme: BME280 on bus at 0x76
}
task pid {
    every 1ms strict {
        bme.temperature()
    }
}
