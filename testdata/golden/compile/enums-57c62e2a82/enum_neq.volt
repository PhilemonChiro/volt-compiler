
chip esp32
enum Mode {
    idle
    active
}
setup {
    var m: Mode = Mode.idle
    if m != Mode.active {
        log("status", "not active")
    }
}
