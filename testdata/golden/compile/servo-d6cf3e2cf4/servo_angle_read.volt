
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    var a: float = s.angle()
}
