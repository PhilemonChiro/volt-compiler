chip esp32
hardware {
    pin led: Output = 36
}
every 1s { led.toggle() }
