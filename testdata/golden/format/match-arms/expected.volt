enum Mode { idle, run }

var m: Mode = Mode.idle

var n: int = 0

setup {
    match(n) {
        0 -> log("a", "zero")
        1..3 -> {
            n += 1
        }
        4 -> {
            n = 0
            log("a", "four")
        }
        _ -> n = 9
    }
    match(m) {
        .idle -> m = Mode.run
        _ -> {
            n -= 1
        }
    }
}
