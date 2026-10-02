chip esp32
hardware {
    pin led: Output = 34
}
every 1s { led.toggle() }
