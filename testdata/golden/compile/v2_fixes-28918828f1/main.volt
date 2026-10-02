chip esp32
hardware {
    pin led: Output = 2
}
machine Test {
    initial state on {
        exit {
            let x = oops_undefined
        }
        after 5s -> off
    }
    state off {
        after 5s -> on
    }
}
