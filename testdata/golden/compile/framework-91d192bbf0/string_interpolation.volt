
chip esp32
hardware {
    pin led: Output = 2
}
setup {
    var count: int = 42
    log("app", "count is {count}")
}
