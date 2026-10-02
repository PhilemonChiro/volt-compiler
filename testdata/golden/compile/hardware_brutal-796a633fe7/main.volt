chip esp32
hardware {
    pin x: Output = 99
}
every 1s { x.toggle() }
