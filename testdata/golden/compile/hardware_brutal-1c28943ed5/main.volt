chip esp32
hardware {
    pin x: Output = 0
}
every 1s { x.toggle() }
