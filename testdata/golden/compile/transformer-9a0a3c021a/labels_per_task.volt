
chip esp32
hardware {
    pin led1: Output = 2
    pin led2: Output = 4
}
task t1 {
    every 500ms { led1.toggle() }
}
task t2 {
    every 1s { led2.toggle() }
}
