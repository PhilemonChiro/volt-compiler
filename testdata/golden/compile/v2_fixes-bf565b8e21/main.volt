chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var i: int = 0
    while i < 10 {
        if i == 5 {
            break
        }
        i += 1
    }
}
