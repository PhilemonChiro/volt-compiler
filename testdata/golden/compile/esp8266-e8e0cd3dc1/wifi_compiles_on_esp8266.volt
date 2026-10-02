
chip esp8266
hardware {
    pin led: Output = 2
}
setup {
    wifi.connect("test", "pass")
}
