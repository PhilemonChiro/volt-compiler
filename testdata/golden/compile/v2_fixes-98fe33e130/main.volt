chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var mask: int = 0
    mask |= 0x04
}
