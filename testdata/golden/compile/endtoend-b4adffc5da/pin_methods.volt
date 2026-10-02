
hardware {
    pin led: Output = 2
    pin btn: PullUpInput = 0
    pin bat: Analog = 36
}
setup {
    led.on()
    led.off()
    led.toggle()
    let pressed = btn.is_pressed()
    let val = bat.read()
}
