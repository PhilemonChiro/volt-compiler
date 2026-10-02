
chip esp32
hardware { pin led: Output = 2 }
enum State { idle, active, error }
setup {
    var s: State = State.idle
    match(s) {
        .idle -> led.off()
        .active -> led.on()
        .error -> led.toggle()
    }
}
