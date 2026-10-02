
hardware {
    pin led: Output = 2
}
machine Blinker {
    initial state off {
        enter {
            led.off()
        }
        on activate() -> on
    }
    state on {
        enter {
            led.on()
        }
        exit {
            log("leaving on state")
        }
        on deactivate() -> off
    }
}
