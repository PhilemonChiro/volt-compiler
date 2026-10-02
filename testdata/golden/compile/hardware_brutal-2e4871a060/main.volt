chip rp2040
hardware {
    pin led: Output = 30
}
every 1s { led.toggle() }
