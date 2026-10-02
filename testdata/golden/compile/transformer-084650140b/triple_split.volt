
chip esp32
hardware {
    pin a: Output = 2
    pin b: Output = 4
    pin c: Output = 5
}
task triple {
    every 100ms { a.toggle() }
    every 200ms { b.toggle() }
    every 500ms { c.toggle() }
}
