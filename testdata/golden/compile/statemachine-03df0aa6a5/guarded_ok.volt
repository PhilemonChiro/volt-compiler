
machine Good {
    var n = 0
    initial state a {
        on x() [n < 5] -> b
        on x() -> c
    }
    state b {
        on y() -> a
    }
    state c {
        on y() -> a
    }
}
