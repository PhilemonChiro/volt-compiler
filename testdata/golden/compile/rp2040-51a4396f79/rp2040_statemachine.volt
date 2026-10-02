
chip rp2040
hardware { pin led: Output = 25 }
machine blinker {
    state off {
        enter { led.off() }
        after 1s -> on
    }
    state on {
        enter { led.on() }
        after 500ms -> off
    }
}
