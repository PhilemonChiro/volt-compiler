
chip esp32
hardware { pin led: Output = 2 }
setup {
    var x: int = 2
    match(x) {
        1 -> led.on()
        2 -> led.off()
        _ -> led.toggle()
    }
}
