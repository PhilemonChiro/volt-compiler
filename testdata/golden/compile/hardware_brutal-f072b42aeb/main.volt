chip esp32
hardware {
    pin x: Output = 2
}
every 1s { x.toggle() }
