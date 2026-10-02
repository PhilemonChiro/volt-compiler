chip esp32
hardware {
    pin led: Output = 2
}
setup {
    let x = 5
    if x < 10 {
        led.on()
    }
    if x > 0 {
        led.off()
    }
}
