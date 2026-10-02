
chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var x: int = 0
    while x < 10 {
        led.toggle()
        x += 1
    }
}
