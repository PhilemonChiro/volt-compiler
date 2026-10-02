
machine Retry {
    var count = 0

    initial state trying {
        on fail() [count < 3] -> trying
        on fail() -> gave_up
        on succeed() -> done
    }
    state gave_up {
        after 10seconds -> trying
    }
    state done {
    }
}
