chip esp32
hardware { pin led: Output = 4 }
machine m {
    state only {
        enter { led.on() }
    }
}
every 1s { led.toggle() }
