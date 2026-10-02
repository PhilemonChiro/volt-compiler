chip esp32
hardware {
    pin x: Output = 12
}
every 1s { x.toggle() }
