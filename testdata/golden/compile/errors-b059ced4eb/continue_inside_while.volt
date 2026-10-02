
chip esp32
setup {
    var i: int = 0
    while i < 10 {
        i = i + 1
        if i == 5 { continue }
    }
}
