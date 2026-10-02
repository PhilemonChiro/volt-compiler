
chip esp32
hardware {
    pin led: Output = 2
}
enum Color { red, green, blue }
setup {
    var c: Color = Color.red
    if c == Color.red {
        led.on()
    }
}
