
machine Bad {
    initial state a {
        on x() -> b
    }
    state b {
        on y() -> a
    }
    state orphan {
        on z() -> a
    }
}
