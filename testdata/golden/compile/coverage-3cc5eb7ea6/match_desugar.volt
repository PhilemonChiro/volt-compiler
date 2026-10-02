
chip esp32
hardware { pin led: Output = 2 }
enum Mode { idle, running, error }
setup {
    var m: Mode = Mode.idle
    match(m) {
        .idle -> led.off()
        .running -> led.on()
        _ -> led.toggle()
    }
}
