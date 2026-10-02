
chip esp32
hardware {
    pin led: Output = 2
}
setup {
    led.read()
}
