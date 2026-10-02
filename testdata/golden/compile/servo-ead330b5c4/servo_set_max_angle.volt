
chip esp32
hardware {
    pin s: Servo = 5
}
setup {
    s.set_max_angle(270)
}
