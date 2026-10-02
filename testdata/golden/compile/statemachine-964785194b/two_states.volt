
machine Light {
    initial state off {
        on toggle() -> on
    }
    state on {
        on toggle() -> off
    }
}
