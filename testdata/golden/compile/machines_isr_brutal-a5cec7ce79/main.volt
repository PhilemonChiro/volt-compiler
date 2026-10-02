chip esp32
hardware { pin led: Output = 4 }
machine m {
    state ticking {
        enter { led.toggle() }
        after 500ms -> ticking
    }
}
