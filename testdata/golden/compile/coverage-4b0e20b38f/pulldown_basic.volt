
chip esp32
hardware {
    pin btn: PullDownInput = 4
}
setup {
    var pressed: bool = btn.is_pressed()
    var level: int = btn.read()
}
