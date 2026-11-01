chip esp32

hardware {
    pin led: Output = 2
}

setup {
    var m: Mode = Mode.idle
    match(m) {
        .idle -> led.off()
        .running -> led.on()
        _ -> led.toggle()
    }
}

enum Mode { idle, running }
