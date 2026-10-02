
hardware {
    pin sensor: Analog = 36
}
setup {
    wifi.connect("net", "pass")
    let val = sensor.read()
}
