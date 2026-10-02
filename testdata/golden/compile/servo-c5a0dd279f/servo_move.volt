
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.move(45)
}
