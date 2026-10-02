chip esp32
hardware { pin led: Output = 4 }
machine m {
    state a {
        enter { led.on() }
        after 500ms -> b
    }
    state b {
        enter { led.off() }
        after 500ms -> a
    }
}
