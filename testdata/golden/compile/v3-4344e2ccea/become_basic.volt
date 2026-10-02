
hardware { pin led = 2 }
machine blinker {
    state on_state {
        enter {
            led.on()
        }
        after 1s -> off_state
    }
    state off_state {
        enter {
            led.off()
        }
        after 1s -> on_state
    }
}
