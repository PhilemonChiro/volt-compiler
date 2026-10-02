chip esp32
hardware { pin led: Output = 4 }
machine m {
    state idle { }
}
every 1s { led.toggle() }
