
chip esp32
hardware {
    pin pad: Touch = 15
}
setup {
    pad.set_threshold(25)
    var raw: int = pad.read()
}
