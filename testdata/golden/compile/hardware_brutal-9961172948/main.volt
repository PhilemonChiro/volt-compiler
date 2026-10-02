chip esp32
hardware {
    pin led: Output = 35
}
every 1s { led.toggle() }
