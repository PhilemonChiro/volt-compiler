chip esp32
hardware {
    pin x: Output = 15
}
every 1s { x.toggle() }
