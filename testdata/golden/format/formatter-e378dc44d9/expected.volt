chip esp32

hardware {
    pin led: Output = 2
}

setup {
    var x: int = 5
    if x > 0 {
        led.on()
    } else {
        led.off()
    }
}
