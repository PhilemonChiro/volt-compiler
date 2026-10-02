chip esp32
hardware { pin led: Output = 4 }
machine m {
    state a {
        enter { led.on() }
        after 500ms -> totally_made_up
    }
}
