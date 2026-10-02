chip esp32
hardware { pin led: Output = 4 }
machine m {
    state a {
        enter { led.on() }
        exit { led.off() }
        after 500ms -> a
    }
}
