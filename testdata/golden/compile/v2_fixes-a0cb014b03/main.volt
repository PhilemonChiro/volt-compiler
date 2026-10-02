chip rp2040
hardware {
    pin led: Output = 25
}
setup {
    led.on()
}
