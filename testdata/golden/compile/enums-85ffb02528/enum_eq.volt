
chip esp32
enum DoorState {
    locked
    unlocked
    ajar
}
hardware {
    pin led: Output = 2
}
setup {
    var state: DoorState = DoorState.locked
    if state == DoorState.unlocked {
        led.on()
    }
}
