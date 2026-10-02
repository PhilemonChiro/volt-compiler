
machine Bad {
    initial state a {
        on x() -> b
        on x() -> c
    }
    state b {
        on y() -> a
    }
    state c {
        on y() -> a
    }
}
