
chip esp32
setup {
    var stride: int = 5
    for i in 0..100 step stride {
        log("step", "i")
    }
}
