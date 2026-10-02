
chip esp32
hardware {
    pin led: Output = 2
}
setup {
    led.write(1, 2, 3)
}
