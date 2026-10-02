
chip rp2040
enum Mode {
    standby
    active
    sleep
}
setup {
    var m: Mode = Mode.standby
}
