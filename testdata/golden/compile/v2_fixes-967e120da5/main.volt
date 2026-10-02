chip esp32
hardware {
    pin led: Output = 2
}
setup {
    let x = 0x0F | 0xF0
}
