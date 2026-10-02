chip esp32
hardware {
    pin led: Output = 39
}
every 1s { led.toggle() }
