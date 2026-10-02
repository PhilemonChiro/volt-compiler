/* LED blink via state machine */

hardware {
    pin led = 2
}

machine blinker {
    state off_state {
        enter {
            led.off()
        }
        after 500ms -> on_state
    }
    state on_state {
        enter {
            led.on()
        }
        after 500ms -> off_state
    }
}
