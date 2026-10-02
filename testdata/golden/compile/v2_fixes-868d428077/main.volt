chip esp32
hardware {
    pin led: Output = 2
}
machine Door {
    initial state locked {
        on unlock_cmd -> unlocked
    }
    state unlocked {
        on lock_cmd -> locked
    }
}
