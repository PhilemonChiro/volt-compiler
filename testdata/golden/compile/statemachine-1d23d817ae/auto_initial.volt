
machine Auto {
    state first {
        on x() -> second
    }
    state second {
        on y() -> first
    }
}
