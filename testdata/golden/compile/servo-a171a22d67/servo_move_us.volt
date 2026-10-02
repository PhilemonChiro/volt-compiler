
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.move_us(1500)
}
