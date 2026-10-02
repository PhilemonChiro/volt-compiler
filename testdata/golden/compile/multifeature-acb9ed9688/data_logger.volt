
chip esp32
hardware {
    pin led: Output = 2
}

storage log_count: int = 0

setup {
    log_count = log_count + 1
    storage.save("runs", log_count)

    for i in 0..5 {
        log("init", "step {i}")
    }

    var msg: string = "Boot #{log_count}"
    log("boot", msg)
}
