chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var i: int = 0
    while i < 10 {
        i += 1
        if i == 5 {
            continue
        }
    }
}
