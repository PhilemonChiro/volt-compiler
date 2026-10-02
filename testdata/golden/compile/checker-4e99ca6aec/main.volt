
hardware {
    pin led: Output = 2
}
setup {
    led.toggle()
    led.on()
    led.off()
}
