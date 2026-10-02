chip esp32
hardware {
    pin x: Output = 5
}
every 1s { x.toggle() }
