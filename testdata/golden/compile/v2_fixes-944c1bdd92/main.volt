chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var mask: u8 = 0xFF
    let result = mask & 0x0F
}
