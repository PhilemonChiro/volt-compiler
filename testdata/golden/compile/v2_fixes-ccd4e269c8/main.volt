chip esp32
hardware {
    pin led: Output = 2
}
setup {
    log("test", "a" + 1 + "b" + 2 + "c" + 3 + "d" + 4 + "e" + 5)
}
